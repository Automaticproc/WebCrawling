// Package syncer runs a platform sync. Descriptions stay in memory until skip rules pass.
package syncer

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"catalog/internal/adapter/huangguoai"
	"catalog/internal/adapter/playurl"
	"catalog/internal/skip"
	"catalog/internal/store"
	"catalog/internal/timeutil"
)

type Logger interface {
	Printf(format string, args ...any)
}

type Service struct {
	Store Store
	Delay time.Duration
	Now   func() time.Time
	Log   Logger

	mu      sync.Mutex
	running map[int64]bool
}

type Store interface {
	store.Store
}

func New(st store.Store, delay time.Duration) *Service {
	return &Service{
		Store:   st,
		Delay:   delay,
		Now:     func() time.Time { return time.Now().In(timeutil.HKT) },
		Log:     log.Default(),
		running: map[int64]bool{},
	}
}

// Start records a job and runs it. Overlapping runs for the same platform are stored as skipped.
// wait runs the sync on the caller goroutine; otherwise it returns as soon as the job row exists.
func (s *Service) Start(ctx context.Context, platformID int64, mode, trigger string, wait bool) (store.SyncJob, error) {
	if mode != store.ModeFull && mode != store.ModeIncremental {
		return store.SyncJob{}, fmt.Errorf("同步模式无效")
	}
	if trigger != store.TriggerManual && trigger != store.TriggerSchedule {
		return store.SyncJob{}, fmt.Errorf("触发方式无效")
	}
	s.mu.Lock()
	if s.running[platformID] {
		s.mu.Unlock()
		return s.skipOverlap(ctx, platformID, mode, trigger)
	}
	has, err := s.Store.HasRunningJob(ctx, platformID)
	if err != nil {
		s.mu.Unlock()
		return store.SyncJob{}, err
	}
	if has {
		s.mu.Unlock()
		return s.skipOverlap(ctx, platformID, mode, trigger)
	}
	job, err := s.Store.CreateJob(ctx, store.SyncJob{
		PlatformID:  platformID,
		Mode:        mode,
		TriggerType: trigger,
		Status:      store.StatusRunning,
	})
	if err != nil {
		s.mu.Unlock()
		return store.SyncJob{}, err
	}
	s.running[platformID] = true
	s.mu.Unlock()

	if wait {
		defer s.clear(platformID)
		s.execute(ctx, job)
		return s.Store.GetJob(ctx, job.ID)
	}
	go func() {
		defer s.clear(platformID)
		s.execute(context.Background(), job)
	}()
	return job, nil
}

func (s *Service) clear(platformID int64) {
	s.mu.Lock()
	delete(s.running, platformID)
	s.mu.Unlock()
}

func (s *Service) skipOverlap(ctx context.Context, platformID int64, mode, trigger string) (store.SyncJob, error) {
	now := s.Now().UTC()
	return s.Store.CreateJob(ctx, store.SyncJob{
		PlatformID:  platformID,
		Mode:        mode,
		TriggerType: trigger,
		Status:      store.StatusSkipped,
		SkipReason:  store.OverlapReason,
		StartedAt:   &now,
		FinishedAt:  &now,
	})
}

func (s *Service) execute(ctx context.Context, job store.SyncJob) {
	finished := false
	defer func() {
		if rec := recover(); rec != nil && !finished {
			s.logf("job=%d panic", job.ID)
			job.Status = store.StatusFailed
			job.ErrorMessage = "任务异常中断"
			end := s.Now().UTC()
			job.FinishedAt = &end
			_ = s.Store.FinishJob(context.Background(), job)
		}
	}()
	job.Status = store.StatusFailed
	job.ErrorMessage = "任务未完成"
	end := func() {
		if finished {
			return
		}
		finished = true
		t := s.Now().UTC()
		job.FinishedAt = &t
		if err := s.Store.FinishJob(context.Background(), job); err != nil {
			s.logf("job=%d finish error", job.ID)
		}
	}
	platform, err := s.Store.GetPlatform(ctx, job.PlatformID)
	if err != nil {
		job.ErrorMessage = "平台不存在"
		end()
		return
	}
	if platform.Code != "huangguoai" {
		job.ErrorMessage = "暂不支持该平台适配器"
		end()
		return
	}
	adapter, err := huangguoai.New(platform.BaseURL, platform.SitemapPath, nil)
	if err != nil {
		job.ErrorMessage = "平台地址无法用于采集"
		end()
		return
	}
	since := time.Time{}
	if job.Mode == store.ModeIncremental {
		since = s.incrementalSince(platform)
	}
	refs, err := adapter.Discover(ctx, job.Mode, since)
	if err != nil {
		job.ErrorMessage = "拉取站点地图失败"
		end()
		return
	}
	if job.Mode == store.ModeFull && len(refs) == 0 {
		job.ErrorMessage = "站点地图没有作品地址，已中止以免误记缺席"
		end()
		return
	}
	job.DiscoveredCount = len(refs)
	rules, err := s.Store.ListSkipRules(ctx)
	if err != nil {
		job.ErrorMessage = "读取跳过规则失败"
		end()
		return
	}
	seen := map[string]struct{}{}
	for i, ref := range refs {
		if ctx.Err() != nil {
			job.ErrorMessage = "任务已取消"
			end()
			return
		}
		if i > 0 && s.Delay > 0 {
			timer := time.NewTimer(s.Delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				job.ErrorMessage = "任务已取消"
				end()
				return
			case <-timer.C:
			}
		}
		seen[ref.ExternalID] = struct{}{}
		detail, err := adapter.Fetch(ctx, ref)
		if err != nil {
			job.FailedCount++
			s.addItem(ctx, job.ID, ref.ExternalID, ref.ExternalID, store.ActionFailed, "拉取作品页失败", ref.PageURL)
			s.logf("job=%d external=%s action=failed", job.ID, ref.ExternalID)
			continue
		}
		s.keep(ctx, &job, platform.ID, rules, detail)
	}
	if job.Mode == store.ModeFull {
		s.markAbsent(ctx, &job, platform.ID, seen)
	}
	if err := s.Store.TouchPlatformSync(ctx, platform.ID, s.Now().UTC()); err != nil {
		job.ErrorMessage = "更新同步时间失败"
		end()
		return
	}
	job.Status = store.StatusSucceeded
	job.ErrorMessage = ""
	end()
}

