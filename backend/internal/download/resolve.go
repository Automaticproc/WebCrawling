package download

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Stream is a resolved playback entry.
type Stream struct {
	M3U8    string
	PageURL string
	Headers http.Header
	Source  string // html | browser | direct
}

// Resolver turns an episode page into an m3u8 URL.
type Resolver struct {
	Client         *http.Client
	BrowserPath    string
	BrowserPort    int
	BrowserTimeout time.Duration
	BrowserProfile string
	TempDir        string
	Cookies        string

	LoginURL            string
	LoginUser           string
	LoginPassword       string
	LoginUserSelector   string
	LoginPassSelector   string
	LoginSubmitSelector string

	UserAgent string

	captureMu sync.Mutex
	loggedIn  bool
}

func NewResolver(browserPath string, browserTimeout time.Duration) *Resolver {
	if browserTimeout <= 0 {
		browserTimeout = 45 * time.Second
	}
	return &Resolver{
		Client:         &http.Client{Timeout: 30 * time.Second},
		BrowserPath:    browserPath,
		BrowserTimeout: browserTimeout,
		UserAgent:      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	}
}

var (
	absM3U8Re = regexp.MustCompile(`(?i)https?://[^\s"'<>\\]+?\.m3u8(?:\?[^\s"'<>\\]*)?`)
	relM3U8Re = regexp.MustCompile(`(?i)["']([^"'\s<>\\]+?\.m3u8(?:\?[^"'\s<>\\]*)?)["']`)
)

func normalizePlaylistText(body []byte) string {
	s := string(body)
	s = strings.ReplaceAll(s, `\/`, `/`)
	s = strings.ReplaceAll(s, `\u002F`, `/`)
	s = strings.ReplaceAll(s, `\u002f`, `/`)
	s = strings.ReplaceAll(s, `\u003A`, `:`)
	s = strings.ReplaceAll(s, `\u003a`, `:`)
	s = html.UnescapeString(s)
	return s
}

// FindPlaylists extracts candidate m3u8 URLs from page HTML or embedded JSON.
func FindPlaylists(body []byte, pageURL string) []string {
	text := normalizePlaylistText(body)
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		if strings.HasPrefix(raw, "//") {
			raw = base.Scheme + ":" + raw
		}
		u, err := url.Parse(raw)
		if err != nil {
			return
		}
		if !u.IsAbs() {
			u = base.ResolveReference(u)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return
		}
		full := u.String()
		if !seen[full] {
			seen[full] = true
			out = append(out, full)
		}
	}
	for _, m := range absM3U8Re.FindAllString(text, -1) {
		add(m)
	}
	for _, m := range relM3U8Re.FindAllStringSubmatch(text, -1) {
		add(m[1])
	}
	return out
}

// Resolve loads the page and falls back to a headless browser if configured.
func (r *Resolver) Resolve(ctx context.Context, pageURL string, extra http.Header) (Stream, error) {
	header := http.Header{}
	if extra != nil {
		for k, vs := range extra {
			for _, v := range vs {
				header.Add(k, v)
			}
		}
	}
	if header.Get("User-Agent") == "" {
		header.Set("User-Agent", r.UserAgent)
	}
	if header.Get("Referer") == "" {
		header.Set("Referer", pageURL)
	}

	body, err := r.fetchPage(ctx, pageURL, header)
	if err == nil {
		if list := FindPlaylists(body, pageURL); len(list) > 0 {
			return Stream{M3U8: list[0], PageURL: pageURL, Headers: header, Source: "html"}, nil
		}
	}

	// 静态页面没有播放地址：用浏览器跑播放器；找不到浏览器时返回明确错误。
	r.captureMu.Lock()
	loginURL := ""
	if !r.loggedIn {
		loginURL = r.LoginURL
	}
	res, capErr := captureStream(ctx, browserOptions{
		PageURL:        pageURL,
		Path:           r.BrowserPath,
		Port:           r.BrowserPort,
		Timeout:        r.BrowserTimeout,
		TempDir:        r.TempDir,
		ProfileDir:     r.BrowserProfile,
		Cookies:        r.Cookies,
		LoginURL:       loginURL,
		LoginUser:      r.LoginUser,
		LoginPassword:  r.LoginPassword,
		UserSelector:   r.LoginUserSelector,
		PassSelector:   r.LoginPassSelector,
		SubmitSelector: r.LoginSubmitSelector,
	})
	if capErr == nil && loginURL != "" {
		r.loggedIn = true
	}
	r.captureMu.Unlock()
	if capErr != nil {
		if err != nil {
			return Stream{}, fmt.Errorf("抓取页面失败：%w；浏览器抓流也失败：%v", err, capErr)
		}
		return Stream{}, capErr
	}
	if res.Cookies != "" {
		header.Set("Cookie", res.Cookies)
	}
	header.Set("Referer", pageURL)
	if ua := res.Headers.Get("User-Agent"); ua != "" {
		header.Set("User-Agent", ua)
	}
	for k, vs := range res.Headers {
		lk := strings.ToLower(k)
		if lk == "host" || lk == "connection" || lk == "content-length" || lk == "cookie" || strings.HasPrefix(lk, "sec-") {
			continue
		}
		if header.Get(k) == "" {
			for _, v := range vs {
				header.Add(k, v)
			}
		}
	}
	return Stream{M3U8: res.URL, PageURL: pageURL, Headers: header, Source: "browser"}, nil
}

func (r *Resolver) fetchPage(ctx context.Context, pageURL string, header http.Header) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, err
	}
	copyHeader(req.Header, header)
	resp, err := r.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// DecodeDataURL is a small helper used by tests and debugging output.
func DecodeDataURL(raw string) ([]byte, error) {
	if i := strings.Index(raw, ","); strings.HasPrefix(raw, "data:") && i > 0 {
		return base64.StdEncoding.DecodeString(raw[i+1:])
	}
	return nil, errors.New("不是 data URL")
}
