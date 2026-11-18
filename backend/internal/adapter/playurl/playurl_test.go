package playurl

import (
	"net/url"
	"testing"
)

func TestNormalizeOfficialPagesOnly(t *testing.T) {
	base, _ := url.Parse("https://catalog.example")
	cases := []struct {
		raw     string
		ok      bool
		episode int
		path    string
	}{
		{"/video/12/", true, 1, "/video/12/"},
		{"/video/12/ep-1/", true, 1, "/video/12/"},
		{"/video/12/ep-2/", true, 2, "/video/12/ep-2/"},
		{"https://catalog.example/video/12/ep-10", true, 10, "/video/12/ep-10/"},
		{"/api/video/12/", false, 0, ""},
		{"/videos/embed/12", false, 0, ""},
		{"https://cdn.example/a.m3u8", false, 0, ""},
		{"/video/12/?play=1", false, 0, ""},
		{"/video/12/ep-2/?x=1", false, 0, ""},
		{"https://other.example/video/12/", false, 0, ""},
	}
	for _, tc := range cases {
		link, ok := Normalize(base, tc.raw)
		if ok != tc.ok {
			t.Fatalf("%s ok=%v want %v", tc.raw, ok, tc.ok)
		}
		if !ok {
			continue
		}
		if link.EpisodeNo != tc.episode {
			t.Fatalf("%s episode %d want %d", tc.raw, link.EpisodeNo, tc.episode)
		}
		u, _ := url.Parse(link.URL)
		if u.Path != tc.path || u.RawQuery != "" || u.Host != "catalog.example" {
			t.Fatalf("%s -> %s", tc.raw, link.URL)
		}
		if !IsOfficial(link.URL) {
			t.Fatalf("normalized URL not official: %s", link.URL)
		}
	}
}