func (s *Service) keep(ctx context.Context, job *store.SyncJob, platformID int64, rules []store.SkipRule, detail huangguoai.Detail) {
	// Withhold the synopsis until skip rules explicitly allow it.
	synopsis := detail.Description
	detail.Description = ""
	tags := strings.Join(detail.Tags, " ")
	decision := skip.Evaluate(rules, detail.Title, tags, synopsis)
	if decision.Skip {
		job.SkippedCount++
		play := ""
		if decision.Minors {
			_ = s.Store.DeleteWorkByExternal(ctx, platformID, detail.ExternalID)
		} else {
			_ = s.Store.ClearWorkDescription(ctx, platformID, detail.ExternalID)
			if playurl.IsOfficial(detail.PlayURL) {
				play = detail.PlayURL
			}
		}
		s.addItem(ctx, job.ID, detail.ExternalID, detail.Title, store.ActionSkipped, decision.Reason, play)
		s.logf("job=%d external=%s action=skipped minors=%t", job.ID, detail.ExternalID, decision.Minors)
		return
	}
	detail.Description = synopsis
	work := toWork(platformID, detail)
	created, err := s.Store.UpsertWork(ctx, work)
	if err != nil {
		job.FailedCount++
		s.addItem(ctx, job.ID, detail.ExternalID, detail.Title, store.ActionFailed, "保存作品失败", detail.PlayURL)
		return
	}
	action := store.ActionUpdated
	if created {
		action = store.ActionCreated
		job.CreatedCount++
	} else {
		job.UpdatedCount++
	}
	s.addItem(ctx, job.ID, detail.ExternalID, detail.Title, action, "", detail.PlayURL)
	s.logf("job=%d external=%s action=%s", job.ID, detail.ExternalID, action)
}

func (s *Service) markAbsent(ctx context.Context, job *store.SyncJob, platformID int64, seen map[string]struct{}) {
	refs, err := s.Store.ListWorksForAbsent(ctx, platformID)
	if err != nil {
		return
	}
	for _, ref := range refs {
		if _, ok := seen[ref.ExternalID]; ok {
			continue
		}
		title, play, count, err := s.Store.AddAbsent(ctx, platformID, ref.ExternalID)
		if err != nil {
			continue
		}
		if !playurl.IsOfficial(play) {
			play = ""
		}
		job.AbsentCount++
		s.addItem(ctx, job.ID, ref.ExternalID, title, store.ActionAbsent, fmt.Sprintf("全量同步未见到该作品，缺席次数 %d", count), play)
	}
}

func (s *Service) addItem(ctx context.Context, jobID int64, externalID, title, action, reason, play string) {
	if title == "" {
		title = externalID
	}
	if !playurl.IsOfficial(play) {
		play = ""
	}
	_ = s.Store.AddJobItem(ctx, store.SyncJobItem{
		JobID:      jobID,
		ExternalID: externalID,
		ItemTitle:  title,
		Action:     action,
		Reason:     reason,
		PlayURL:    play,
	})
}

func (s *Service) incrementalSince(p store.Platform) time.Time {
	if p.LastSyncedAt != nil && !p.LastSyncedAt.IsZero() {
		return p.LastSyncedAt.In(timeutil.HKT)
	}
	return s.Now().In(timeutil.HKT).Add(-24 * time.Hour)
}

func (s *Service) logf(format string, args ...any) {
	if s.Log != nil {
		s.Log.Printf(format, args...)
	}
}

func toWork(platformID int64, d huangguoai.Detail) store.Work {
	eps := make([]store.Episode, 0, len(d.Episodes))
	for _, ep := range d.Episodes {
		if !playurl.IsOfficial(ep.PlayURL) {
			continue
		}
		eps = append(eps, store.Episode{EpisodeNo: ep.No, Title: ep.Title, PlayURL: ep.PlayURL})
	}
	seen := time.Now().UTC()
	return store.Work{
		PlatformID:    platformID,
		ExternalID:    d.ExternalID,
		Title:         d.Title,
		Description:   d.Description,
		CoverURL:      d.CoverURL,
		Category:      d.Category,
		Tags:          d.Tags,
		Author:        d.Author,
		PlayURL:       d.PlayURL,
		PublishedAt:   d.PublishedAt,
		ViewCount:     d.ViewCount,
		LikeCount:     d.LikeCount,
		FavoriteCount: d.FavoriteCount,
		CommentCount:  d.CommentCount,
		EpisodeCount:  len(eps),
		LastSeenAt:    &seen,
		Episodes:      eps,
	}
}
