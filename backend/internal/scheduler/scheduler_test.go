package scheduler

import (
	"context"
	"testing"
	"time"

	"catalog/internal/store"
	"catalog/internal/store/memstore"
	"catalog/internal/timeutil"
)

type fakeSync struct {
	calls []call
}

type call struct {
	id      int64
	mode    string
	trigger string
	wait    bool
}

func (f *fakeSync) Start(_ context.Context, platformID int64, mode, trigger string, wait bool) (store.SyncJob, error) {
	f.calls = append(f.calls, call{platformID, mode, trigger, wait})
	return store.SyncJob{Status: store.StatusRunning}, nil
}

func TestTickFiresOnlyAtEight(t *testing.T) {
	st := memstore.New()
	ctx := context.Background()
	enabled, err := st.CreatePlatform(ctx, store.Platform{
		Code: "huangguoai", Name: "甲", BaseURL: "https://catalog.example", Enabled: true,
		CronExpr: timeutil.DefaultCron, SitemapPath: "/sitemap.xml",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreatePlatform(ctx, store.Platform{
		Code: "other", Name: "乙", BaseURL: "https://other.example", Enabled: false,
		CronExpr: timeutil.DefaultCron, SitemapPath: "/sitemap.xml",
	}); err != nil {
		t.Fatal(err)
	}
	fake := &fakeSync{}
	sched := New(st, fake)
	sched.Now = func() time.Time { return time.Date(2026, 11, 17, 7, 0, 0, 0, timeutil.HKT) }
	if err := sched.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("fired early: %+v", fake.calls)
	}
	sched.Now = func() time.Time { return time.Date(2026, 11, 17, 8, 0, 20, 0, timeutil.HKT) }
	if err := sched.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sched.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("calls %+v", fake.calls)
	}
	got := fake.calls[0]
	if got.id != enabled.ID || got.mode != store.ModeIncremental || got.trigger != store.TriggerSchedule || got.wait {
		t.Fatalf("call %+v", got)
	}
}
