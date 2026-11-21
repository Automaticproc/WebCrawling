package download

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// TestCaptureBrowser is opt-in because it launches a real browser.
// Run with: TEST_BROWSER=1 go test ./internal/download -run TestCaptureBrowser -v
func TestCaptureBrowser(t *testing.T) {
	if os.Getenv("TEST_BROWSER") != "1" {
		t.Skip("设置 TEST_BROWSER=1 才会启动真实浏览器")
	}
	dir := t.TempDir()
	base, err := os.MkdirTemp(".", "bt-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	m3u8 := "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:4\n#EXTINF:4,\nseg0.ts\n#EXT-X-ENDLIST\n"
	if err := os.WriteFile(filepath.Join(dir, "index.m3u8"), []byte(m3u8), 0o644); err != nil {
		t.Fatal(err)
	}
	media := httptest.NewServer(http.FileServer(http.Dir(dir)))
	defer media.Close()

	target := media.URL + "/index.m3u8"
	encoded := base64.StdEncoding.EncodeToString([]byte(target))
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `<!doctype html><html><body><script>
setTimeout(function(){ fetch(atob(%q)); }, 600);
</script></body></html>`, encoded)
	}))
	defer page.Close()

	res, err := captureStream(context.Background(), browserOptions{
		PageURL: page.URL, Timeout: 25 * time.Second, TempDir: base,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.URL != target {
		t.Fatalf("captured %q, want %q", res.URL, target)
	}
}

// TestBrowserLogin 覆盖“自动登录后抓 VIP 页”的流程：目标页只有带登录 Cookie 才下发 m3u8。
// Run with: TEST_BROWSER=1 go test ./internal/download -run TestBrowserLogin -v
func TestBrowserLogin(t *testing.T) {
	if os.Getenv("TEST_BROWSER") != "1" {
		t.Skip("设置 TEST_BROWSER=1 才会启动真实浏览器")
	}
	base, err := os.MkdirTemp(".", "bt-login-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })

	m3u8 := "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:4\n#EXTINF:4,\nseg0.ts\n#EXT-X-ENDLIST\n"
	mux := http.NewServeMux()
	var site string
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_ = r.ParseForm()
			if r.FormValue("u") == "vip" && r.FormValue("p") == "secret" {
				http.SetCookie(w, &http.Cookie{Name: "vip", Value: "1", Path: "/"})
				_, _ = w.Write([]byte("logged in"))
				return
			}
			http.Error(w, "bad credentials", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`<!doctype html><html><body>
<form method="post" action="/login">
<input type="text" name="u"><input type="password" name="p">
<button type="submit">登录</button>
</form></body></html>`))
	})
	mux.HandleFunc("/video/1/", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("vip"); err != nil || c.Value != "1" {
			_, _ = w.Write([]byte(`<!doctype html><html><body>请先登录</body></html>`))
			return
		}
		fmt.Fprintf(w, `<!doctype html><html><body><script>fetch(%q);</script></body></html>`, site+"/index.m3u8")
	})
	mux.HandleFunc("/index.m3u8", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(m3u8))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	site = srv.URL

	res, err := captureStream(context.Background(), browserOptions{
		PageURL:       srv.URL + "/video/1/",
		Timeout:       20 * time.Second,
		TempDir:       base,
		LoginURL:      srv.URL + "/login",
		LoginUser:     "vip",
		LoginPassword: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.URL != srv.URL+"/index.m3u8" {
		t.Fatalf("captured %q", res.URL)
	}
}

// TestCaptureOnPort reuses an already-running Chrome (started by a launcher that
// works on this machine) so the DevTools client itself can be exercised.
// Run with: CHROME_PORT=9222 go test ./internal/download -run TestCaptureOnPort -v
func TestCaptureOnPort(t *testing.T) {
	portStr := os.Getenv("CHROME_PORT")
	if portStr == "" {
		t.Skip("设置 CHROME_PORT 才会连接已有浏览器")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 {
		t.Fatalf("CHROME_PORT 无效：%s", portStr)
	}
	dir := t.TempDir()
	m3u8 := "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:4\n#EXTINF:4,\nseg0.ts\n#EXT-X-ENDLIST\n"
	if err := os.WriteFile(filepath.Join(dir, "index.m3u8"), []byte(m3u8), 0o644); err != nil {
		t.Fatal(err)
	}
	media := httptest.NewServer(http.FileServer(http.Dir(dir)))
	defer media.Close()
	target := media.URL + "/index.m3u8"
	encoded := base64.StdEncoding.EncodeToString([]byte(target))
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `<!doctype html><html><body><script>
setTimeout(function(){ fetch(atob(%q)); }, 600);
</script></body></html>`, encoded)
	}))
	defer page.Close()

	res, err := captureOnPort(context.Background(), port, browserOptions{
		PageURL: page.URL, Timeout: 25 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.URL != target {
		t.Fatalf("captured %q, want %q", res.URL, target)
	}
}
