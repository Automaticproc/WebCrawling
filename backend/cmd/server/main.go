package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
	"unicode/utf8"

	"catalog/internal/config"
	"catalog/internal/download"
	"catalog/internal/httpapi"
	"catalog/internal/migrate"
	"catalog/internal/scheduler"
	"catalog/internal/seed"
	"catalog/internal/store/mysqlstore"
	"catalog/internal/syncer"
)

func main() {
	cfg := config.Load()
	if utf8.RuneCountInString(cfg.AdminPassword) < 8 {
		log.Fatal("请在 ADMIN_PASSWORD 中设置至少 8 位的管理员密码")
	}
	if utf8.RuneCountInString(cfg.SessionSecret) < 16 {
		log.Fatal("请在 SESSION_SECRET 中设置至少 16 位的会话密钥")
	}
	db, err := mysqlstore.Open(cfg.MySQLDSN)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := db.PingContext(ctx); err != nil {
		cancel()
		log.Fatalf("无法连接 MySQL：%v", err)
	}
	if err := migrate.Up(ctx, db); err != nil {
		cancel()
		log.Fatalf("迁移失败：%v", err)
	}
	st := mysqlstore.New(db)
	if err := seed.Run(ctx, st, cfg); err != nil {
		cancel()
		log.Fatalf("初始化失败：%v", err)
	}
	cancel()

	svc := syncer.New(st, cfg.SyncDelay)
	engine := download.NewEngine(cfg.FFmpegPath, cfg.DownloadConcurrency)
	engine.TempRoot = filepath.Join(cfg.DownloadDir, ".tmp")
	resolver := download.NewResolver(cfg.BrowserPath, cfg.BrowserTimeout)
	resolver.BrowserPort = cfg.BrowserPort
	resolver.BrowserProfile = cfg.BrowserProfile
	if resolver.BrowserProfile == "" {
		// 默认持久化，登录态可以跨进程复用
		resolver.BrowserProfile = filepath.Join(cfg.DownloadDir, ".browser-profile")
	}
	resolver.Cookies = cfg.BrowserCookie
	resolver.LoginURL = cfg.LoginURL
	resolver.LoginUser = cfg.LoginUser
	resolver.LoginPassword = cfg.LoginPassword
	resolver.LoginUserSelector = cfg.LoginUserSelector
	resolver.LoginPassSelector = cfg.LoginPassSelector
	resolver.LoginSubmitSelector = cfg.LoginSubmitSelector
	resolver.TempDir = engine.TempRoot
	dl := download.NewService(st, resolver, engine, cfg.DownloadDir)
	dl.Secret = cfg.SessionSecret
	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go scheduler.New(st, svc).Run(runCtx)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           httpapi.New(cfg, st, svc, dl).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-runCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	log.Printf("目录采集台正在监听 %s（时区 Asia/Hong_Kong，默认定时增量 %s）", cfg.Addr, "0 8 * * *")
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
