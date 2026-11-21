// Package download turns HLS playlists into MP4 files.
//
// It is intentionally independent from the catalog adapter policy: the media
// CDN usually lives on another host and signs its playlist URLs. The caller is
// responsible for only downloading streams it is allowed to keep.
package download

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Variant is one entry of a master playlist.
type Variant struct {
	URL       string
	Bandwidth int
	Width     int
	Height    int
	Codecs    string
}

// KeyInfo is an EXT-X-KEY declaration.
type KeyInfo struct {
	Method string
	URI    string
	IV     string
}

// Segment is one media segment.
type Segment struct {
	URL      string
	Seq      int
	Duration float64
	Key      KeyInfo
	InitURL  string
	Offset   int64
	Length   int64
}

// Playlist is a resolved media playlist.
type Playlist struct {
	URL      string
	Segments []Segment
	Duration float64
	Complete bool
}

// Result describes a finished download.
type Result struct {
	Path     string
	Size     int64
	Duration float64
	Segments int
}

// Engine downloads and merges HLS streams.
type Engine struct {
	Client      *http.Client
	FFmpeg      string
	Concurrency int
	TempRoot    string
	KeepTemp    bool
}

func NewEngine(ffmpeg string, concurrency int) *Engine {
	if concurrency < 1 {
		concurrency = 8
	}
	return &Engine{
		Client:      &http.Client{Timeout: 60 * time.Second},
		FFmpeg:      ffmpeg,
		Concurrency: concurrency,
	}
}

var attrRe = regexp.MustCompile(`([A-Z0-9-]+)=("[^"]*"|[^,]*)`)

func attrs(line string) map[string]string {
	out := map[string]string{}
	for _, m := range attrRe.FindAllStringSubmatch(line, -1) {
		v := strings.TrimSpace(m[2])
		v = strings.Trim(v, `"`)
		out[m[1]] = v
	}
	return out
}

func resolve(base *url.URL, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", errors.New("空地址")
	}
	u, err := url.Parse(ref)
	if err != nil {
		return "", err
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	return u.String(), nil
}

// ParsePlaylist parses either a master or a media playlist.
func ParsePlaylist(text, baseURL string) ([]Variant, *Playlist, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil, nil, err
	}
	if !strings.Contains(text, "#EXTM3U") {
		return nil, nil, errors.New("不是 m3u8 播放列表")
	}
	var variants []Variant
	pl := &Playlist{URL: baseURL, Complete: false}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")

	var (
		key        KeyInfo
		initURL    string
		duration   float64
		seq        int
		offset     int64
		length     int64
		hasRange   bool
		lastEnd    int64
		pendingInf bool
	)
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "#EXT-X-STREAM-INF:"):
			a := attrs(strings.TrimPrefix(line, "#EXT-X-STREAM-INF:"))
			var uri string
			for j := i + 1; j < len(lines); j++ {
				next := strings.TrimSpace(lines[j])
				if next == "" || strings.HasPrefix(next, "#") {
					continue
				}
				uri = next
				i = j
				break
			}
			if uri == "" {
				continue
			}
			abs, err := resolve(base, uri)
			if err != nil {
				continue
			}
			v := Variant{URL: abs, Codecs: a["CODECS"]}
			v.Bandwidth, _ = strconv.Atoi(a["BANDWIDTH"])
			if res := a["RESOLUTION"]; res != "" {
				fmt.Sscanf(res, "%dx%d", &v.Width, &v.Height)
			}
			variants = append(variants, v)

		case strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"):
			seq, _ = strconv.Atoi(strings.TrimPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"))

		case strings.HasPrefix(line, "#EXT-X-KEY:"):
			a := attrs(strings.TrimPrefix(line, "#EXT-X-KEY:"))
			method := strings.ToUpper(a["METHOD"])
			if method == "NONE" {
				key = KeyInfo{}
				continue
			}
			key = KeyInfo{Method: method, IV: a["IV"]}
			if a["URI"] != "" {
				key.URI, _ = resolve(base, a["URI"])
			}

		case strings.HasPrefix(line, "#EXT-X-MAP:"):
			a := attrs(strings.TrimPrefix(line, "#EXT-X-MAP:"))
			if a["URI"] != "" {
				initURL, _ = resolve(base, a["URI"])
			}

		case strings.HasPrefix(line, "#EXT-X-BYTERANGE:"):
			spec := strings.TrimPrefix(line, "#EXT-X-BYTERANGE:")
			if at := strings.Index(spec, "@"); at >= 0 {
				length, _ = strconv.ParseInt(spec[:at], 10, 64)
				offset, _ = strconv.ParseInt(spec[at+1:], 10, 64)
			} else {
				length, _ = strconv.ParseInt(spec, 10, 64)
				offset = lastEnd
			}
			hasRange = length > 0

		case strings.HasPrefix(line, "#EXTINF:"):
			spec := strings.TrimPrefix(line, "#EXTINF:")
			if c := strings.Index(spec, ","); c >= 0 {
				spec = spec[:c]
			}
			duration, _ = strconv.ParseFloat(strings.TrimSpace(spec), 64)
			pendingInf = true

		case strings.HasPrefix(line, "#EXT-X-ENDLIST"):
			pl.Complete = true

		case strings.HasPrefix(line, "#"):
			// ignore other tags

		default:
			if !pendingInf {
				continue
			}
			abs, err := resolve(base, line)
			if err != nil {
				continue
			}
			seg := Segment{
				URL: abs, Seq: seq + len(pl.Segments), Duration: duration,
				Key: key, InitURL: initURL,
			}
			if hasRange {
				seg.Offset, seg.Length = offset, length
				lastEnd = offset + length
			}
			pl.Segments = append(pl.Segments, seg)
			pl.Duration += duration
			duration, hasRange, offset, length, pendingInf = 0, false, 0, 0, false
		}
	}
	if len(variants) > 0 {
		return variants, nil, nil
	}
	return nil, pl, nil
}

