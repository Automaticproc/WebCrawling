// Package httpapi is the login-gated admin API.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"catalog/internal/auth"
	"catalog/internal/config"
	"catalog/internal/download"
	"catalog/internal/schedule"
	"catalog/internal/secretbox"
	"catalog/internal/store"
	"catalog/internal/syncer"
	"catalog/internal/timeutil"
)

const cookieName = "catalog_session"

type Server struct {
	cfg   config.Config
	store store.Store
	sync  *syncer.Service
	dl    *download.Service
}

func New(cfg config.Config, st store.Store, sync *syncer.Service, dl *download.Service) *Server {
	return &Server{cfg: cfg, store: st, sync: sync, dl: dl}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.health)
	mux.HandleFunc("POST /api/v1/login", s.login)
	mux.HandleFunc("POST /api/v1/logout", s.protected(s.logout))
	mux.HandleFunc("GET /api/v1/me", s.protected(s.me))
	mux.HandleFunc("GET /api/v1/platforms", s.protected(s.listPlatforms))
	mux.HandleFunc("POST /api/v1/platforms", s.protected(s.createPlatform))
	mux.HandleFunc("GET /api/v1/platforms/{id}", s.protected(s.getPlatform))
	mux.HandleFunc("PUT /api/v1/platforms/{id}", s.protected(s.updatePlatform))
	mux.HandleFunc("DELETE /api/v1/platforms/{id}", s.protected(s.deletePlatform))
	mux.HandleFunc("POST /api/v1/platforms/{id}/sync", s.protected(s.syncPlatform))
	mux.HandleFunc("GET /api/v1/works", s.protected(s.listWorks))
	mux.HandleFunc("GET /api/v1/works/{id}", s.protected(s.getWork))
	mux.HandleFunc("POST /api/v1/works/{id}/downloads", s.protected(s.startDownload))
	mux.HandleFunc("GET /api/v1/downloads", s.protected(s.listDownloads))
	mux.HandleFunc("GET /api/v1/downloads/{id}", s.protected(s.getDownload))
	mux.HandleFunc("POST /api/v1/downloads/{id}/cancel", s.protected(s.cancelDownload))
	mux.HandleFunc("POST /api/v1/downloads/{id}/retry", s.protected(s.retryDownload))
	mux.HandleFunc("GET /api/v1/downloads/{id}/items/{itemID}/file", s.protected(s.downloadFile))
	mux.HandleFunc("GET /api/v1/sync-jobs", s.protected(s.listJobs))
	mux.HandleFunc("GET /api/v1/sync-jobs/{id}", s.protected(s.getJob))
	mux.HandleFunc("GET /api/v1/sync-jobs/{id}/items", s.protected(s.listJobItems))
	mux.HandleFunc("GET /api/v1/skip-rules", s.protected(s.listRules))
	mux.HandleFunc("POST /api/v1/skip-rules", s.protected(s.createRule))
	mux.HandleFunc("PUT /api/v1/skip-rules/{id}", s.protected(s.updateRule))
	mux.HandleFunc("DELETE /api/v1/skip-rules/{id}", s.protected(s.deleteRule))
	return s.withCORS(mux)
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && origin == s.cfg.WebOrigin {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Requested-With")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) protected(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-Requested-With") != "catalog" {
			writeError(w, http.StatusBadRequest, "缺少请求头")
			return
		}
		username, err := s.username(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "请先登录")
			return
		}
		if _, err := s.store.GetUserByUsername(r.Context(), username); err != nil {
			writeError(w, http.StatusUnauthorized, "请先登录")
			return
		}
		ctx := context.WithValue(r.Context(), userKey{}, username)
		next(w, r.WithContext(ctx))
	}
}

type userKey struct{}

func (s *Server) username(r *http.Request) (string, error) {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return "", auth.ErrSession
	}
	return auth.Verify(s.cfg.SessionSecret, c.Value, time.Now())
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Requested-With") != "catalog" {
		writeError(w, http.StatusBadRequest, "缺少请求头")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式无效")
		return
	}
	user, err := s.store.GetUserByUsername(r.Context(), strings.TrimSpace(body.Username))
	if err != nil || !auth.Check(user.PasswordHash, body.Password) {
		writeError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	exp := time.Now().Add(auth.SessionTTL)
	token, err := auth.Sign(s.cfg.SessionSecret, user.Username, exp)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法创建会话")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cfg.CookieSecure,
		Expires:  exp,
	})
	writeJSON(w, http.StatusOK, map[string]any{"username": user.Username, "timezone": "Asia/Hong_Kong"})
}

