package huangguoai

import (
	"bytes"
	"encoding/json"
	"html"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"catalog/internal/adapter/playurl"
	"catalog/internal/timeutil"
)

// Episode is one official episode page.
type Episode struct {
	No      int
	Title   string
	PlayURL string
}

// Detail is catalog metadata. It has no fields for media streams or player secrets.
// Description is returned to the caller so skip rules can run; the caller must not
// persist it unless those rules allow the item.
type Detail struct {
	ExternalID    string
	Title         string
	Description   string
	CoverURL      string
	Category      string
	Tags          []string
	Author        string
	PlayURL       string
	PublishedAt   *time.Time
	ViewCount     int64
	LikeCount     int64
	FavoriteCount int64
	CommentCount  int64
	Episodes      []Episode
}

type flexInt64 int64

func (f *flexInt64) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = flexInt64(parseCount(s))
		return nil
	}
	var n float64
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*f = flexInt64(math.Round(n))
	return nil
}

// allowlisted page payload. Unknown JSON fields are ignored by encoding/json.
type pageData struct {
	Title         string          `json:"title"`
	Description   string          `json:"description"`
	Author        string          `json:"author"`
	Published     string          `json:"time"`
	Views         string          `json:"views"`
	Likes         string          `json:"likes"`
	LikesNum      *flexInt64      `json:"likesNum"`
	Collects      string          `json:"collects"`
	CollectsNum   *flexInt64      `json:"collectsNum"`
	CommentsCount flexInt64       `json:"commentsCount"`
	Tags          json.RawMessage `json:"tags"`
	TagLinks      json.RawMessage `json:"tagLinks"`
	Tracking      struct {
		TotalEpisode  int    `json:"total_episode"`
		DramaTypeName string `json:"drama_type_name"`
	} `json:"tracking"`
}

