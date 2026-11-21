// Package memstore is an in-memory Store used by tests.
package memstore

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"catalog/internal/store"
)

type Store struct {
	mu        sync.Mutex
	users     map[int64]store.User
	userName  map[string]int64
	platforms map[int64]store.Platform
	codes     map[string]int64
	works     map[int64]store.Work
	episodes  map[int64][]store.Episode
	jobs      map[int64]store.SyncJob
	items     map[int64][]store.SyncJobItem
	rules     map[int64]store.SkipRule
	downloads map[int64]store.DownloadTask
	dlItems   map[int64][]store.DownloadItem
	nextID    int64
}

func New() *Store {
	return &Store{
		users:     map[int64]store.User{},
		userName:  map[string]int64{},
		platforms: map[int64]store.Platform{},
		codes:     map[string]int64{},
		works:     map[int64]store.Work{},
		episodes:  map[int64][]store.Episode{},
		jobs:      map[int64]store.SyncJob{},
		items:     map[int64][]store.SyncJobItem{},
		rules:     map[int64]store.SkipRule{},
		downloads: map[int64]store.DownloadTask{},
		dlItems:   map[int64][]store.DownloadItem{},
		nextID:    1,
	}
}

func (s *Store) id() int64 {
	n := s.nextID
	s.nextID++
	return n
}

func now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

func (s *Store) CreateUser(_ context.Context, username, passwordHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.userName[username]; ok {
		return store.ErrConflict
	}
	id := s.id()
	ts := now()
	s.users[id] = store.User{ID: id, Username: username, PasswordHash: passwordHash, CreatedAt: ts, UpdatedAt: ts}
	s.userName[username] = id
	return nil
}

func (s *Store) GetUserByUsername(_ context.Context, username string) (store.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.userName[username]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	return s.users[id], nil
}

func (s *Store) CountUsers(_ context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.users), nil
}

func (s *Store) ListPlatforms(_ context.Context) ([]store.Platform, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]store.Platform, 0, len(s.platforms))
	for _, p := range s.platforms {
		out = append(out, clonePlatform(p))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) GetPlatform(_ context.Context, id int64) (store.Platform, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.platforms[id]
	if !ok {
		return store.Platform{}, store.ErrNotFound
	}
	return clonePlatform(p), nil
}

func (s *Store) GetPlatformByCode(_ context.Context, code string) (store.Platform, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.codes[code]
	if !ok {
		return store.Platform{}, store.ErrNotFound
	}
	return clonePlatform(s.platforms[id]), nil
}

func (s *Store) CreatePlatform(_ context.Context, p store.Platform) (store.Platform, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.codes[p.Code]; ok {
		return store.Platform{}, store.ErrConflict
	}
	p.ID = s.id()
	ts := now()
	p.CreatedAt, p.UpdatedAt = ts, ts
	s.platforms[p.ID] = clonePlatform(p)
	s.codes[p.Code] = p.ID
	return clonePlatform(p), nil
}

func (s *Store) UpdatePlatform(_ context.Context, p store.Platform) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.platforms[p.ID]
	if !ok {
		return store.ErrNotFound
	}
	if other, ok := s.codes[p.Code]; ok && other != p.ID {
		return store.ErrConflict
	}
	delete(s.codes, cur.Code)
	p.CreatedAt = cur.CreatedAt
	p.UpdatedAt = now()
	if p.LastSyncedAt == nil {
		p.LastSyncedAt = cur.LastSyncedAt
	}
	s.platforms[p.ID] = clonePlatform(p)
	s.codes[p.Code] = p.ID
	return nil
}

func (s *Store) DeletePlatform(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.platforms[id]
	if !ok {
		return store.ErrNotFound
	}
	delete(s.platforms, id)
	delete(s.codes, p.Code)
	for wid, w := range s.works {
		if w.PlatformID == id {
			delete(s.works, wid)
			delete(s.episodes, wid)
		}
	}
	for jid, j := range s.jobs {
		if j.PlatformID == id {
			delete(s.jobs, jid)
			delete(s.items, jid)
		}
	}
	for rid, r := range s.rules {
		if r.PlatformID != nil && *r.PlatformID == id {
			delete(s.rules, rid)
		}
	}
	return nil
}

func (s *Store) TouchPlatformSync(_ context.Context, id int64, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.platforms[id]
	if !ok {
		return store.ErrNotFound
	}
	t := at.UTC()
	p.LastSyncedAt = &t
	p.UpdatedAt = now()
	s.platforms[id] = p
	return nil
}