func (s *Server) logout(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: "/", HttpOnly: true,
		Expires: time.Unix(0, 0), MaxAge: -1, SameSite: http.SameSiteLaxMode, Secure: s.cfg.CookieSecure,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	name, _ := r.Context().Value(userKey{}).(string)
	writeJSON(w, http.StatusOK, map[string]any{"username": name, "timezone": "Asia/Hong_Kong"})
}

func (s *Server) listPlatforms(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListPlatforms(r.Context())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	out := make([]any, 0, len(items))
	for _, p := range items {
		out = append(out, platformJSON(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) getPlatform(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPlatform(r.Context(), pathID(r))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, platformJSON(p))
}

func (s *Server) createPlatform(w http.ResponseWriter, r *http.Request) {
	p, _, err := s.readPlatform(r, 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := s.store.CreatePlatform(r.Context(), p)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, platformJSON(created))
}

func (s *Server) updatePlatform(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	existing, err := s.store.GetPlatform(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	p, flags, err := s.readPlatform(r, id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	p.ID = id
	if p.LoginPasswordEnc == "" && !flags.ClearLoginPassword {
		p.LoginPasswordEnc = existing.LoginPasswordEnc
	}
	if p.Cookie == "" && !flags.ClearCookie {
		p.Cookie = existing.Cookie
	}
	if err := s.store.UpdatePlatform(r.Context(), p); err != nil {
		writeStoreErr(w, err)
		return
	}
	updated, err := s.store.GetPlatform(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, platformJSON(updated))
}

func (s *Server) deletePlatform(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeletePlatform(r.Context(), pathID(r)); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) syncPlatform(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode string `json:"mode"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式无效")
		return
	}
	if body.Mode != store.ModeFull && body.Mode != store.ModeIncremental {
		writeError(w, http.StatusBadRequest, "模式只能是 full 或 incremental")
		return
	}
	job, err := s.sync.Start(r.Context(), pathID(r), body.Mode, store.TriggerManual, false)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeStoreErr(w, err)
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, jobJSON(job))
}

func (s *Server) listWorks(w http.ResponseWriter, r *http.Request) {
	page, size := pageQuery(r)
	platformID, _ := strconv.ParseInt(r.URL.Query().Get("platform_id"), 10, 64)
	items, total, err := s.store.ListWorks(r.Context(), store.WorkFilter{
		PlatformID: platformID, Query: r.URL.Query().Get("q"), Page: page, PageSize: size,
	})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, workJSON(item, false))
	}
	writeJSON(w, http.StatusOK, pageBody(out, total, page, size))
}

func (s *Server) getWork(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.GetWork(r.Context(), pathID(r))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, workJSON(item, true))
}

var qualityRe = regexp.MustCompile(`^(best|worst|[0-9]{3,4}p?)$`)

func (s *Server) startDownload(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		writeError(w, http.StatusServiceUnavailable, "下载服务未启用")
		return
	}
	var body struct {
		Quality string `json:"quality"`
	}
	if r.ContentLength != 0 {
		if err := decode(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "请求格式无效")
			return
		}
	}
	if body.Quality == "" {
		body.Quality = "best"
	}
	if !qualityRe.MatchString(body.Quality) {
		writeError(w, http.StatusBadRequest, "画质只能是 best / worst / 720p / 1080p")
		return
	}
	task, err := s.dl.StartWork(r.Context(), pathID(r), body.Quality)
	if err != nil {
		switch {
		case errors.Is(err, download.ErrRunning):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, store.ErrNotFound):
			writeStoreErr(w, err)
		default:
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusAccepted, downloadJSON(task, download.Progress{}, false))
}

func (s *Server) listDownloads(w http.ResponseWriter, r *http.Request) {
	page, size := pageQuery(r)
	workID, _ := strconv.ParseInt(r.URL.Query().Get("work_id"), 10, 64)
	items, total, err := s.store.ListDownloadTasks(r.Context(), workID, page, size)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	out := make([]any, 0, len(items))
	for _, t := range items {
		out = append(out, downloadJSON(t, download.Progress{}, false))
	}
	writeJSON(w, http.StatusOK, pageBody(out, total, page, size))
}

func (s *Server) getDownload(w http.ResponseWriter, r *http.Request) {
	task, err := s.store.GetDownloadTask(r.Context(), pathID(r))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	var (
		prog download.Progress
		ok   bool
	)
	if s.dl != nil {
		prog, ok = s.dl.Snapshot(task.ID)
	}
	writeJSON(w, http.StatusOK, downloadJSON(task, prog, ok))
}

func (s *Server) cancelDownload(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		writeError(w, http.StatusServiceUnavailable, "下载服务未启用")
		return
	}
	if err := s.dl.Cancel(r.Context(), pathID(r)); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

func (s *Server) retryDownload(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		writeError(w, http.StatusServiceUnavailable, "下载服务未启用")
		return
	}
	task, err := s.dl.RetryTask(r.Context(), pathID(r))
	if err != nil {
		switch {
		case errors.Is(err, download.ErrRunning):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, download.ErrNothingToRetry):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, store.ErrNotFound):
			writeStoreErr(w, err)
		default:
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusAccepted, downloadJSON(task, download.Progress{}, false))
}

func (s *Server) downloadFile(w http.ResponseWriter, r *http.Request) {
	taskID := pathID(r)
	itemID, _ := strconv.ParseInt(r.PathValue("itemID"), 10, 64)
	items, err := s.store.ListDownloadItems(r.Context(), taskID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	var target string
	for _, it := range items {
		if it.ID == itemID {
			target = it.FilePath
			break
		}
	}
	if target == "" {
		writeError(w, http.StatusNotFound, "文件不存在")
		return
	}
	root, err := filepath.Abs(s.cfg.DownloadDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "下载目录配置无效")
		return
	}
	abs, err := filepath.Abs(target)
	if err != nil || !strings.HasPrefix(abs, root+string(os.PathSeparator)) {
		writeError(w, http.StatusNotFound, "文件不存在")
		return
	}
	if _, err := os.Stat(abs); err != nil {
		writeError(w, http.StatusNotFound, "文件不存在")
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(abs)+`"`)
	http.ServeFile(w, r, abs)
}

func downloadJSON(t store.DownloadTask, prog download.Progress, hasProg bool) map[string]any {
	items := make([]any, 0, len(t.Items))
	for _, it := range t.Items {
		item := map[string]any{
			"id": it.ID, "task_id": it.TaskID, "episode_no": it.EpisodeNo, "title": it.Title,
			"page_url": it.PageURL, "stream_url": it.StreamURL, "size_bytes": it.SizeBytes,
			"duration_seconds": it.DurationSeconds, "status": it.Status, "error_message": it.ErrorMessage,
		}
		if it.Status == store.DownloadSucceeded && it.FilePath != "" {
			item["file_url"] = "/api/v1/downloads/" + strconv.FormatInt(t.ID, 10) +
				"/items/" + strconv.FormatInt(it.ID, 10) + "/file"
		}
		items = append(items, item)
	}
	out := map[string]any{
		"id": t.ID, "work_id": t.WorkID, "platform_id": t.PlatformID, "status": t.Status,
		"total_episodes": t.TotalEpisodes, "completed_episodes": t.CompletedEpisodes,
		"failed_episodes": t.FailedEpisodes, "quality": t.Quality, "output_dir": t.OutputDir,
		"error_message": t.ErrorMessage, "started_at": utcPtr(t.StartedAt), "finished_at": utcPtr(t.FinishedAt),
		"created_at": t.CreatedAt.UTC(), "updated_at": t.UpdatedAt.UTC(), "items": items,
	}
	if hasProg {
		out["progress"] = prog
	}
	return out
}

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	page, size := pageQuery(r)
	platformID, _ := strconv.ParseInt(r.URL.Query().Get("platform_id"), 10, 64)
	items, total, err := s.store.ListJobs(r.Context(), platformID, page, size)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, jobJSON(item))
	}
	writeJSON(w, http.StatusOK, pageBody(out, total, page, size))
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.GetJob(r.Context(), pathID(r))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, jobJSON(item))
}

