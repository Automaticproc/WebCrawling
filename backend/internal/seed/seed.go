package seed

import (
	"context"
	"errors"

	"catalog/internal/auth"
	"catalog/internal/config"
	"catalog/internal/skip"
	"catalog/internal/store"
	"catalog/internal/timeutil"
)

// Run creates the first admin, the built-in skip rule, and the huangguoai platform.
// It does not start a sync.
func Run(ctx context.Context, st store.Store, cfg config.Config) error {
	n, err := st.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		hash, err := auth.Hash(cfg.AdminPassword)
		if err != nil {
			return err
		}
		if err := st.CreateUser(ctx, cfg.AdminUser, hash); err != nil {
			return err
		}
	}
	if _, err := st.GetPlatformByCode(ctx, "huangguoai"); errors.Is(err, store.ErrNotFound) {
		if _, err := st.CreatePlatform(ctx, store.Platform{
			Code:        "huangguoai",
			Name:        "黄果爱",
			BaseURL:     "https://huangguoai.com",
			Enabled:     true,
			CronExpr:    timeutil.DefaultCron,
			SitemapPath: "/sitemap.xml",
		}); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := st.EnsureBuiltinRule(ctx, skip.BuiltinRule()); err != nil {
		return err
	}
	if err := st.FailStaleJobs(ctx, "进程重启，未完成的同步已中止"); err != nil {
		return err
	}
	return st.FailStaleDownloads(ctx, "进程重启，未完成的下载已中止")
}