// ChooseVariant picks best/worst or the closest height not below want.
func ChooseVariant(vs []Variant, quality string) (Variant, error) {
	if len(vs) == 0 {
		return Variant{}, errors.New("没有可用清晰度")
	}
	best := func(a, b Variant) bool {
		if a.Height != b.Height {
			return a.Height > b.Height
		}
		return a.Bandwidth > b.Bandwidth
	}
	cur := vs[0]
	for _, v := range vs[1:] {
		if best(v, cur) {
			cur = v
		}
	}
	switch strings.ToLower(strings.TrimSpace(quality)) {
	case "", "best":
		return cur, nil
	case "worst":
		cur = vs[0]
		for _, v := range vs[1:] {
			if !best(v, cur) {
				cur = v
			}
		}
		return cur, nil
	}
	want, err := strconv.Atoi(strings.TrimSuffix(strings.ToLower(quality), "p"))
	if err != nil {
		return Variant{}, fmt.Errorf("画质参数无效：%s", quality)
	}
	var chosen Variant
	found := false
	for _, v := range vs {
		if v.Height >= want && (!found || v.Height < chosen.Height || (v.Height == chosen.Height && v.Bandwidth > chosen.Bandwidth)) {
			chosen, found = v, true
		}
	}
	if found {
		return chosen, nil
	}
	return cur, nil
}

func (e *Engine) fetch(ctx context.Context, raw string, header http.Header) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	copyHeader(req.Header, header)
	resp, err := e.Client.Do(req)
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

func copyHeader(dst, src http.Header) {
	for k, vs := range src {
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

// LoadMedia resolves a master playlist down to a media playlist.
func (e *Engine) LoadMedia(ctx context.Context, raw string, header http.Header, quality string) (*Playlist, error) {
	cur := raw
	for depth := 0; depth < 4; depth++ {
		body, err := e.fetch(ctx, cur, header)
		if err != nil {
			return nil, err
		}
		vs, pl, err := ParsePlaylist(string(body), cur)
		if err != nil {
			return nil, err
		}
		if pl != nil {
			if len(pl.Segments) == 0 {
				return nil, errors.New("播放列表没有分片")
			}
			return pl, nil
		}
		v, err := ChooseVariant(vs, quality)
		if err != nil {
			return nil, err
		}
		cur = v.URL
	}
	return nil, errors.New("播放列表嵌套过深")
}

func ivFor(seg Segment) []byte {
	if strings.TrimSpace(seg.Key.IV) != "" {
		raw := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(seg.Key.IV)), "0x")
		if b, err := hex.DecodeString(strings.Repeat("0", 32-len(raw)) + raw); err == nil && len(b) == 16 {
			return b
		}
	}
	b := make([]byte, 16)
	binary.BigEndian.PutUint64(b[8:], uint64(seg.Seq))
	return b
}

func decryptAES128(data, key, iv []byte) ([]byte, error) {
	if len(data)%aes.BlockSize != 0 {
		return nil, errors.New("密文长度不是 16 的倍数")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, data)
	if n := int(out[len(out)-1]); n > 0 && n <= aes.BlockSize && n <= len(out) {
		padOK := true
		for _, b := range out[len(out)-n:] {
			if int(b) != n {
				padOK = false
				break
			}
		}
		if padOK {
			out = out[:len(out)-n]
		}
	}
	return out, nil
}

func (e *Engine) segment(ctx context.Context, seg Segment, keyCache map[string][]byte, mu *sync.Mutex, header http.Header) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, seg.URL, nil)
	if err != nil {
		return nil, err
	}
	copyHeader(req.Header, header)
	if seg.Length > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", seg.Offset, seg.Offset+seg.Length-1))
	}
	resp, err := e.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(seg.Key.Method, "AES-128") {
		if seg.Key.URI == "" {
			return nil, errors.New("AES-128 分片缺少密钥地址")
		}
		mu.Lock()
		key, ok := keyCache[seg.Key.URI]
		mu.Unlock()
		if !ok {
			raw, err := e.fetch(ctx, seg.Key.URI, header)
			if err != nil {
				return nil, fmt.Errorf("拉取密钥失败：%w", err)
			}
			if len(raw) != 16 {
				return nil, fmt.Errorf("密钥长度异常：%d", len(raw))
			}
			mu.Lock()
			keyCache[seg.Key.URI] = raw
			mu.Unlock()
			key = raw
		}
		data, err = decryptAES128(data, key, ivFor(seg))
		if err != nil {
			return nil, err
		}
	}
	return data, nil
}

