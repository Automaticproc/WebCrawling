package seed

import (
	"context"
	"testing"

	"catalog/internal/config"
	"catalog/internal/store"
	"catalog/internal/store/memstore"
	"catalog/internal/timeutil"
)

func TestSeedDoesNotSync(t *testing.T) {
	st := memstore.New()
	cfg := config.Config{AdminUser: "admin", AdminPassword: "correct-horse"}
	ctx := context.Background()
	if err := Run(ctx, st, cfg); err != nil {
		t.Fatal(err)
	}
	if err := Run(ctx, st, cfg); err != nil {
		t.Fatal(err)
	}
	users, err := st.CountUsers(ctx)
	if err != nil || users != 1 {
		t.Fatalf("users %d %v", users, err)
	}
	platforms, err := st.ListPlatforms(ctx)
	if err != nil || len(platforms) != 1 {
		t.Fatalf("platforms %+v %v", platforms, err)
	}
	p := platforms[0]
	if p.Code != "huangguoai" || p.CronExpr != timeutil.DefaultCron || !p.Enabled || p.LastSyncedAt != nil {
		t.Fatalf("platform %+v", p)
	}
	rules, err := st.ListSkipRules(ctx)
	if err != nil || len(rules) != 1 || !rules[0].Locked || !rules[0].Enabled || rules[0].RuleType != store.RuleBuiltin {
		t.Fatalf("rules %+v %v", rules, err)
	}
	_, n, err := st.ListJobs(ctx, 0, 1, 20)
	if err != nil || n != 0 {
		t.Fatalf("jobs %d %v", n, err)
	}
}
