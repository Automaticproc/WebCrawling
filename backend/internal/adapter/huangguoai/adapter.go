package huangguoai

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"catalog/internal/adapter/playurl"
	"catalog/internal/adapter/policy"
	"catalog/internal/store"
	"catalog/internal/timeutil"
)

const userAgent = "LocalCatalogBot/1.0 (+local admin; official page metadata only)"

// Ref is a work page discovered from a public sitemap.
type Ref struct {
	ExternalID string
	PageURL    string
	LastMod    time.Time
}

// Adapter reads public sitemaps and work-page HTML for one origin.
type Adapter struct {
	base        *url.URL
	sitemapPath string
	client      *http.Client
}

func New(baseURL, sitemapPath string, client *http.Client) (*Adapter, error) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("平台地址无效")
	}
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	u.User = nil
	if sitemapPath == "" {
		sitemapPath = "/sitemap.xml"
	}
	if !policy.IndexPath(sitemapPath) {
		return nil, policy.ErrDenied
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	base := u
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("重定向过多")
		}
		_, err := policy.Check(base, req.URL.String(), policy.KindAny)
		return err
	}
	return &Adapter{base: base, sitemapPath: sitemapPath, client: client}, nil
}

func (a *Adapter) Code() string { return "huangguoai" }

// Discover lists work pages. Incremental mode keeps entries on or after since (Hong Kong calendar day).
// Locations that fail the request policy are ignored and never fetched.
func (a *Adapter) Discover(ctx context.Context, mode string, since time.Time) ([]Ref, error) {
	index, err := a.indexURL()
	if err != nil {
		return nil, err
	}
	body, err := a.get(ctx, index, policy.KindAny)
	if err != nil {
		return nil, err
	}
	children, pages, err := parseSitemap(body)
	if err != nil {
		return nil, err
	}
	var refs []Ref
	if len(children) == 0 {
		return dedupe(a.filterPages(pages, mode, since)), nil
	}
	for _, child := range children {
		u, err := policy.Check(a.base, child, policy.KindVideoList)
		if err != nil {
			continue
		}
		body, err := a.get(ctx, u, policy.KindVideoList)
		if err != nil {
			return nil, fmt.Errorf("拉取作品站点地图失败")
		}
		_, childPages, err := parseSitemap(body)
		if err != nil {
			return nil, err
		}
		refs = append(refs, a.filterPages(childPages, mode, since)...)
	}
	return dedupe(refs), nil
}

// Fetch loads one work page. The returned description is unsaved caller input for skip checks.
func (a *Adapter) Fetch(ctx context.Context, ref Ref) (Detail, error) {
	u, err := policy.Check(a.base, ref.PageURL, policy.KindDetail)
	if err != nil {
		return Detail{}, err
	}
	body, err := a.get(ctx, u, policy.KindDetail)
	if err != nil {
		return Detail{}, fmt.Errorf("拉取作品 %s 失败", ref.ExternalID)
	}
	return ParseHTML(a.base, u.String(), body)
}

func (a *Adapter) indexURL() (*url.URL, error) {
	u := *a.base
	u.Path = a.sitemapPath
	checked, err := policy.Check(a.base, u.String(), policy.KindAny)
	if err != nil {
		return nil, err
	}
	return checked, nil
}

func (a *Adapter) get(ctx context.Context, u *url.URL, kind policy.Kind) ([]byte, error) {
	checked, err := policy.Check(a.base, u.String(), kind)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checked.String(), nil)
	if err != nil {
		return nil, policy.ErrDenied
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xml,text/xml;q=0.9,*/*;q=0.8")
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败")
	}
	defer resp.Body.Close()
	if _, err := policy.Check(a.base, resp.Request.URL.String(), kind); err != nil {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, policy.ErrDenied
	}
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("上游状态 %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

func (a *Adapter) filterPages(pages []sitePage, mode string, since time.Time) []Ref {
	var out []Ref
	for _, page := range pages {
		link, ok := playurl.Normalize(a.base, page.Loc)
		if !ok || link.EpisodeNo != 1 {
			continue
		}
		if !strings.HasSuffix(urlPath(link.URL), "/video/"+link.ExternalID+"/") {
			continue
		}
		mod, dated := timeutil.ParseLastMod(page.LastMod)
		if mode == store.ModeIncremental && !since.IsZero() {
			if dated && !timeutil.OnOrAfterDay(mod, since) {
				continue
			}
		}
		out = append(out, Ref{ExternalID: link.ExternalID, PageURL: link.URL, LastMod: mod})
	}
	return out
}

func urlPath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Path
}

func dedupe(in []Ref) []Ref {
	byID := map[string]Ref{}
	for _, ref := range in {
		prev, ok := byID[ref.ExternalID]
		if !ok || ref.LastMod.After(prev.LastMod) {
			byID[ref.ExternalID] = ref
		}
	}
	out := make([]Ref, 0, len(byID))
	for _, ref := range byID {
		out = append(out, ref)
	}
	sort.Slice(out, func(i, j int) bool {
		ni, ei := strconv.Atoi(out[i].ExternalID)
		nj, ej := strconv.Atoi(out[j].ExternalID)
		if ei == nil && ej == nil && ni != nj {
			return ni < nj
		}
		return out[i].ExternalID < out[j].ExternalID
	})
	return out
}

type sitePage struct {
	Loc     string
	LastMod string
}

func parseSitemap(data []byte) (children []string, pages []sitePage, err error) {
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	dec.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) { return input, nil }
	var cur string
	var inSitemap, inURL bool
	var loc, lastmod string
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("站点地图无法解析")
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "sitemap":
				inSitemap = true
				loc = ""
			case "url":
				inURL = true
				loc, lastmod = "", ""
			case "loc", "lastmod":
				cur = t.Name.Local
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "sitemap":
				if strings.TrimSpace(loc) != "" {
					children = append(children, strings.TrimSpace(loc))
				}
				inSitemap = false
				cur = ""
			case "url":
				if strings.TrimSpace(loc) != "" {
					pages = append(pages, sitePage{Loc: strings.TrimSpace(loc), LastMod: strings.TrimSpace(lastmod)})
				}
				inURL = false
				cur = ""
			case "loc", "lastmod":
				cur = ""
			}
		case xml.CharData:
			if cur == "loc" && (inSitemap || inURL) {
				loc += string(t)
			} else if cur == "lastmod" && inURL {
				lastmod += string(t)
			}
		}
	}
	return children, pages, nil
}
