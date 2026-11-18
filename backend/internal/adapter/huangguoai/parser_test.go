package huangguoai

import (
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestParserFiltersUnsafeLinks(t *testing.T) {
	base, _ := url.Parse("https://catalog.example")
	body, err := os.ReadFile("testdata/detail_ok.html")
	if err != nil {
		t.Fatal(err)
	}
	detail, err := ParseHTML(base, "https://catalog.example/video/12/", body)
	if err != nil {
		t.Fatal(err)
	}
	if detail.ExternalID != "12" || detail.Title != "虚构作品甲" {
		t.Fatalf("identity: %+v", detail)
	}
	if detail.ViewCount != 15000 || detail.LikeCount != 2150 || detail.FavoriteCount != 7728 || detail.CommentCount != 6 {
		t.Fatalf("metrics: views=%d likes=%d fav=%d comments=%d", detail.ViewCount, detail.LikeCount, detail.FavoriteCount, detail.CommentCount)
	}
	if detail.CoverURL != "https://img.example/covers/12.jpg" {
		t.Fatalf("cover: %s", detail.CoverURL)
	}
	if detail.PlayURL != "https://catalog.example/video/12/" {
		t.Fatalf("play: %s", detail.PlayURL)
	}
	if len(detail.Episodes) != 3 {
		t.Fatalf("episodes: %+v", detail.Episodes)
	}
	want := []string{
		"https://catalog.example/video/12/",
		"https://catalog.example/video/12/ep-2/",
		"https://catalog.example/video/12/ep-3/",
	}
	for i, ep := range detail.Episodes {
		if ep.PlayURL != want[i] || ep.No != i+1 {
			t.Fatalf("episode %d: %+v", i, ep)
		}
	}
	blob, _ := json.Marshal(detail)
	text := string(blob) + detail.Description + detail.Title + detail.CoverURL
	for _, bad := range []string{
		"SENTINEL_KEY_DO_NOT_STORE",
		"SENTINEL_IV_DO_NOT_STORE",
		"SENTINEL_CDN_AUTH_DO_NOT_STORE",
		".m3u8",
		"/api/",
		"/embed/",
		"token=",
	} {
		if strings.Contains(text, bad) {
			t.Fatalf("parser kept %q in %s", bad, text)
		}
	}
	if detail.PublishedAt == nil {
		t.Fatal("missing published")
	}
	wantPub := time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC)
	if !detail.PublishedAt.Equal(wantPub) {
		t.Fatalf("published %s want %s", detail.PublishedAt, wantPub)
	}
	joined := strings.Join(detail.Tags, ",")
	if joined != "短剧,都市" {
		t.Fatalf("tags %s", joined)
	}
}

func TestParseCount(t *testing.T) {
	cases := map[string]int64{
		"82.1w": 821000,
		"2.1k":  2100,
		"0.8w":  8000,
		"1.2万":  12000,
		"3亿":    300000000,
		"1,234": 1234,
		"":      0,
	}
	for in, want := range cases {
		if got := parseCount(in); got != want {
			t.Fatalf("%s -> %d want %d", in, got, want)
		}
	}
}

func TestAnchorsOnlyWhenJSONMissingEpisodes(t *testing.T) {
	base, _ := url.Parse("https://catalog.example")
	html := `<h1>只有锚点</h1><a href="/video/7/ep-2/">02</a><a href="/api/v">x</a>`
	d, err := ParseHTML(base, "/video/7/", []byte(html))
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "只有锚点" {
		t.Fatal(d.Title)
	}
	if len(d.Episodes) != 2 {
		t.Fatalf("%+v", d.Episodes)
	}
	if d.Episodes[0].PlayURL != "https://catalog.example/video/7/" {
		t.Fatal(d.Episodes[0].PlayURL)
	}
	if d.Episodes[1].PlayURL != "https://catalog.example/video/7/ep-2/" {
		t.Fatal(d.Episodes[1].PlayURL)
	}
}