func (s *Store) UpsertWork(_ context.Context, w store.Work) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.platforms[w.PlatformID]; !ok {
		return false, store.ErrNotFound
	}
	ts := now()
	created := true
	var id int64
	for _, cur := range s.works {
		if cur.PlatformID == w.PlatformID && cur.ExternalID == w.ExternalID {
			created = false
			id = cur.ID
			w.CreatedAt = cur.CreatedAt
			break
		}
	}
	if created {
		id = s.id()
		w.CreatedAt = ts
	}
	w.ID = id
	w.UpdatedAt = ts
	seen := ts
	if w.LastSeenAt != nil {
		seen = w.LastSeenAt.UTC()
	}
	w.LastSeenAt = &seen
	w.AbsentCount = 0
	w.EpisodeCount = len(w.Episodes)
	s.works[id] = cloneWork(w)
	eps := make([]store.Episode, 0, len(w.Episodes))
	for _, ep := range w.Episodes {
		ep.ID = s.id()
		ep.WorkID = id
		ep.PlatformID = w.PlatformID
		ep.CreatedAt, ep.UpdatedAt = ts, ts
		eps = append(eps, ep)
	}
	s.episodes[id] = eps
	return created, nil
}

func (s *Store) ClearWorkDescription(_ context.Context, platformID int64, externalID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, w := range s.works {
		if w.PlatformID == platformID && w.ExternalID == externalID {
			w.Description = ""
			w.UpdatedAt = now()
			s.works[id] = w
			return nil
		}
	}
	return nil
}

func (s *Store) DeleteWorkByExternal(_ context.Context, platformID int64, externalID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, w := range s.works {
		if w.PlatformID == platformID && w.ExternalID == externalID {
			delete(s.works, id)
			delete(s.episodes, id)
		}
	}
	return nil
}

func (s *Store) GetWork(_ context.Context, id int64) (store.Work, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.works[id]
	if !ok {
		return store.Work{}, store.ErrNotFound
	}
	w.Episodes = append([]store.Episode(nil), s.episodes[id]...)
	sort.Slice(w.Episodes, func(i, j int) bool { return w.Episodes[i].EpisodeNo < w.Episodes[j].EpisodeNo })
	return cloneWork(w), nil
}

func (s *Store) ListWorks(_ context.Context, f store.WorkFilter) ([]store.Work, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := strings.ToLower(strings.TrimSpace(f.Query))
	var all []store.Work
	for _, w := range s.works {
		if f.PlatformID != 0 && w.PlatformID != f.PlatformID {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(w.Title), q) && !strings.Contains(strings.ToLower(w.ExternalID), q) {
			continue
		}
		all = append(all, cloneWork(w))
	}
	sort.Slice(all, func(i, j int) bool {
		if !all[i].UpdatedAt.Equal(all[j].UpdatedAt) {
			return all[i].UpdatedAt.After(all[j].UpdatedAt)
		}
		return all[i].ID > all[j].ID
	})
	total := len(all)
	start, end := pageBounds(f.Page, f.PageSize, total)
	return all[start:end], total, nil
}

func (s *Store) ListWorksForAbsent(_ context.Context, platformID int64) ([]store.WorkRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.WorkRef
	for _, w := range s.works {
		if w.PlatformID != platformID {
			continue
		}
		out = append(out, store.WorkRef{ExternalID: w.ExternalID, Title: w.Title, PlayURL: w.PlayURL, AbsentCount: w.AbsentCount})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ExternalID < out[j].ExternalID })
	return out, nil
}

func (s *Store) AddAbsent(_ context.Context, platformID int64, externalID string) (string, string, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, w := range s.works {
		if w.PlatformID == platformID && w.ExternalID == externalID {
			w.AbsentCount++
			w.UpdatedAt = now()
			s.works[id] = w
			return w.Title, w.PlayURL, w.AbsentCount, nil
		}
	}
	return "", "", 0, store.ErrNotFound
}

func (s *Store) CreateJob(_ context.Context, j store.SyncJob) (store.SyncJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j.ID = s.id()
	j.CreatedAt = now()
	if j.StartedAt == nil && j.Status == store.StatusRunning {
		t := j.CreatedAt
		j.StartedAt = &t
	}
	s.jobs[j.ID] = j
	return j, nil
}

func (s *Store) GetJob(_ context.Context, id int64) (store.SyncJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return store.SyncJob{}, store.ErrNotFound
	}
	return j, nil
}

func (s *Store) ListJobs(_ context.Context, platformID int64, page, pageSize int) ([]store.SyncJob, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var all []store.SyncJob
	for _, j := range s.jobs {
		if platformID != 0 && j.PlatformID != platformID {
			continue
		}
		all = append(all, j)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID > all[j].ID })
	total := len(all)
	start, end := pageBounds(page, pageSize, total)
	return all[start:end], total, nil
}

func (s *Store) HasRunningJob(_ context.Context, platformID int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.jobs {
		if j.PlatformID == platformID && j.Status == store.StatusRunning {
			return true, nil
		}
	}
	return false, nil
}

