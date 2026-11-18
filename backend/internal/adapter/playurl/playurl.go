// Package playurl keeps only official work and episode page URLs.
// Episode 1 is the work page (/video/{id}/). Later episodes are /video/{id}/ep-{n}/.
package playurl

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"catalog/internal/adapter/policy"
)

var (
	workRe = regexp.MustCompile(`^/video/([0-9]+)/$`)
	epRe   = regexp.MustCompile(`^/video/([0-9]+)/ep-([0-9]+)/$`)
)

// Link is an official page URL for one episode.
type Link struct {
	ExternalID string
	EpisodeNo  int
	URL        string
}

// Normalize returns an official play link or false.
// Query strings, other hosts, /api/, embed paths, and media files are rejected.
// /video/{id}/ep-1/ is rewritten to /video/{id}/.
func Normalize(base *url.URL, raw string) (Link, bool) {
	if base == nil {
		return Link{}, false
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return Link{}, false
	}
	if !u.IsAbs() {
		u = base.ResolveReference(u)
	}
	if u.Path != "" && !strings.HasSuffix(u.Path, "/") {
		if workRe.MatchString(u.Path+"/") || epRe.MatchString(u.Path+"/") {
			u.Path += "/"
		}
	}
	checked, err := policy.Check(base, u.String(), policy.KindAny)
	if err != nil {
		return Link{}, false
	}
	path := checked.Path
	if m := workRe.FindStringSubmatch(path); m != nil {
		return absLink(base, m[1], 1, path), true
	}
	m := epRe.FindStringSubmatch(path)
	if m == nil {
		return Link{}, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil || n < 1 {
		return Link{}, false
	}
	if n == 1 {
		path = "/video/" + m[1] + "/"
	}
	return absLink(base, m[1], n, path), true
}

func absLink(base *url.URL, id string, episode int, path string) Link {
	u := *base
	u.Path = path
	u.RawQuery = ""
	u.Fragment = ""
	u.User = nil
	return Link{ExternalID: id, EpisodeNo: episode, URL: u.String()}
}

// IsOfficial reports whether raw is an official work or episode page.
func IsOfficial(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	path := u.Path
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	if workRe.MatchString(path) {
		return true
	}
	m := epRe.FindStringSubmatch(path)
	if m == nil {
		return false
	}
	n, err := strconv.Atoi(m[2])
	return err == nil && n >= 2
}

// WorkPath is /video/{id}/.
func WorkPath(externalID string) string {
	return "/video/" + externalID + "/"
}

// EpisodePath is the official path for an episode. Episode 1 has no /ep-1/ segment.
func EpisodePath(externalID string, episode int) string {
	if episode <= 1 {
		return WorkPath(externalID)
	}
	return "/video/" + externalID + "/ep-" + strconv.Itoa(episode) + "/"
}
