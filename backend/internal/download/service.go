package download

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"catalog/internal/adapter/playurl"
	"catalog/internal/secretbox"
	"catalog/internal/store"
)

// ErrRunning is returned when a work already has an unfinished download.
var ErrRunning = errors.New("该作品已有进行中的下载")

// ErrNothingToRetry is returned when a task has no failed or missing episodes.
var ErrNothingToRetry = errors.New("没有需要重试的集")

type Logger interface {
	Printf(format string, args ...any)
}

// Progress is a live snapshot for the UI.
type Progress struct {
	TaskID    int64 `json:"task_id"`
	EpisodeNo int   `json:"episode_no"`
	Done      int   `json:"done"`
	Total     int   `json:"total"`
	Percent   int   `json:"percent"`
}

type progressState struct {
	mu sync.Mutex
	p  Progress
}

// Service coordinates per-work downloads.
type Service struct {
	Store      store.Store
	Resolver   *Resolver
	Engine     *Engine
	OutputRoot string
	Delay      time.Duration
	Now        func() time.Time
	Log        Logger
	Secret     string // SESSION_SECRET，用于解密平台登录密码

	mu          sync.Mutex
	cancels     map[int64]context.CancelFunc
	activeWork  map[int64]int64
	progressMap map[int64]*progressState
}

func NewService(st store.Store, resolver *Resolver, engine *Engine, outputRoot string) *Service {
	if outputRoot == "" {
		outputRoot = "downloads"
	}
	return &Service{
		Store:       st,
		Resolver:    resolver,
		Engine:      engine,
		OutputRoot:  outputRoot,
		Delay:       500 * time.Millisecond,
		Now:         func() time.Time { return time.Now().UTC() },
		Log:         log.Default(),
		cancels:     map[int64]context.CancelFunc{},
		activeWork:  map[int64]int64{},
		progressMap: map[int64]*progressState{},
	}
}

// StartWork creates a task for every episode of a work and runs it in the background.
func (s *Service) StartWork(ctx context.Context, workID int64, quality string) (store.DownloadTask, error) {
	if strings.TrimSpace(quality) == "" {
		quality = "best"
	}
	work, err := s.Store.GetWork(ctx, workID)
	if err != nil {
		return store.DownloadTask{}, err
	}
	platform, err := s.Store.GetPlatform(ctx, work.PlatformID)
	if err != nil {
		return store.DownloadTask{}, err
	}

	s.mu.Lock()
	_, busy := s.activeWork[workID]
	s.mu.Unlock()
	if busy {
		return store.DownloadTask{}, ErrRunning
	}
	if has, err := s.Store.HasRunningDownload(ctx, workID); err != nil {
		return store.DownloadTask{}, err
	} else if has {
		return store.DownloadTask{}, ErrRunning
	}

	episodes := work.Episodes
	if len(episodes) == 0 {
		episodes = []store.Episode{{EpisodeNo: 1, Title: "第1集", PlayURL: work.PlayURL}}
	}
	dir := filepath.Join(s.OutputRoot, safeName(platform.Code), safeName(work.Title))
	task, err := s.Store.CreateDownloadTask(ctx, store.DownloadTask{
		WorkID:        work.ID,
		PlatformID:    work.PlatformID,
		Status:        store.DownloadQueued,
		TotalEpisodes: len(episodes),
		Quality:       quality,
		OutputDir:     dir,
	})
	if err != nil {
		return store.DownloadTask{}, err
	}
	items := make([]store.DownloadItem, 0, len(episodes))
	for _, ep := range episodes {
		pageURL := strings.TrimSpace(ep.PlayURL)
		if !playurl.IsOfficial(pageURL) {
			pageURL = strings.TrimRight(platform.BaseURL, "/") + playurl.EpisodePath(work.ExternalID, ep.EpisodeNo)
		}
		item, err := s.Store.AddDownloadItem(ctx, store.DownloadItem{
			TaskID:    task.ID,
			EpisodeID: ep.ID,
			EpisodeNo: ep.EpisodeNo,
			Title:     ep.Title,
			PageURL:   pageURL,
			Status:    store.DownloadQueued,
		})
		if err != nil {
			return store.DownloadTask{}, err
		}
		items = append(items, item)
	}
	task.Items = items

	go s.run(task.ID, work, quality, s.resolverFor(platform))
	return task, nil
}