func (s *Store) FinishJob(_ context.Context, j store.SyncJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.jobs[j.ID]
	if !ok {
		return store.ErrNotFound
	}
	j.CreatedAt = cur.CreatedAt
	if j.StartedAt == nil {
		j.StartedAt = cur.StartedAt
	}
	s.jobs[j.ID] = j
	return nil
}

func (s *Store) FailStaleJobs(_ context.Context, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ts := now()
	for id, j := range s.jobs {
		if j.Status != store.StatusRunning {
			continue
		}
		j.Status = store.StatusFailed
		j.ErrorMessage = reason
		j.FinishedAt = &ts
		s.jobs[id] = j
	}
	return nil
}

func (s *Store) AddJobItem(_ context.Context, it store.SyncJobItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[it.JobID]; !ok {
		return store.ErrNotFound
	}
	it.ID = s.id()
	it.CreatedAt = now()
	s.items[it.JobID] = append(s.items[it.JobID], it)
	return nil
}

func (s *Store) ListJobItems(_ context.Context, jobID int64, page, pageSize int) ([]store.SyncJobItem, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[jobID]; !ok {
		return nil, 0, store.ErrNotFound
	}
	all := append([]store.SyncJobItem(nil), s.items[jobID]...)
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	total := len(all)
	start, end := pageBounds(page, pageSize, total)
	return all[start:end], total, nil
}

func (s *Store) ListSkipRules(_ context.Context) ([]store.SkipRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]store.SkipRule, 0, len(s.rules))
	for _, r := range s.rules {
		out = append(out, cloneRule(r))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (s *Store) GetSkipRule(_ context.Context, id int64) (store.SkipRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rules[id]
	if !ok {
		return store.SkipRule{}, store.ErrNotFound
	}
	return cloneRule(r), nil
}

func (s *Store) CreateSkipRule(_ context.Context, r store.SkipRule) (store.SkipRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Locked || r.RuleType == store.RuleBuiltin {
		return store.SkipRule{}, store.ErrRuleLocked
	}
	r.ID = s.id()
	ts := now()
	r.CreatedAt, r.UpdatedAt = ts, ts
	r.Locked = false
	s.rules[r.ID] = cloneRule(r)
	return cloneRule(r), nil
}

func (s *Store) UpdateSkipRule(_ context.Context, r store.SkipRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.rules[r.ID]
	if !ok {
		return store.ErrNotFound
	}
	if cur.Locked || cur.RuleType == store.RuleBuiltin || r.Locked || r.RuleType == store.RuleBuiltin {
		return store.ErrRuleLocked
	}
	r.CreatedAt = cur.CreatedAt
	r.UpdatedAt = now()
	r.Locked = false
	s.rules[r.ID] = cloneRule(r)
	return nil
}

func (s *Store) DeleteSkipRule(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.rules[id]
	if !ok {
		return store.ErrNotFound
	}
	if cur.Locked || cur.RuleType == store.RuleBuiltin {
		return store.ErrRuleLocked
	}
	delete(s.rules, id)
	return nil
}

func (s *Store) EnsureBuiltinRule(_ context.Context, r store.SkipRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, cur := range s.rules {
		if cur.RuleType == store.RuleBuiltin && cur.Locked {
			return nil
		}
	}
	r.ID = s.id()
	ts := now()
	r.CreatedAt, r.UpdatedAt = ts, ts
	r.RuleType = store.RuleBuiltin
	r.Locked = true
	r.Enabled = true
	s.rules[r.ID] = cloneRule(r)
	return nil
}

// TextSnapshot concatenates stored text so tests can prove a synopsis was not kept.
func (s *Store) TextSnapshot() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var b strings.Builder
	for _, w := range s.works {
		b.WriteString(w.Title)
		b.WriteByte('\n')
		b.WriteString(w.Description)
		b.WriteByte('\n')
		b.WriteString(w.PlayURL)
		b.WriteByte('\n')
		b.WriteString(w.CoverURL)
		b.WriteByte('\n')
		b.WriteString(w.Category)
		b.WriteByte('\n')
		b.WriteString(strings.Join(w.Tags, ","))
		b.WriteByte('\n')
	}
	for _, eps := range s.episodes {
		for _, ep := range eps {
			b.WriteString(ep.Title)
			b.WriteByte('\n')
			b.WriteString(ep.PlayURL)
			b.WriteByte('\n')
		}
	}
	for _, j := range s.jobs {
		b.WriteString(j.SkipReason)
		b.WriteByte('\n')
		b.WriteString(j.ErrorMessage)
		b.WriteByte('\n')
	}
	for _, items := range s.items {
		for _, it := range items {
			b.WriteString(it.ItemTitle)
			b.WriteByte('\n')
			b.WriteString(it.Reason)
			b.WriteByte('\n')
			b.WriteString(it.PlayURL)
			b.WriteByte('\n')
		}
	}
	for _, r := range s.rules {
		b.WriteString(r.Name)
		b.WriteByte('\n')
		b.WriteString(r.Pattern)
		b.WriteByte('\n')
		b.WriteString(r.Reason)
		b.WriteByte('\n')
	}
	return b.String()
}