var (
	scriptRe = regexp.MustCompile(`(?is)<script[^>]*\bid\s*=\s*["']videoInitialData["'][^>]*>(.*?)</script>`)
	h1Re     = regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`)
	metaRe   = regexp.MustCompile(`(?is)<meta\s+[^>]*>`)
	anchorRe = regexp.MustCompile(`(?is)<a\s+([^>]*?)>(.*?)</a>`)
	tagRe    = regexp.MustCompile(`(?s)<[^>]+>`)
	digitsRe = regexp.MustCompile(`^\d{1,4}$`)
)

// ParseHTML reads a work page. Only official episode links are kept.
func ParseHTML(base *url.URL, pageURL string, body []byte) (Detail, error) {
	link, ok := playurl.Normalize(base, pageURL)
	if !ok || link.EpisodeNo != 1 {
		return Detail{}, errBadPage
	}
	raw := string(body)
	data := pageData{}
	if m := scriptRe.FindStringSubmatch(raw); m != nil {
		payload := strings.TrimSpace(m[1])
		payload = html.UnescapeString(payload)
		_ = json.Unmarshal([]byte(payload), &data)
	}
	title := plain(data.Title)
	if title == "" {
		if m := h1Re.FindStringSubmatch(raw); m != nil {
			title = plain(m[1])
		}
	}
	title = clip(title, 500)
	if title == "" {
		return Detail{}, errNoTitle
	}
	desc := clip(plain(data.Description), 8000)
	if desc == "" {
		desc = clip(plain(metaContent(raw, "description")), 8000)
	}
	total := data.Tracking.TotalEpisode
	if total < 0 {
		total = 0
	}
	if total > 500 {
		total = 500
	}
	episodes := extractEpisodes(base, link.ExternalID, raw, total)
	views := parseCount(data.Views)
	likes := parseCount(data.Likes)
	if data.LikesNum != nil {
		likes = int64(*data.LikesNum)
	}
	favorites := parseCount(data.Collects)
	if data.CollectsNum != nil {
		favorites = int64(*data.CollectsNum)
	}
	d := Detail{
		ExternalID:    link.ExternalID,
		Title:         title,
		Description:   desc,
		CoverURL:      safeImage(metaContent(raw, "og:image")),
		Category:      clip(plain(data.Tracking.DramaTypeName), 120),
		Tags:          parseTags(data.Tags, data.TagLinks),
		Author:        clip(plain(data.Author), 120),
		PlayURL:       link.URL,
		PublishedAt:   timeutil.ParseSourceTime(data.Published),
		ViewCount:     views,
		LikeCount:     likes,
		FavoriteCount: favorites,
		CommentCount:  int64(data.CommentsCount),
		Episodes:      episodes,
	}
	return d, nil
}

func extractEpisodes(base *url.URL, externalID, page string, total int) []Episode {
	found := map[int]Episode{}
	for _, m := range anchorRe.FindAllStringSubmatch(page, -1) {
		href := attr(m[1], "href")
		link, ok := playurl.Normalize(base, href)
		if !ok || link.ExternalID != externalID {
			continue
		}
		title := plain(m[2])
		if title == "" || digitsRe.MatchString(title) || len([]rune(title)) > 40 {
			title = episodeTitle(link.EpisodeNo)
		}
		found[link.EpisodeNo] = Episode{No: link.EpisodeNo, Title: title, PlayURL: link.URL}
	}
	if len(found) == 0 && total > 0 {
		origin := *base
		for n := 1; n <= total; n++ {
			origin.Path = playurl.EpisodePath(externalID, n)
			origin.RawQuery = ""
			origin.Fragment = ""
			found[n] = Episode{No: n, Title: episodeTitle(n), PlayURL: origin.String()}
		}
	}
	if _, ok := found[1]; !ok {
		origin := *base
		origin.Path = playurl.WorkPath(externalID)
		origin.RawQuery = ""
		origin.Fragment = ""
		found[1] = Episode{No: 1, Title: episodeTitle(1), PlayURL: origin.String()}
	}
	out := make([]Episode, 0, len(found))
	for _, ep := range found {
		if !playurl.IsOfficial(ep.PlayURL) {
			continue
		}
		out = append(out, ep)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].No < out[j].No })
	return out
}

func episodeTitle(n int) string {
	return "第" + strconv.Itoa(n) + "集"
}

func parseTags(tags, links json.RawMessage) []string {
	var out []string
	seen := map[string]struct{}{}
	add := func(s string) {
		s = clip(plain(s), 40)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	appendNames(tags, add)
	appendNames(links, add)
	return out
}

func appendNames(raw json.RawMessage, add func(string)) {
	if len(raw) == 0 {
		return
	}
	var strs []string
	if json.Unmarshal(raw, &strs) == nil {
		for _, s := range strs {
			add(s)
		}
		return
	}
	var objs []struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &objs) == nil {
		for _, o := range objs {
			add(o.Name)
		}
	}
}

func metaContent(page, key string) string {
	for _, tag := range metaRe.FindAllString(page, -1) {
		prop := attr(tag, "property")
		name := attr(tag, "name")
		if strings.EqualFold(prop, key) || strings.EqualFold(name, key) {
			return attr(tag, "content")
		}
	}
	return ""
}

func attr(tag, name string) string {
	re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(name) + `\s*=\s*("([^"]*)"|'([^']*)')`)
	m := re.FindStringSubmatch(tag)
	if m == nil {
		return ""
	}
	if strings.HasPrefix(m[1], `"`) {
		return html.UnescapeString(m[2])
	}
	return html.UnescapeString(m[3])
}

func plain(s string) string {
	s = html.UnescapeString(s)
	s = tagRe.ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(s), " ")
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func parseCount(s string) int64 {
	s = strings.TrimSpace(strings.ToLower(strings.ReplaceAll(s, ",", "")))
	if s == "" {
		return 0
	}
	mult := 1.0
	switch {
	case strings.HasSuffix(s, "亿"):
		mult = 100000000
		s = strings.TrimSuffix(s, "亿")
	case strings.HasSuffix(s, "万"), strings.HasSuffix(s, "w"):
		mult = 10000
		s = strings.TrimSuffix(strings.TrimSuffix(s, "万"), "w")
	case strings.HasSuffix(s, "千"):
		mult = 1000
		s = strings.TrimSuffix(s, "千")
	case strings.HasSuffix(s, "k"):
		mult = 1000
		s = strings.TrimSuffix(s, "k")
	}
	s = strings.TrimSpace(s)
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int64(math.Round(n * mult))
}

func safeImage(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || !u.IsAbs() {
		return ""
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	path := strings.ToLower(u.Path)
	if path == "/api" || strings.HasPrefix(path, "/api/") || strings.Contains(path, "/api/") {
		return ""
	}
	switch {
	case strings.HasSuffix(path, ".jpg"), strings.HasSuffix(path, ".jpeg"),
		strings.HasSuffix(path, ".png"), strings.HasSuffix(path, ".webp"),
		strings.HasSuffix(path, ".gif"):
	default:
		return ""
	}
	if len(u.String()) > 1000 {
		return ""
	}
	return u.String()
}

// PublishedUTC exposes the helper for tests that want the zone conversion.
func PublishedUTC(value string) *time.Time {
	return timeutil.ParseSourceTime(value)
}
