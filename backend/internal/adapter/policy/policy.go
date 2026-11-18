// Package policy rejects source requests that are outside the public page catalog.
// Allowed document fetches are the configured sitemap, /sitemap/video/{n}.xml,
// and official work pages /video/{id}/. Paths under /api/ are always refused.
package policy

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// ErrDenied is returned for every refused URL. It does not include the URL.
var ErrDenied = errors.New("采集策略拒绝了该请求")

type Kind int

const (
	KindAny Kind = iota
	KindIndex
	KindVideoList
	KindDetail
)

var (
	videoListRe = regexp.MustCompile(`^/sitemap/video/[0-9]+\.xml$`)
	detailRe    = regexp.MustCompile(`^/video/[0-9]+/$`)
	mediaExtRe  = regexp.MustCompile(`\.(m3u8|mp4|m4s|m4v|ts|flv|mov|mkv|webm|key)$`)
)

var blockedPrefixes = []string{
	"/admin", "/profile", "/upload", "/search", "/login", "/register",
}

// Check resolves raw against base and applies the deny rules plus an optional allow kind.
func Check(base *url.URL, raw string, kind Kind) (*url.URL, error) {
	u, err := resolve(base, raw)
	if err != nil {
		return nil, ErrDenied
	}
	if err := vet(base, u); err != nil {
		return nil, ErrDenied
	}
	switch kind {
	case KindIndex:
		want := base.Path
		if want == "" {
			want = "/"
		}
		// Index path is supplied separately; caller passes the absolute index URL
		// and KindAny when the path was already chosen. KindIndex only checks safety.
	case KindVideoList:
		if !videoListRe.MatchString(u.Path) {
			return nil, ErrDenied
		}
	case KindDetail:
		if !detailRe.MatchString(u.Path) {
			return nil, ErrDenied
		}
	}
	return u, nil
}

func resolve(base *url.URL, raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || base == nil {
		return nil, errors.New("empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if !u.IsAbs() {
		u = base.ResolveReference(u)
	}
	if u.Path == "" {
		u.Path = "/"
	}
	if !strings.HasSuffix(u.Path, "/") && detailLooksBare(u.Path) {
		u.Path += "/"
	}
	return u, nil
}

func detailLooksBare(path string) bool {
	return regexp.MustCompile(`^/video/[0-9]+$`).MatchString(path)
}

func vet(base, u *url.URL) error {
	if u.User != nil {
		return ErrDenied
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ErrDenied
	}
	if !strings.EqualFold(u.Hostname(), base.Hostname()) {
		return ErrDenied
	}
	if u.Port() != base.Port() {
		return ErrDenied
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return ErrDenied
	}
	path := u.Path
	if strings.Contains(path, "..") {
		return ErrDenied
	}
	lower := strings.ToLower(path)
	if lower == "/api" || strings.HasPrefix(lower, "/api/") || strings.Contains(lower, "/api/") {
		return ErrDenied
	}
	for _, prefix := range blockedPrefixes {
		if lower == prefix || strings.HasPrefix(lower, prefix+"/") {
			return ErrDenied
		}
	}
	if strings.Contains(lower, "/embed") {
		return ErrDenied
	}
	if mediaExtRe.MatchString(lower) {
		return ErrDenied
	}
	return nil
}

// IndexPath reports whether path is an allowed sitemap index path (not under /api/).
func IndexPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/") || strings.Contains(path, "..") {
		return false
	}
	lower := strings.ToLower(path)
	if lower == "/api" || strings.HasPrefix(lower, "/api/") {
		return false
	}
	if strings.Contains(lower, "?") {
		return false
	}
	return true
}
