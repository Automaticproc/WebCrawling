// Package store defines the catalog persistence model.
// Datetimes are UTC. There is no history table for interaction counts:
// view/like/favorite/comment values on a work are the latest snapshot only.
package store

import (
	"context"
	"errors"
	"time"
)

const (
	ModeFull        = "full"
	ModeIncremental = "incremental"

	TriggerManual   = "manual"
	TriggerSchedule = "schedule"

	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusSkipped   = "skipped"

	ActionCreated = "created"
	ActionUpdated = "updated"
	ActionSkipped = "skipped"
	ActionFailed  = "failed"
	ActionAbsent  = "absent"

	RuleBuiltin = "builtin"
	RuleKeyword = "keyword"
	RuleRegex   = "regex"

	TargetTitle       = "title"
	TargetTags        = "tags"
	TargetDescription = "description"
	TargetAll         = "all"

	OverlapReason = "同一平台已有进行中的同步，本次已跳过"
)

var (
	ErrNotFound   = errors.New("不存在")
	ErrConflict   = errors.New("已存在")
	ErrRuleLocked = errors.New("内置规则不可修改或停用")
)

type User struct {
	ID           int64
	Username     string
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Platform struct {
	ID           int64
	Code         string
	Name         string
	BaseURL      string
	Enabled      bool
	CronExpr     string
	SitemapPath  string
	LastSyncedAt *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Episode struct {
	ID         int64
	WorkID     int64
	PlatformID int64
	EpisodeNo  int
	Title      string
	PlayURL    string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type Work struct {
	ID            int64
	PlatformID    int64
	ExternalID    string
	Title         string
	Description   string
	CoverURL      string
	Category      string
	Tags          []string
	Author        string
	PlayURL       string
	PublishedAt   *time.Time
	ViewCount     int64
	LikeCount     int64
	FavoriteCount int64
	CommentCount  int64
	EpisodeCount  int
	AbsentCount   int
	LastSeenAt    *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Episodes      []Episode
}

type WorkFilter struct {
	PlatformID int64
	Query      string
	Page       int
	PageSize   int
}

type WorkRef struct {
	ExternalID  string
	Title       string
	PlayURL     string
	AbsentCount int
}

type SyncJob struct {
	ID              int64
	PlatformID      int64
	Mode            string
	TriggerType     string
	Status          string
	SkipReason      string
	StartedAt       *time.Time
	FinishedAt      *time.Time
	DiscoveredCount int
	CreatedCount    int
	UpdatedCount    int
	SkippedCount    int
	FailedCount     int
	AbsentCount     int
	ErrorMessage    string
	CreatedAt       time.Time
}

type SyncJobItem struct {
	ID         int64
	JobID      int64
	ExternalID string
	ItemTitle  string
	Action     string
	Reason     string
	PlayURL    string
	CreatedAt  time.Time
}

type SkipRule struct {
	ID         int64
	PlatformID *int64
	Name       string
	RuleType   string
	Target     string
	Pattern    string
	Reason     string
	Enabled    bool
	Locked     bool
	Priority   int
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Store is the persistence port used by HTTP handlers and the sync engine.
type Store interface {
	CreateUser(ctx context.Context, username, passwordHash string) error
	GetUserByUsername(ctx context.Context, username string) (User, error)
	CountUsers(ctx context.Context) (int, error)

	ListPlatforms(ctx context.Context) ([]Platform, error)
	GetPlatform(ctx context.Context, id int64) (Platform, error)
	GetPlatformByCode(ctx context.Context, code string) (Platform, error)
	CreatePlatform(ctx context.Context, p Platform) (Platform, error)
	UpdatePlatform(ctx context.Context, p Platform) error
	DeletePlatform(ctx context.Context, id int64) error
	TouchPlatformSync(ctx context.Context, id int64, at time.Time) error

	UpsertWork(ctx context.Context, w Work) (created bool, err error)
	ClearWorkDescription(ctx context.Context, platformID int64, externalID string) error
	DeleteWorkByExternal(ctx context.Context, platformID int64, externalID string) error
	GetWork(ctx context.Context, id int64) (Work, error)
	ListWorks(ctx context.Context, f WorkFilter) ([]Work, int, error)
	ListWorksForAbsent(ctx context.Context, platformID int64) ([]WorkRef, error)
	AddAbsent(ctx context.Context, platformID int64, externalID string) (title, playURL string, count int, err error)

	CreateJob(ctx context.Context, j SyncJob) (SyncJob, error)
	GetJob(ctx context.Context, id int64) (SyncJob, error)
	ListJobs(ctx context.Context, platformID int64, page, pageSize int) ([]SyncJob, int, error)
	HasRunningJob(ctx context.Context, platformID int64) (bool, error)
	FinishJob(ctx context.Context, j SyncJob) error
	FailStaleJobs(ctx context.Context, reason string) error
	AddJobItem(ctx context.Context, it SyncJobItem) error
	ListJobItems(ctx context.Context, jobID int64, page, pageSize int) ([]SyncJobItem, int, error)

	ListSkipRules(ctx context.Context) ([]SkipRule, error)
	GetSkipRule(ctx context.Context, id int64) (SkipRule, error)
	CreateSkipRule(ctx context.Context, r SkipRule) (SkipRule, error)
	UpdateSkipRule(ctx context.Context, r SkipRule) error
	DeleteSkipRule(ctx context.Context, id int64) error
	EnsureBuiltinRule(ctx context.Context, r SkipRule) error
}
