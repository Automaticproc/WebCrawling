package mysqlstore

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"catalog/internal/migrate"
	"catalog/internal/store"
)

func TestMySQLRoundTrip(t *testing.T) {
	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		dsn = "catalog:catalog@tcp(127.0.0.1:3306)/catalog_test?parseTime=true&charset=utf8mb4&loc=UTC&clientFoundRows=true"
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Skip(err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Skip(err)
	}
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `SET FOREIGN_KEY_CHECKS=0`); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"sync_job_items", "episodes", "sync_jobs", "skip_rules", "works", "platforms", "users", "schema_migrations"} {
		if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS "+table); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `SET FOREIGN_KEY_CHECKS=1`); err != nil {
		t.Fatal(err)
	}
	if err := migrate.Up(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := migrate.Up(ctx, db); err != nil {
		t.Fatal(err)
	}
	st := New(db)
	p, err := st.CreatePlatform(ctx, store.Platform{
		Code: "huangguoai", Name: "黄果爱", BaseURL: "https://huangguoai.com", Enabled: true,
		CronExpr: "0 8 * * *", SitemapPath: "/sitemap.xml",
	})
	if err != nil {
		t.Fatal(err)
	}
	published := time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC)
	created, err := st.UpsertWork(ctx, store.Work{
		PlatformID: p.ID, ExternalID: "12", Title: "虚构作品甲", Description: "简介",
		PlayURL: "https://huangguoai.com/video/12/", PublishedAt: &published,
		ViewCount: 15000, LikeCount: 10, FavoriteCount: 3, CommentCount: 1,
		Tags: []string{"短剧"},
		Episodes: []store.Episode{
			{EpisodeNo: 1, Title: "第1集", PlayURL: "https://huangguoai.com/video/12/"},
			{EpisodeNo: 2, Title: "第2集", PlayURL: "https://huangguoai.com/video/12/ep-2/"},
		},
	})
	if err != nil || !created {
		t.Fatalf("create %v %v", created, err)
	}
	created, err = st.UpsertWork(ctx, store.Work{
		PlatformID: p.ID, ExternalID: "12", Title: "虚构作品甲", Description: "新简介",
		PlayURL: "https://huangguoai.com/video/12/", ViewCount: 16000, LikeCount: 11,
		Episodes: []store.Episode{{EpisodeNo: 1, Title: "第1集", PlayURL: "https://huangguoai.com/video/12/"}},
	})
	if err != nil || created {
		t.Fatalf("update %v %v", created, err)
	}
	works, total, err := st.ListWorks(ctx, store.WorkFilter{Page: 1, PageSize: 10})
	if err != nil || total != 1 || works[0].LikeCount != 11 || works[0].ViewCount != 16000 || works[0].Description != "新简介" {
		t.Fatalf("list %+v total %d err %v", works, total, err)
	}
	full, err := st.GetWork(ctx, works[0].ID)
	if err != nil || len(full.Episodes) != 1 {
		t.Fatalf("episodes %+v %v", full.Episodes, err)
	}
	title, _, count, err := st.AddAbsent(ctx, p.ID, "12")
	if err != nil || title != "虚构作品甲" || count != 1 {
		t.Fatalf("absent %s %d %v", title, count, err)
	}
	if err := st.ClearWorkDescription(ctx, p.ID, "12"); err != nil {
		t.Fatal(err)
	}
	full, _ = st.GetWork(ctx, works[0].ID)
	if full.Description != "" {
		t.Fatalf("description %q", full.Description)
	}
	rule := store.SkipRule{Name: "未成年与未满21岁", RuleType: store.RuleBuiltin, Target: store.TargetAll, Pattern: "builtin", Reason: "内置", Enabled: true, Locked: true}
	if err := st.EnsureBuiltinRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	rules, err := st.ListSkipRules(ctx)
	if err != nil || len(rules) != 1 || !rules[0].Locked {
		t.Fatalf("rules %+v %v", rules, err)
	}
	if err := st.DeleteSkipRule(ctx, rules[0].ID); err != store.ErrRuleLocked {
		t.Fatalf("delete builtin: %v", err)
	}
	job, err := st.CreateJob(ctx, store.SyncJob{PlatformID: p.ID, Mode: store.ModeFull, TriggerType: store.TriggerManual, Status: store.StatusRunning})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddJobItem(ctx, store.SyncJobItem{JobID: job.ID, ExternalID: "12", ItemTitle: "虚构作品甲", Action: store.ActionSkipped, Reason: "跳过"}); err != nil {
		t.Fatal(err)
	}
	items, n, err := st.ListJobItems(ctx, job.ID, 1, 10)
	if err != nil || n != 1 || items[0].ItemTitle != "虚构作品甲" {
		t.Fatalf("items %+v %d %v", items, n, err)
	}
}
