package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"catalog/internal/auth"
	"catalog/internal/config"
	"catalog/internal/store"
	"catalog/internal/store/memstore"
	"catalog/internal/syncer"
)

func TestLoginRequiredAndBuiltinRuleLocked(t *testing.T) {
	st := memstore.New()
	hash, err := auth.Hash("correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateUser(context.Background(), "admin", hash); err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureBuiltinRule(context.Background(), store.SkipRule{
		Name: "未成年与未满21岁", RuleType: store.RuleBuiltin, Target: store.TargetAll,
		Pattern: "builtin:minors-under-21", Reason: "疑似未成年或未满21岁相关内容", Enabled: true, Locked: true,
	}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{SessionSecret: "test-session-secret-value", WebOrigin: "http://127.0.0.1:43124"}
	h := New(cfg, st, syncer.New(st, 0), nil).Handler()

	res := httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v1/platforms", nil))
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("platforms without session: %d", res.Code)
	}

	body := bytes.NewBufferString(`{"username":"admin","password":"wrong-password"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/login", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "catalog")
	res = httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("bad password: %d %s", res.Code, res.Body.String())
	}

	body = bytes.NewBufferString(`{"username":"admin","password":"correct-horse"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/login", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "catalog")
	res = httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("login: %d %s", res.Code, res.Body.String())
	}
	cookie := res.Result().Cookies()
	if len(cookie) == 0 || cookie[0].Name != "catalog_session" || !cookie[0].HttpOnly {
		t.Fatalf("cookie %+v", cookie)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/skip-rules", nil)
	req.AddCookie(cookie[0])
	res = httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "未成年与未满21岁") {
		t.Fatalf("rules %d %s", res.Code, res.Body.String())
	}
	var payload struct {
		Items []struct {
			ID     int64 `json:"id"`
			Locked bool  `json:"locked"`
		} `json:"items"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) != 1 || !payload.Items[0].Locked {
		t.Fatalf("payload %+v", payload.Items)
	}
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/skip-rules/"+strconv.FormatInt(payload.Items[0].ID, 10), nil)
	req.AddCookie(cookie[0])
	req.Header.Set("X-Requested-With", "catalog")
	res = httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("delete builtin: %d %s", res.Code, res.Body.String())
	}
}