// resolverFor layers per-platform login settings on top of the global defaults.
func (s *Service) resolverFor(p store.Platform) *Resolver {
	base := s.Resolver
	r := NewResolver(base.BrowserPath, base.BrowserTimeout)
	r.Client = base.Client
	r.BrowserPort = base.BrowserPort
	r.TempDir = base.TempDir
	r.UserAgent = base.UserAgent
	r.BrowserProfile = base.BrowserProfile
	if r.BrowserProfile != "" {
		// 每个平台单独一个 profile，避免 Cookie 混用
		r.BrowserProfile = filepath.Join(r.BrowserProfile, safeName(p.Code))
	}
	r.Cookies = base.Cookies
	r.LoginURL, r.LoginUser, r.LoginPassword = base.LoginURL, base.LoginUser, base.LoginPassword
	r.LoginUserSelector = base.LoginUserSelector
	r.LoginPassSelector = base.LoginPassSelector
	r.LoginSubmitSelector = base.LoginSubmitSelector

	if p.BrowserProfile != "" {
		r.BrowserProfile = p.BrowserProfile
	}
	if p.Cookie != "" {
		r.Cookies = p.Cookie
	}
	if p.LoginURL != "" {
		r.LoginURL = p.LoginURL
	}
	if p.LoginUsername != "" {
		r.LoginUser = p.LoginUsername
	}
	if p.LoginUserSelector != "" {
		r.LoginUserSelector = p.LoginUserSelector
	}
	if p.LoginPassSelector != "" {
		r.LoginPassSelector = p.LoginPassSelector
	}
	if p.LoginSubmitSelector != "" {
		r.LoginSubmitSelector = p.LoginSubmitSelector
	}
	if p.LoginPasswordEnc != "" {
		if pw, err := secretbox.Decrypt(s.Secret, p.LoginPasswordEnc); err == nil {
			r.LoginPassword = pw
		} else if s.Log != nil {
			s.logf("platform=%s 登录密码解密失败：%v", p.Code, err)
		}
	}
	return r
}

// RetryTask creates a new task containing only the episodes that failed, were
// canceled before starting, or whose file disappeared. Finished episodes stay untouched.
func (s *Service) RetryTask(ctx context.Context, taskID int64) (store.DownloadTask, error) {
	old, err := s.Store.GetDownloadTask(ctx, taskID)
	if err != nil {
		return store.DownloadTask{}, err
	}
	if old.Status == store.DownloadQueued || old.Status == store.DownloadRunning {
		return store.DownloadTask{}, ErrRunning
	}
	work, err := s.Store.GetWork(ctx, old.WorkID)
	if err != nil {
		return store.DownloadTask{}, err
	}
	s.mu.Lock()
	_, busy := s.activeWork[old.WorkID]
	s.mu.Unlock()
	if busy {
		return store.DownloadTask{}, ErrRunning
	}
	if has, err := s.Store.HasRunningDownload(ctx, old.WorkID); err != nil {
		return store.DownloadTask{}, err
	} else if has {
		return store.DownloadTask{}, ErrRunning
	}

	var pending []store.DownloadItem
	for _, it := range old.Items {
		if needsRetry(it) {
			pending = append(pending, it)
		}
	}
	if len(pending) == 0 {
		return store.DownloadTask{}, ErrNothingToRetry
	}

	platform, err := s.Store.GetPlatform(ctx, old.PlatformID)
	if err != nil {
		return store.DownloadTask{}, err
	}
	dir := strings.TrimSpace(old.OutputDir)
	if dir == "" {
		dir = filepath.Join(s.OutputRoot, safeName(platform.Code), safeName(work.Title))
	}
	task, err := s.Store.CreateDownloadTask(ctx, store.DownloadTask{
		WorkID:        old.WorkID,
		PlatformID:    old.PlatformID,
		Status:        store.DownloadQueued,
		TotalEpisodes: len(pending),
		Quality:       old.Quality,
		OutputDir:     dir,
	})
	if err != nil {
		return store.DownloadTask{}, err
	}
	items := make([]store.DownloadItem, 0, len(pending))
	for _, it := range pending {
		created, err := s.Store.AddDownloadItem(ctx, store.DownloadItem{
			TaskID:    task.ID,
			EpisodeID: it.EpisodeID,
			EpisodeNo: it.EpisodeNo,
			Title:     it.Title,
			PageURL:   it.PageURL,
			Status:    store.DownloadQueued,
		})
		if err != nil {
			return store.DownloadTask{}, err
		}
		items = append(items, created)
	}
	task.Items = items
	go s.run(task.ID, work, old.Quality, s.resolverFor(platform))
	return task, nil
}

func needsRetry(it store.DownloadItem) bool {
	if it.Status == store.DownloadSucceeded && it.FilePath != "" {
		if st, err := os.Stat(it.FilePath); err == nil && st.Size() > 0 {
			return false
		}
	}
	return true
}