// Download fetches every segment, concatenates them and remuxes to MP4.
func (e *Engine) Download(ctx context.Context, playlistURL string, header http.Header, outPath, quality string, progress func(done, total int)) (Result, error) {
	if header == nil {
		header = http.Header{}
	}
	pl, err := e.LoadMedia(ctx, playlistURL, header, quality)
	if err != nil {
		return Result{}, err
	}
	if e.FFmpeg == "" {
		ff, err := exec.LookPath("ffmpeg")
		if err != nil {
			return Result{}, errors.New("未找到 ffmpeg，请安装或在配置里指定 FFMPEG_PATH")
		}
		e.FFmpeg = ff
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return Result{}, err
	}
	root := e.TempRoot
	if root == "" {
		root = os.TempDir()
	}
	tmp, err := os.MkdirTemp(root, ".m3u8dl-*")
	if err != nil {
		return Result{}, err
	}
	if !e.KeepTemp {
		defer os.RemoveAll(tmp)
	}

	files := make([]string, len(pl.Segments))
	total := len(pl.Segments)
	var (
		wg       sync.WaitGroup
		keyMu    sync.Mutex
		keyCache = map[string][]byte{}
		errMu    sync.Mutex
		firstErr error
		done     int
		doneMu   sync.Mutex
	)
	sem := make(chan struct{}, max(1, e.Concurrency))
	for i, seg := range pl.Segments {
		wg.Add(1)
		go func(i int, seg Segment) {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			case sem <- struct{}{}:
			}
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			var data []byte
			var err error
			for attempt := 0; attempt < 3; attempt++ {
				data, err = e.segment(ctx, seg, keyCache, &keyMu, header)
				if err == nil {
					break
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Duration(attempt+1) * 400 * time.Millisecond):
				}
			}
			if err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("第 %d 个分片：%w", i+1, err)
				}
				errMu.Unlock()
				return
			}
			path := filepath.Join(tmp, fmt.Sprintf("seg_%05d.bin", i))
			if werr := os.WriteFile(path, data, 0o644); werr != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = werr
				}
				errMu.Unlock()
				return
			}
			files[i] = path
			doneMu.Lock()
			done++
			d := done
			doneMu.Unlock()
			if progress != nil {
				progress(d, total)
			}
		}(i, seg)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if firstErr != nil {
		return Result{}, firstErr
	}

	joinedName := "joined.ts"
	if pl.Segments[0].InitURL != "" {
		joinedName = "joined.mp4"
	}
	joined := filepath.Join(tmp, joinedName)
	out, err := os.Create(joined)
	if err != nil {
		return Result{}, err
	}
	if init := pl.Segments[0].InitURL; init != "" {
		initData, err := e.fetch(ctx, init, header)
		if err == nil {
			_, _ = out.Write(initData)
		}
	}
	for _, p := range files {
		if p == "" {
			out.Close()
			return Result{}, errors.New("分片缺失")
		}
		in, err := os.Open(p)
		if err != nil {
			out.Close()
			return Result{}, err
		}
		_, copyErr := io.Copy(out, in)
		in.Close()
		if copyErr != nil {
			out.Close()
			return Result{}, copyErr
		}
	}
	if err := out.Close(); err != nil {
		return Result{}, err
	}

	if err := e.remux(ctx, joined, outPath); err != nil {
		return Result{}, err
	}
	st, err := os.Stat(outPath)
	if err != nil {
		return Result{}, err
	}
	return Result{Path: outPath, Size: st.Size(), Duration: pl.Duration, Segments: len(pl.Segments)}, nil
}

func (e *Engine) remux(ctx context.Context, src, dst string) error {
	base := []string{"-hide_banner", "-y", "-loglevel", "error", "-i", src,
		"-c", "copy", "-movflags", "+faststart", dst}
	if out, err := exec.CommandContext(ctx, e.FFmpeg, base...).CombinedOutput(); err == nil {
		return nil
	} else {
		second := []string{"-hide_banner", "-y", "-loglevel", "error", "-i", src,
			"-c", "copy", "-bsf:a", "aac_adtstoasc", "-movflags", "+faststart", dst}
		if out2, err2 := exec.CommandContext(ctx, e.FFmpeg, second...).CombinedOutput(); err2 == nil {
			return nil
		} else {
			return fmt.Errorf("ffmpeg 合封失败：%s / %s", tail(string(out)), tail(string(out2)))
		}
	}
}

func tail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 400 {
		return s
	}
	return s[len(s)-400:]
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
