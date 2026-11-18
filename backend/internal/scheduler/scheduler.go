// Package scheduler fires incremental syncs from each platform cron expression.
// Expressions are evaluated in Asia/Hong_Kong. The loop does not run a sync on startup
// unless the current minute is actually due.
package scheduler

import (
	"context"
	"time"

	"catalog/internal/schedule"
	"catalog/internal/store"
	"catalog/internal/timeutil"
)

type Starter interface {
	Start(ctx context.Context, platformID int64, mode, trigger string, wait bool) (store.SyncJob, error)
}

type Scheduler struct {
	Store     store.Store
	Sync      Starter
	Now       func() time.Time
	fired     map[string]struct{}
	tickEvery time.Duration
}

func New(st store.Store, sync Starter) *Scheduler {
	return &Scheduler{
		Store:     st,
		Sync:      sync,
		Now:       time.Now,
		fired:     map[string]struct{}{},
		tickEvery: 20 * time.Second,
	}
}

func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.tickEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.Tick(ctx)
		}
	}
}

// Tick starts an incremental sync for every enabled platform whose cron matches this minute.
func (s *Scheduler) Tick(ctx context.Context) error {
	now := s.Now()
	platforms, err := s.Store.ListPlatforms(ctx)
	if err != nil {
		return err
	}
	slot := now.In(timeutil.HKT).Format("2006-01-02 15:04")
	for _, p := range platforms {
		if !p.Enabled || !schedule.Due(p.CronExpr, now) {
			continue
		}
		key := slot + "|" + itoa(p.ID)
		if _, ok := s.fired[key]; ok {
			continue
		}
		s.fired[key] = struct{}{}
		if len(s.fired) > 1000 {
			s.fired = map[string]struct{}{key: {}}
		}
		_, _ = s.Sync.Start(ctx, p.ID, store.ModeIncremental, store.TriggerSchedule, false)
	}
	return nil
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