func (s *Service) run(taskID int64, work store.Work, quality string, resolver *Resolver) {
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.cancels[taskID] = cancel
	s.activeWork[work.ID] = taskID
	s.progressMap[taskID] = &progressState{}
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		delete(s.cancels, taskID)
		delete(s.activeWork, work.ID)
		delete(s.progressMap, taskID)
		s.mu.Unlock()
	}()

	task, err := s.Store.GetDownloadTask(context.Background(), taskID)
	if err != nil {
		s.logf("download task=%d load: %v", taskID, err)
		return
	}
	started := s.Now()
	task.Status = store.DownloadRunning
	task.StartedAt = &started
	task.ErrorMessage = ""
	_ = s.Store.UpdateDownloadTask(context.Background(), task)

	completed, failed := 0, 0
	firstErr := ""
	for _, it := range task.Items {
		if ctx.Err() != nil {
			break
		}
		it.Status = store.DownloadRunning
		_ = s.Store.UpdateDownloadItem(context.Background(), it)

		err := s.one(ctx, task, &it, resolver)
		if err != nil {
			failed++
			it.Status = store.DownloadFailed
			it.ErrorMessage = clipText(err.Error(), 500)
			if firstErr == "" {
				firstErr = fmt.Sprintf("第%d集：%s", it.EpisodeNo, it.ErrorMessage)
			}
			s.logf("download task=%d ep=%d failed: %v", taskID, it.EpisodeNo, err)
		} else {
			completed++
			it.Status = store.DownloadSucceeded
			it.ErrorMessage = ""
		}
		it.UpdatedAt = s.Now()
		if uerr := s.Store.UpdateDownloadItem(context.Background(), it); uerr != nil {
			s.logf("download task=%d ep=%d update: %v", taskID, it.EpisodeNo, uerr)
		}
		task.CompletedEpisodes = completed
		task.FailedEpisodes = failed
		_ = s.Store.UpdateDownloadTask(context.Background(), task)

		if s.Delay > 0 && ctx.Err() == nil {
			select {
			case <-ctx.Done():
			case <-time.After(s.Delay):
			}
		}
	}

	finished := s.Now()
	task.FinishedAt = &finished
	task.CompletedEpisodes = completed
	task.FailedEpisodes = failed
	switch {
	case ctx.Err() != nil:
		task.Status = store.DownloadCanceled
		task.ErrorMessage = "任务已取消"
	case failed > 0:
		task.Status = store.DownloadFailed
		task.ErrorMessage = fmt.Sprintf("%d 集下载失败", failed)
		if firstErr != "" {
			task.ErrorMessage += "：" + firstErr
		}
		task.ErrorMessage = clipText(task.ErrorMessage, 500)
	default:
		task.Status = store.DownloadSucceeded
		task.ErrorMessage = ""
	}
	if err := s.Store.UpdateDownloadTask(context.Background(), task); err != nil {
		s.logf("download task=%d finish: %v", taskID, err)
	}
}

func (s *Service) one(ctx context.Context, task store.DownloadTask, it *store.DownloadItem, resolver *Resolver) error {
	stream, err := resolver.Resolve(ctx, it.PageURL, nil)
	if err != nil {
		return err
	}
	out := filepath.Join(task.OutputDir, fmt.Sprintf("第%02d集.mp4", it.EpisodeNo))
	if err := os.MkdirAll(task.OutputDir, 0o755); err != nil {
		return err
	}
	res, err := s.Engine.Download(ctx, stream.M3U8, stream.Headers, out, task.Quality, func(done, total int) {
		s.setProgress(task.ID, it.EpisodeNo, done, total)
	})
	if err != nil {
		return err
	}
	it.StreamURL = stream.M3U8
	it.FilePath = res.Path
	it.SizeBytes = res.Size
	it.DurationSeconds = res.Duration
	it.Status = store.DownloadSucceeded
	return nil
}

// Cancel stops a running task.
func (s *Service) Cancel(_ context.Context, taskID int64) error {
	s.mu.Lock()
	cancel, ok := s.cancels[taskID]
	s.mu.Unlock()
	if !ok {
		return store.ErrNotFound
	}
	cancel()
	return nil
}

// Snapshot returns the live segment progress for a task.
func (s *Service) Snapshot(taskID int64) (Progress, bool) {
	s.mu.Lock()
	st, ok := s.progressMap[taskID]
	s.mu.Unlock()
	if !ok {
		return Progress{}, false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	p := st.p
	if p.Total > 0 {
		p.Percent = p.Done * 100 / p.Total
	}
	return p, true
}

func (s *Service) setProgress(taskID int64, episode, done, total int) {
	s.mu.Lock()
	st, ok := s.progressMap[taskID]
	s.mu.Unlock()
	if !ok {
		return
	}
	st.mu.Lock()
	st.p = Progress{TaskID: taskID, EpisodeNo: episode, Done: done, Total: total}
	st.mu.Unlock()
}

func (s *Service) logf(format string, args ...any) {
	if s.Log != nil {
		s.Log.Printf(format, args...)
	}
}

func safeName(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', '\n', '\r', '\t':
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), " .")
	if out == "" {
		out = "work"
	}
	r := []rune(out)
	if len(r) > 80 {
		out = string(r[:80])
	}
	return out
}

func clipText(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
