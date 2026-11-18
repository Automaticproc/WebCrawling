package policy

import (
	"net/url"
	"testing"
)

func TestRejectsAPIMediaAndEmbed(t *testing.T) {
	base, _ := url.Parse("https://catalog.example")
	cases := []struct {
		raw  string
		kind Kind
		ok   bool
	}{
		{"/sitemap.xml", KindAny, true},
		{"/sitemap/video/1.xml", KindVideoList, true},
		{"/video/12/", KindDetail, true},
		{"/video/12", KindDetail, true},
		{"/api/", KindAny, false},
		{"/api/play/12", KindAny, false},
		{"https://catalog.example/api/sitemap.xml", KindVideoList, false},
		{"/videos/embed/12", KindAny, false},
		{"/stream/a.m3u8", KindAny, false},
		{"/video/12/?token=1", KindDetail, false},
		{"https://cdn.example/a.mp4", KindAny, false},
		{"/admin/", KindAny, false},
		{"/search/", KindAny, false},
		{"/upload/a.jpg", KindAny, false},
		{"/video/12/ep-2/", KindDetail, false},
	}
	for _, tc := range cases {
		_, err := Check(base, tc.raw, tc.kind)
		if tc.ok && err != nil {
			t.Errorf("%s kind %d: %v", tc.raw, tc.kind, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("%s should be denied", tc.raw)
		}
		if err != nil && err.Error() != ErrDenied.Error() {
			t.Errorf("error leaked detail: %v", err)
		}
	}
}
