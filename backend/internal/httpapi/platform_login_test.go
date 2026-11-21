package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"catalog/internal/config"
	"catalog/internal/secretbox"
	"catalog/internal/store/memstore"
	"catalog/internal/syncer"
)

func TestPlatformLoginConfig(t *testing.T) {
	cfg := config.Config{SessionSecret: "test-session-secret-value"}
	st := memstore.New()
	srv := New(cfg, st, syncer.New(st, 0), nil)

	body := `{"code":"demo","name":"Demo","base_url":"https://example.com",` +
		`"login_url":"https://example.com/login","login_username":"vip",` +
		`"login_password":"p@ss-中文","login_user_selector":"#u",` +
		`"login_pass_selector":"#p","login_submit_selector":"#go",` +
		`"cookie":"a=1; b=2","browser_profile":"/tmp/profile"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/platforms", strings.NewReader(body))
	p, flags, err := srv.readPlatform(req, 0)
	if err != nil {
		t.Fatal(err)
	}
	if flags.ClearLoginPassword || flags.ClearCookie {
		t.Fatal("unexpected clear flags")
	}
	if p.LoginPasswordEnc == "" || strings.Contains(p.LoginPasswordEnc, "p@ss") {
		t.Fatalf("password not encrypted: %q", p.LoginPasswordEnc)
	}
	plain, err := secretbox.Decrypt(cfg.SessionSecret, p.LoginPasswordEnc)
	if err != nil || plain != "p@ss-中文" {
		t.Fatalf("decrypt: %q %v", plain, err)
	}
	if p.LoginURL != "https://example.com/login" || p.LoginUserSelector != "#u" || p.Cookie != "a=1; b=2" {
		t.Fatalf("fields: %+v", p)
	}

	out := platformJSON(p)
	if _, ok := out["login_password"]; ok {
		t.Fatal("password leaked in JSON")
	}
	if _, ok := out["cookie"]; ok {
		t.Fatal("cookie leaked in JSON")
	}
	if out["has_login_password"] != true || out["has_cookie"] != true {
		t.Fatalf("flags: %+v", out)
	}
}