func (s *Server) listJobItems(w http.ResponseWriter, r *http.Request) {
	page, size := pageQuery(r)
	items, total, err := s.store.ListJobItems(r.Context(), pathID(r), page, size)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, itemJSON(item))
	}
	writeJSON(w, http.StatusOK, pageBody(out, total, page, size))
}

func (s *Server) listRules(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListSkipRules(r.Context())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, ruleJSON(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) createRule(w http.ResponseWriter, r *http.Request) {
	rule, err := readRule(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := s.store.CreateSkipRule(r.Context(), rule)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, ruleJSON(created))
}

func (s *Server) updateRule(w http.ResponseWriter, r *http.Request) {
	rule, err := readRule(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rule.ID = pathID(r)
	if err := s.store.UpdateSkipRule(r.Context(), rule); err != nil {
		writeStoreErr(w, err)
		return
	}
	updated, err := s.store.GetSkipRule(r.Context(), rule.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ruleJSON(updated))
}

func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteSkipRule(r.Context(), pathID(r)); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

var codeRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,31}$`)

type platformFlags struct {
	ClearLoginPassword bool
	ClearCookie        bool
}

func (s *Server) readPlatform(r *http.Request, id int64) (store.Platform, platformFlags, error) {
	var body struct {
		Code                string `json:"code"`
		Name                string `json:"name"`
		BaseURL             string `json:"base_url"`
		Enabled             *bool  `json:"enabled"`
		CronExpr            string `json:"cron_expr"`
		SitemapPath         string `json:"sitemap_path"`
		LoginURL            string `json:"login_url"`
		LoginUsername       string `json:"login_username"`
		LoginPassword       string `json:"login_password"`
		LoginUserSelector   string `json:"login_user_selector"`
		LoginPassSelector   string `json:"login_pass_selector"`
		LoginSubmitSelector string `json:"login_submit_selector"`
		BrowserProfile      string `json:"browser_profile"`
		Cookie              string `json:"cookie"`
		ClearLoginPassword  bool   `json:"clear_login_password"`
		ClearCookie         bool   `json:"clear_cookie"`
	}
	if err := decode(r, &body); err != nil {
		return store.Platform{}, platformFlags{}, errors.New("请求格式无效")
	}
	body.Code = strings.TrimSpace(body.Code)
	body.Name = strings.TrimSpace(body.Name)
	if !codeRe.MatchString(body.Code) {
		return store.Platform{}, platformFlags{}, errors.New("平台代码需为 2-32 位小写字母、数字、下划线或连字符")
	}
	if body.Name == "" || len([]rune(body.Name)) > 128 {
		return store.Platform{}, platformFlags{}, errors.New("请填写平台名称")
	}
	origin, err := originOnly(body.BaseURL)
	if err != nil {
		return store.Platform{}, platformFlags{}, err
	}
	cronExpr := strings.TrimSpace(body.CronExpr)
	if cronExpr == "" {
		cronExpr = timeutil.DefaultCron
	}
	if !schedule.Valid(cronExpr) {
		return store.Platform{}, platformFlags{}, errors.New("Cron 需要 5 段，并按香港时间解释")
	}
	path := strings.TrimSpace(body.SitemapPath)
	if path == "" {
		path = "/sitemap.xml"
	}
	if !strings.HasPrefix(path, "/") || strings.Contains(path, "..") || strings.Contains(path, "?") || strings.HasPrefix(strings.ToLower(path), "/api") {
		return store.Platform{}, platformFlags{}, errors.New("站点地图路径无效")
	}

	loginURL := strings.TrimSpace(body.LoginURL)
	if loginURL != "" {
		u, err := url.Parse(loginURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(loginURL) > 1024 {
			return store.Platform{}, platformFlags{}, errors.New("登录地址需要是 http 或 https 完整地址")
		}
	}
	fields := map[string]string{
		"login_username": body.LoginUsername, "login_password": body.LoginPassword,
		"login_user_selector": body.LoginUserSelector, "login_pass_selector": body.LoginPassSelector,
		"login_submit_selector": body.LoginSubmitSelector, "browser_profile": body.BrowserProfile,
		"cookie": body.Cookie,
	}
	limits := map[string]int{
		"login_username": 200, "login_password": 500, "login_user_selector": 200,
		"login_pass_selector": 200, "login_submit_selector": 200, "browser_profile": 1000, "cookie": 4000,
	}
	for k, v := range fields {
		if len([]rune(v)) > limits[k] {
			return store.Platform{}, platformFlags{}, errors.New("登录配置字段过长：" + k)
		}
	}

	enc, err := secretbox.Encrypt(s.cfg.SessionSecret, body.LoginPassword)
	if err != nil {
		return store.Platform{}, platformFlags{}, errors.New("登录密码加密失败")
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	return store.Platform{
		ID: id, Code: body.Code, Name: body.Name, BaseURL: origin, Enabled: enabled, CronExpr: cronExpr, SitemapPath: path,
		LoginURL: loginURL, LoginUsername: strings.TrimSpace(body.LoginUsername), LoginPasswordEnc: enc,
		LoginUserSelector:   strings.TrimSpace(body.LoginUserSelector),
		LoginPassSelector:   strings.TrimSpace(body.LoginPassSelector),
		LoginSubmitSelector: strings.TrimSpace(body.LoginSubmitSelector),
		BrowserProfile:      strings.TrimSpace(body.BrowserProfile), Cookie: strings.TrimSpace(body.Cookie),
	}, platformFlags{ClearLoginPassword: body.ClearLoginPassword, ClearCookie: body.ClearCookie}, nil
}

func originOnly(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", errors.New("平台地址需要是 http 或 https 源站")
	}
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func readRule(r *http.Request) (store.SkipRule, error) {
	var body struct {
		PlatformID *int64 `json:"platform_id"`
		Name       string `json:"name"`
		RuleType   string `json:"rule_type"`
		Target     string `json:"target"`
		Pattern    string `json:"pattern"`
		Reason     string `json:"reason"`
		Enabled    *bool  `json:"enabled"`
		Priority   int    `json:"priority"`
	}
	if err := decode(r, &body); err != nil {
		return store.SkipRule{}, errors.New("请求格式无效")
	}
	body.Name = strings.TrimSpace(body.Name)
	body.Pattern = strings.TrimSpace(body.Pattern)
	body.Reason = strings.TrimSpace(body.Reason)
	if body.Name == "" || body.Reason == "" {
		return store.SkipRule{}, errors.New("请填写规则名称和原因")
	}
	if body.RuleType != store.RuleKeyword && body.RuleType != store.RuleRegex {
		return store.SkipRule{}, errors.New("规则类型只能是 keyword 或 regex")
	}
	switch body.Target {
	case store.TargetTitle, store.TargetTags, store.TargetDescription, store.TargetAll:
	default:
		return store.SkipRule{}, errors.New("匹配范围无效")
	}
	if body.RuleType == store.RuleRegex {
		if _, err := regexp.Compile(body.Pattern); err != nil || len(body.Pattern) > 200 {
			return store.SkipRule{}, errors.New("正则无效或过长")
		}
	} else if len([]rune(body.Pattern)) == 0 || len([]rune(body.Pattern)) > 100 {
		return store.SkipRule{}, errors.New("关键词长度需在 1 到 100 之间")
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	if body.Priority == 0 {
		body.Priority = 100
	}
	return store.SkipRule{
		PlatformID: body.PlatformID, Name: body.Name, RuleType: body.RuleType, Target: body.Target,
		Pattern: body.Pattern, Reason: body.Reason, Enabled: enabled, Priority: body.Priority,
	}, nil
}

func platformJSON(p store.Platform) map[string]any {
	var next *time.Time
	if t, err := schedule.Next(p.CronExpr, time.Now()); err == nil {
		u := t.UTC()
		next = &u
	}
	return map[string]any{
		"id": p.ID, "code": p.Code, "name": p.Name, "base_url": p.BaseURL, "enabled": p.Enabled,
		"cron_expr": p.CronExpr, "sitemap_path": p.SitemapPath, "supported": p.Code == "huangguoai",
		"last_synced_at": utcPtr(p.LastSyncedAt), "next_run_at": next,
		"created_at": p.CreatedAt.UTC(), "updated_at": p.UpdatedAt.UTC(),
		"login_url": p.LoginURL, "login_username": p.LoginUsername,
		"login_user_selector": p.LoginUserSelector, "login_pass_selector": p.LoginPassSelector,
		"login_submit_selector": p.LoginSubmitSelector, "browser_profile": p.BrowserProfile,
		"has_login_password": p.LoginPasswordEnc != "", "has_cookie": p.Cookie != "",
	}
}

func workJSON(w store.Work, withEpisodes bool) map[string]any {
	item := map[string]any{
		"id": w.ID, "platform_id": w.PlatformID, "external_id": w.ExternalID, "title": w.Title,
		"description": w.Description, "cover_url": w.CoverURL, "category": w.Category, "tags": nonNil(w.Tags),
		"author": w.Author, "play_url": w.PlayURL, "published_at": utcPtr(w.PublishedAt),
		"view_count": w.ViewCount, "like_count": w.LikeCount, "favorite_count": w.FavoriteCount,
		"comment_count": w.CommentCount, "episode_count": w.EpisodeCount, "absent_count": w.AbsentCount,
		"last_seen_at": utcPtr(w.LastSeenAt), "created_at": w.CreatedAt.UTC(), "updated_at": w.UpdatedAt.UTC(),
	}
	if withEpisodes {
		eps := make([]any, 0, len(w.Episodes))
		for _, ep := range w.Episodes {
			eps = append(eps, map[string]any{"id": ep.ID, "episode_no": ep.EpisodeNo, "title": ep.Title, "play_url": ep.PlayURL})
		}
		item["episodes"] = eps
	}
	return item
}

func jobJSON(j store.SyncJob) map[string]any {
	return map[string]any{
		"id": j.ID, "platform_id": j.PlatformID, "mode": j.Mode, "trigger_type": j.TriggerType, "status": j.Status,
		"skip_reason": j.SkipReason, "started_at": utcPtr(j.StartedAt), "finished_at": utcPtr(j.FinishedAt),
		"discovered_count": j.DiscoveredCount, "created_count": j.CreatedCount, "updated_count": j.UpdatedCount,
		"skipped_count": j.SkippedCount, "failed_count": j.FailedCount, "absent_count": j.AbsentCount,
		"error_message": j.ErrorMessage, "created_at": j.CreatedAt.UTC(),
	}
}

func itemJSON(it store.SyncJobItem) map[string]any {
	return map[string]any{
		"id": it.ID, "job_id": it.JobID, "external_id": it.ExternalID, "item_title": it.ItemTitle,
		"action": it.Action, "reason": it.Reason, "play_url": it.PlayURL, "created_at": it.CreatedAt.UTC(),
	}
}

func ruleJSON(r store.SkipRule) map[string]any {
	return map[string]any{
		"id": r.ID, "platform_id": r.PlatformID, "name": r.Name, "rule_type": r.RuleType, "target": r.Target,
		"pattern": r.Pattern, "reason": r.Reason, "enabled": r.Enabled, "locked": r.Locked, "priority": r.Priority,
		"created_at": r.CreatedAt.UTC(), "updated_at": r.UpdatedAt.UTC(),
	}
}

func pageBody(items []any, total, page, size int) map[string]any {
	return map[string]any{"items": items, "total": total, "page": page, "page_size": size}
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil || t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}

func nonNil(tags []string) []string {
	if tags == nil {
		return []string{}
	}
	return tags
}

func pathID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}

func pageQuery(r *http.Request) (int, int) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return page, size
}

func decode(r *http.Request, dest any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(dest)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

func writeStoreErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "不存在")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "已存在")
	case errors.Is(err, store.ErrRuleLocked):
		writeError(w, http.StatusForbidden, store.ErrRuleLocked.Error())
	default:
		writeError(w, http.StatusInternalServerError, "服务器错误")
	}
}