func pageBounds(page, size, n int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	start := (page - 1) * size
	if start > n {
		start = n
	}
	end := start + size
	if end > n {
		end = n
	}
	return start, end
}

func clonePlatform(p store.Platform) store.Platform {
	if p.LastSyncedAt != nil {
		t := p.LastSyncedAt.UTC()
		p.LastSyncedAt = &t
	}
	return p
}

func cloneWork(w store.Work) store.Work {
	w.Tags = append([]string(nil), w.Tags...)
	w.Episodes = append([]store.Episode(nil), w.Episodes...)
	if w.PublishedAt != nil {
		t := w.PublishedAt.UTC()
		w.PublishedAt = &t
	}
	if w.LastSeenAt != nil {
		t := w.LastSeenAt.UTC()
		w.LastSeenAt = &t
	}
	return w
}

func cloneRule(r store.SkipRule) store.SkipRule {
	if r.PlatformID != nil {
		id := *r.PlatformID
		r.PlatformID = &id
	}
	return r
}

var _ store.Store = (*Store)(nil)

func (s *Store) CreateDownloadTask(_ context.Context, t store.DownloadTask) (store.DownloadTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t.ID = s.id()
	t.CreatedAt = now()
	t.UpdatedAt = t.CreatedAt
	if t.Status == "" {
		t.Status = store.DownloadQueued
	}
	if t.Status == store.DownloadRunning && t.StartedAt == nil {
		ts := t.CreatedAt
		t.StartedAt = &ts
	}
	s.downloads[t.ID] = t
	return t, nil
}

func (s *Store) GetDownloadTask(_ context.Context, id int64) (store.DownloadTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.downloads[id]
	if !ok {
		return store.DownloadTask{}, store.ErrNotFound
	}
	t.Items = append([]store.DownloadItem(nil), s.dlItems[id]...)
	return t, nil
}

func (s *Store) ListDownloadTasks(_ context.Context, workID int64, page, pageSize int) ([]store.DownloadTask, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var all []store.DownloadTask
	for _, t := range s.downloads {
		if workID != 0 && t.WorkID != workID {
			continue
		}
		all = append(all, t)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID > all[j].ID })
	total := len(all)
	start, end := pageBounds(page, pageSize, total)
	return all[start:end], total, nil
}

func (s *Store) UpdateDownloadTask(_ context.Context, t store.DownloadTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.downloads[t.ID]
	if !ok {
		return store.ErrNotFound
	}
	t.CreatedAt = cur.CreatedAt
	t.UpdatedAt = now()
	s.downloads[t.ID] = t
	return nil
}

func (s *Store) HasRunningDownload(_ context.Context, workID int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.downloads {
		if t.WorkID == workID && (t.Status == store.DownloadQueued || t.Status == store.DownloadRunning) {
			return true, nil
		}
	}
	return false, nil
}

func (s *Store) FailStaleDownloads(_ context.Context, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ts := now()
	for id, t := range s.downloads {
		if t.Status != store.DownloadQueued && t.Status != store.DownloadRunning {
			continue
		}
		t.Status = store.DownloadFailed
		t.ErrorMessage = reason
		t.FinishedAt = &ts
		t.UpdatedAt = ts
		s.downloads[id] = t
	}
	return nil
}

func (s *Store) AddDownloadItem(_ context.Context, it store.DownloadItem) (store.DownloadItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.downloads[it.TaskID]; !ok {
		return store.DownloadItem{}, store.ErrNotFound
	}
	if it.Status == "" {
		it.Status = store.DownloadQueued
	}
	it.ID = s.id()
	it.CreatedAt, it.UpdatedAt = now(), now()
	s.dlItems[it.TaskID] = append(s.dlItems[it.TaskID], it)
	return it, nil
}

func (s *Store) UpdateDownloadItem(_ context.Context, it store.DownloadItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.dlItems[it.TaskID]
	for i := range list {
		if list[i].ID != it.ID {
			continue
		}
		it.CreatedAt = list[i].CreatedAt
		it.UpdatedAt = now()
		list[i] = it
		s.dlItems[it.TaskID] = list
		return nil
	}
	return store.ErrNotFound
}

func (s *Store) ListDownloadItems(_ context.Context, taskID int64) ([]store.DownloadItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.downloads[taskID]; !ok {
		return nil, store.ErrNotFound
	}
	out := append([]store.DownloadItem(nil), s.dlItems[taskID]...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].EpisodeNo != out[j].EpisodeNo {
			return out[i].EpisodeNo < out[j].EpisodeNo
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}
