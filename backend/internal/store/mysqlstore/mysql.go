package mysqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	"catalog/internal/store"
)

type Store struct {
	db *sql.DB
}

func New(db *sql.DB) *Store { return &Store{db: db} }

func Open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)
	return db, nil
}

func isDup(err error) bool {
	var me *mysqldriver.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}

func now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

func (s *Store) CreateUser(ctx context.Context, username, passwordHash string) error {
	ts := now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO users (username, password_hash, created_at, updated_at) VALUES (?, ?, ?, ?)`, username, passwordHash, ts, ts)
	if isDup(err) {
		return store.ErrConflict
	}
	return err
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (store.User, error) {
	var u store.User
	err := s.db.QueryRowContext(ctx, `SELECT id, username, password_hash, created_at, updated_at FROM users WHERE username = ?`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return store.User{}, store.ErrNotFound
	}
	return u, err
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) ListPlatforms(ctx context.Context) ([]store.Platform, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+platformCols+` FROM platforms ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Platform
	for rows.Next() {
		p, err := scanPlatform(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetPlatform(ctx context.Context, id int64) (store.Platform, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+platformCols+` FROM platforms WHERE id = ?`, id)
	p, err := scanPlatform(row)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Platform{}, store.ErrNotFound
	}
	return p, err
}

func (s *Store) GetPlatformByCode(ctx context.Context, code string) (store.Platform, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+platformCols+` FROM platforms WHERE code = ?`, code)
	p, err := scanPlatform(row)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Platform{}, store.ErrNotFound
	}
	return p, err
}

func (s *Store) CreatePlatform(ctx context.Context, p store.Platform) (store.Platform, error) {
	ts := now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO platforms (code, name, base_url, enabled, cron_expr, sitemap_path,
		login_url, login_username, login_password, login_user_selector, login_pass_selector, login_submit_selector, browser_profile, cookie,
		created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Code, p.Name, p.BaseURL, boolInt(p.Enabled), p.CronExpr, p.SitemapPath,
		nullStr(p.LoginURL), nullStr(p.LoginUsername), nullStr(p.LoginPasswordEnc), nullStr(p.LoginUserSelector),
		nullStr(p.LoginPassSelector), nullStr(p.LoginSubmitSelector), nullStr(p.BrowserProfile), nullStr(p.Cookie),
		ts, ts)
	if isDup(err) {
		return store.Platform{}, store.ErrConflict
	}
	if err != nil {
		return store.Platform{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return store.Platform{}, err
	}
	return s.GetPlatform(ctx, id)
}

func (s *Store) UpdatePlatform(ctx context.Context, p store.Platform) error {
	res, err := s.db.ExecContext(ctx, `UPDATE platforms SET code=?, name=?, base_url=?, enabled=?, cron_expr=?, sitemap_path=?,
		login_url=?, login_username=?, login_password=?, login_user_selector=?, login_pass_selector=?, login_submit_selector=?,
		browser_profile=?, cookie=?, updated_at=? WHERE id=?`,
		p.Code, p.Name, p.BaseURL, boolInt(p.Enabled), p.CronExpr, p.SitemapPath,
		nullStr(p.LoginURL), nullStr(p.LoginUsername), nullStr(p.LoginPasswordEnc), nullStr(p.LoginUserSelector),
		nullStr(p.LoginPassSelector), nullStr(p.LoginSubmitSelector), nullStr(p.BrowserProfile), nullStr(p.Cookie),
		now(), p.ID)
	if isDup(err) {
		return store.ErrConflict
	}
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) DeletePlatform(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM platforms WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) TouchPlatformSync(ctx context.Context, id int64, at time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE platforms SET last_synced_at=?, updated_at=? WHERE id=?`, at.UTC(), now(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) UpsertWork(ctx context.Context, w store.Work) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	ts := now()
	seen := ts
	if w.LastSeenAt != nil {
		seen = w.LastSeenAt.UTC()
	}
	tags, err := json.Marshal(nonNil(w.Tags))
	if err != nil {
		return false, err
	}
	var existing int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM works WHERE platform_id=? AND external_id=?`, w.PlatformID, w.ExternalID).Scan(&existing)
	created := false
	if errors.Is(err, sql.ErrNoRows) {
		created = true
		res, err := tx.ExecContext(ctx, `INSERT INTO works (
			platform_id, external_id, title, description, cover_url, category, tags, author, play_url,
			published_at, view_count, like_count, favorite_count, comment_count, episode_count, absent_count,
			last_seen_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?)`,
			w.PlatformID, w.ExternalID, w.Title, nullStr(w.Description), nullStr(w.CoverURL), nullStr(w.Category), tags, nullStr(w.Author), w.PlayURL,
			nullTime(w.PublishedAt), w.ViewCount, w.LikeCount, w.FavoriteCount, w.CommentCount, len(w.Episodes), seen, ts, ts)
		if err != nil {
			return false, err
		}
		existing, err = res.LastInsertId()
		if err != nil {
			return false, err
		}
	} else if err != nil {
		return false, err
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE works SET title=?, description=?, cover_url=?, category=?, tags=?, author=?, play_url=?,
			published_at=?, view_count=?, like_count=?, favorite_count=?, comment_count=?, episode_count=?, absent_count=0,
			last_seen_at=?, updated_at=? WHERE id=?`,
			w.Title, nullStr(w.Description), nullStr(w.CoverURL), nullStr(w.Category), tags, nullStr(w.Author), w.PlayURL,
			nullTime(w.PublishedAt), w.ViewCount, w.LikeCount, w.FavoriteCount, w.CommentCount, len(w.Episodes), seen, ts, existing)
		if err != nil {
			return false, err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM episodes WHERE work_id=?`, existing); err != nil {
		return false, err
	}
	for _, ep := range w.Episodes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO episodes (work_id, platform_id, episode_no, title, play_url, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			existing, w.PlatformID, ep.EpisodeNo, ep.Title, ep.PlayURL, ts, ts); err != nil {
			return false, err
		}
	}
	return created, tx.Commit()
}

func (s *Store) ClearWorkDescription(ctx context.Context, platformID int64, externalID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE works SET description=NULL, updated_at=? WHERE platform_id=? AND external_id=?`, now(), platformID, externalID)
	return err
}

func (s *Store) DeleteWorkByExternal(ctx context.Context, platformID int64, externalID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM works WHERE platform_id=? AND external_id=?`, platformID, externalID)
	return err
}

func (s *Store) GetWork(ctx context.Context, id int64) (store.Work, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+workCols+` FROM works WHERE id=?`, id)
	w, err := scanWork(row)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Work{}, store.ErrNotFound
	}
	if err != nil {
		return store.Work{}, err
	}
	w.Episodes, err = s.episodes(ctx, w.ID)
	return w, err
}

func (s *Store) ListWorks(ctx context.Context, f store.WorkFilter) ([]store.Work, int, error) {
	page, size := normPage(f.Page, f.PageSize)
	like := "%" + escapeLike(strings.TrimSpace(f.Query)) + "%"
	q := strings.TrimSpace(f.Query)
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM works WHERE (?=0 OR platform_id=?) AND (?='' OR title LIKE ? OR external_id LIKE ?)`,
		f.PlatformID, f.PlatformID, q, like, like).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+workCols+` FROM works WHERE (?=0 OR platform_id=?) AND (?='' OR title LIKE ? OR external_id LIKE ?)
		ORDER BY updated_at DESC, id DESC LIMIT ? OFFSET ?`,
		f.PlatformID, f.PlatformID, q, like, like, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []store.Work
	for rows.Next() {
		w, err := scanWork(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, w)
	}
	return out, total, rows.Err()
}

func (s *Store) ListWorksForAbsent(ctx context.Context, platformID int64) ([]store.WorkRef, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT external_id, title, play_url, absent_count FROM works WHERE platform_id=? ORDER BY external_id`, platformID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.WorkRef
	for rows.Next() {
		var ref store.WorkRef
		var play sql.NullString
		if err := rows.Scan(&ref.ExternalID, &ref.Title, &play, &ref.AbsentCount); err != nil {
			return nil, err
		}
		ref.PlayURL = play.String
		out = append(out, ref)
	}
	return out, rows.Err()
}

func (s *Store) AddAbsent(ctx context.Context, platformID int64, externalID string) (string, string, int, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE works SET absent_count = absent_count + 1, updated_at=? WHERE platform_id=? AND external_id=?`, now(), platformID, externalID)
	if err != nil {
		return "", "", 0, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return "", "", 0, store.ErrNotFound
	}
	var title, play string
	var count int
	err = s.db.QueryRowContext(ctx, `SELECT title, play_url, absent_count FROM works WHERE platform_id=? AND external_id=?`, platformID, externalID).Scan(&title, &play, &count)
	return title, play, count, err
}

func (s *Store) CreateJob(ctx context.Context, j store.SyncJob) (store.SyncJob, error) {
	ts := now()
	if j.StartedAt == nil && j.Status == store.StatusRunning {
		j.StartedAt = &ts
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO sync_jobs (
		platform_id, mode, trigger_type, status, skip_reason, started_at, finished_at,
		discovered_count, created_count, updated_count, skipped_count, failed_count, absent_count, error_message, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		j.PlatformID, j.Mode, j.TriggerType, j.Status, nullStr(j.SkipReason), nullTime(j.StartedAt), nullTime(j.FinishedAt),
		j.DiscoveredCount, j.CreatedCount, j.UpdatedCount, j.SkippedCount, j.FailedCount, j.AbsentCount, nullStr(j.ErrorMessage), ts)
	if err != nil {
		return store.SyncJob{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return store.SyncJob{}, err
	}
	return s.GetJob(ctx, id)
}

func (s *Store) GetJob(ctx context.Context, id int64) (store.SyncJob, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+jobCols+` FROM sync_jobs WHERE id=?`, id)
	j, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return store.SyncJob{}, store.ErrNotFound
	}
	return j, err
}

func (s *Store) ListJobs(ctx context.Context, platformID int64, page, pageSize int) ([]store.SyncJob, int, error) {
	page, size := normPage(page, pageSize)
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sync_jobs WHERE (?=0 OR platform_id=?)`, platformID, platformID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+jobCols+` FROM sync_jobs WHERE (?=0 OR platform_id=?) ORDER BY id DESC LIMIT ? OFFSET ?`,
		platformID, platformID, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []store.SyncJob
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, j)
	}
	return out, total, rows.Err()
}

func (s *Store) HasRunningJob(ctx context.Context, platformID int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sync_jobs WHERE platform_id=? AND status=?`, platformID, store.StatusRunning).Scan(&n)
	return n > 0, err
}

func (s *Store) FinishJob(ctx context.Context, j store.SyncJob) error {
	res, err := s.db.ExecContext(ctx, `UPDATE sync_jobs SET status=?, skip_reason=?, started_at=?, finished_at=?,
		discovered_count=?, created_count=?, updated_count=?, skipped_count=?, failed_count=?, absent_count=?, error_message=? WHERE id=?`,
		j.Status, nullStr(j.SkipReason), nullTime(j.StartedAt), nullTime(j.FinishedAt),
		j.DiscoveredCount, j.CreatedCount, j.UpdatedCount, j.SkippedCount, j.FailedCount, j.AbsentCount, nullStr(j.ErrorMessage), j.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) FailStaleJobs(ctx context.Context, reason string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sync_jobs SET status=?, error_message=?, finished_at=? WHERE status=?`,
		store.StatusFailed, reason, now(), store.StatusRunning)
	return err
}

func (s *Store) AddJobItem(ctx context.Context, it store.SyncJobItem) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sync_job_items (job_id, external_id, item_title, action, reason, play_url, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		it.JobID, it.ExternalID, clip(it.ItemTitle, 500), it.Action, nullStr(clip(it.Reason, 240)), nullStr(it.PlayURL), now())
	return err
}

func (s *Store) ListJobItems(ctx context.Context, jobID int64, page, pageSize int) ([]store.SyncJobItem, int, error) {
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sync_jobs WHERE id=?`, jobID).Scan(&exists); err != nil {
		return nil, 0, err
	}
	if exists == 0 {
		return nil, 0, store.ErrNotFound
	}
	page, size := normPage(page, pageSize)
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sync_job_items WHERE job_id=?`, jobID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, job_id, external_id, item_title, action, reason, play_url, created_at FROM sync_job_items WHERE job_id=? ORDER BY id LIMIT ? OFFSET ?`,
		jobID, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []store.SyncJobItem
	for rows.Next() {
		var it store.SyncJobItem
		var reason, play sql.NullString
		if err := rows.Scan(&it.ID, &it.JobID, &it.ExternalID, &it.ItemTitle, &it.Action, &reason, &play, &it.CreatedAt); err != nil {
			return nil, 0, err
		}
		it.Reason = reason.String
		it.PlayURL = play.String
		out = append(out, it)
	}
	return out, total, rows.Err()
}

func (s *Store) ListSkipRules(ctx context.Context) ([]store.SkipRule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+ruleCols+` FROM skip_rules ORDER BY priority, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.SkipRule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetSkipRule(ctx context.Context, id int64) (store.SkipRule, error) {
	r, err := scanRule(s.db.QueryRowContext(ctx, `SELECT `+ruleCols+` FROM skip_rules WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return store.SkipRule{}, store.ErrNotFound
	}
	return r, err
}

func (s *Store) CreateSkipRule(ctx context.Context, r store.SkipRule) (store.SkipRule, error) {
	if r.Locked || r.RuleType == store.RuleBuiltin {
		return store.SkipRule{}, store.ErrRuleLocked
	}
	ts := now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO skip_rules (platform_id, name, rule_type, target, pattern, reason, enabled, locked, priority, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?)`, nullID(r.PlatformID), r.Name, r.RuleType, r.Target, r.Pattern, r.Reason, boolInt(r.Enabled), r.Priority, ts, ts)
	if err != nil {
		return store.SkipRule{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return store.SkipRule{}, err
	}
	return s.GetSkipRule(ctx, id)
}

func (s *Store) UpdateSkipRule(ctx context.Context, r store.SkipRule) error {
	cur, err := s.GetSkipRule(ctx, r.ID)
	if err != nil {
		return err
	}
	if cur.Locked || cur.RuleType == store.RuleBuiltin || r.Locked || r.RuleType == store.RuleBuiltin {
		return store.ErrRuleLocked
	}
	res, err := s.db.ExecContext(ctx, `UPDATE skip_rules SET platform_id=?, name=?, rule_type=?, target=?, pattern=?, reason=?, enabled=?, locked=0, priority=?, updated_at=? WHERE id=?`,
		nullID(r.PlatformID), r.Name, r.RuleType, r.Target, r.Pattern, r.Reason, boolInt(r.Enabled), r.Priority, now(), r.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) DeleteSkipRule(ctx context.Context, id int64) error {
	cur, err := s.GetSkipRule(ctx, id)
	if err != nil {
		return err
	}
	if cur.Locked || cur.RuleType == store.RuleBuiltin {
		return store.ErrRuleLocked
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM skip_rules WHERE id=?`, id)
	return err
}

func (s *Store) EnsureBuiltinRule(ctx context.Context, r store.SkipRule) error {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM skip_rules WHERE rule_type=? AND locked=1`, store.RuleBuiltin).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	ts := now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO skip_rules (platform_id, name, rule_type, target, pattern, reason, enabled, locked, priority, created_at, updated_at)
		VALUES (NULL, ?, ?, ?, ?, ?, 1, 1, ?, ?, ?)`, r.Name, store.RuleBuiltin, r.Target, r.Pattern, r.Reason, r.Priority, ts, ts)
	return err
}

func (s *Store) episodes(ctx context.Context, workID int64) ([]store.Episode, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, work_id, platform_id, episode_no, title, play_url, created_at, updated_at FROM episodes WHERE work_id=? ORDER BY episode_no`, workID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Episode
	for rows.Next() {
		var ep store.Episode
		if err := rows.Scan(&ep.ID, &ep.WorkID, &ep.PlatformID, &ep.EpisodeNo, &ep.Title, &ep.PlayURL, &ep.CreatedAt, &ep.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, ep)
	}
	return out, rows.Err()
}

const platformCols = `id, code, name, base_url, enabled, cron_expr, sitemap_path, last_synced_at, created_at, updated_at,
	login_url, login_username, login_password, login_user_selector, login_pass_selector, login_submit_selector, browser_profile, cookie`
const workCols = `id, platform_id, external_id, title, description, cover_url, category, tags, author, play_url, published_at, view_count, like_count, favorite_count, comment_count, episode_count, absent_count, last_seen_at, created_at, updated_at`
const jobCols = `id, platform_id, mode, trigger_type, status, skip_reason, started_at, finished_at, discovered_count, created_count, updated_count, skipped_count, failed_count, absent_count, error_message, created_at`
const ruleCols = `id, platform_id, name, rule_type, target, pattern, reason, enabled, locked, priority, created_at, updated_at`

type rower interface{ Scan(dest ...any) error }

func scanPlatform(row rower) (store.Platform, error) {
	var p store.Platform
	var enabled int
	var last sql.NullTime
	var loginURL, loginUser, loginPass, userSel, passSel, submitSel, profile, cookie sql.NullString
	err := row.Scan(&p.ID, &p.Code, &p.Name, &p.BaseURL, &enabled, &p.CronExpr, &p.SitemapPath, &last, &p.CreatedAt, &p.UpdatedAt,
		&loginURL, &loginUser, &loginPass, &userSel, &passSel, &submitSel, &profile, &cookie)
	if err != nil {
		return store.Platform{}, err
	}
	p.Enabled = enabled == 1
	p.LastSyncedAt = timePtr(last)
	p.LoginURL, p.LoginUsername, p.LoginPasswordEnc = loginURL.String, loginUser.String, loginPass.String
	p.LoginUserSelector, p.LoginPassSelector, p.LoginSubmitSelector = userSel.String, passSel.String, submitSel.String
	p.BrowserProfile, p.Cookie = profile.String, cookie.String
	return p, nil
}

func scanWork(row rower) (store.Work, error) {
	var w store.Work
	var desc, cover, category, author sql.NullString
	var tags []byte
	var published, seen sql.NullTime
	err := row.Scan(&w.ID, &w.PlatformID, &w.ExternalID, &w.Title, &desc, &cover, &category, &tags, &author, &w.PlayURL, &published,
		&w.ViewCount, &w.LikeCount, &w.FavoriteCount, &w.CommentCount, &w.EpisodeCount, &w.AbsentCount, &seen, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return store.Work{}, err
	}
	w.Description = desc.String
	w.CoverURL = cover.String
	w.Category = category.String
	w.Author = author.String
	w.PublishedAt = timePtr(published)
	w.LastSeenAt = timePtr(seen)
	if len(tags) > 0 {
		_ = json.Unmarshal(tags, &w.Tags)
	}
	return w, nil
}

func scanJob(row rower) (store.SyncJob, error) {
	var j store.SyncJob
	var skip, errMsg sql.NullString
	var started, finished sql.NullTime
	err := row.Scan(&j.ID, &j.PlatformID, &j.Mode, &j.TriggerType, &j.Status, &skip, &started, &finished,
		&j.DiscoveredCount, &j.CreatedCount, &j.UpdatedCount, &j.SkippedCount, &j.FailedCount, &j.AbsentCount, &errMsg, &j.CreatedAt)
	if err != nil {
		return store.SyncJob{}, err
	}
	j.SkipReason = skip.String
	j.ErrorMessage = errMsg.String
	j.StartedAt = timePtr(started)
	j.FinishedAt = timePtr(finished)
	return j, nil
}

func scanRule(row rower) (store.SkipRule, error) {
	var r store.SkipRule
	var platform sql.NullInt64
	var enabled, locked int
	err := row.Scan(&r.ID, &platform, &r.Name, &r.RuleType, &r.Target, &r.Pattern, &r.Reason, &enabled, &locked, &r.Priority, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return store.SkipRule{}, err
	}
	r.Enabled = enabled == 1
	r.Locked = locked == 1
	if platform.Valid {
		id := platform.Int64
		r.PlatformID = &id
	}
	return r, nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func nullStr(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func nullTime(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.UTC()
}

func nullID(id *int64) any {
	if id == nil || *id == 0 {
		return nil
	}
	return *id
}

func timePtr(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time.UTC()
	return &t
}

func nonNil(tags []string) []string {
	if tags == nil {
		return []string{}
	}
	return tags
}

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func normPage(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return page, size
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

var _ store.Store = (*Store)(nil)

const downloadTaskCols = `id, work_id, platform_id, status, total_episodes, completed_episodes,
	failed_episodes, quality, output_dir, error_message, started_at, finished_at, created_at, updated_at`

const downloadItemCols = `id, task_id, episode_id, episode_no, title, page_url, stream_url,
	file_path, size_bytes, duration_seconds, status, error_message, created_at, updated_at`

func (s *Store) CreateDownloadTask(ctx context.Context, t store.DownloadTask) (store.DownloadTask, error) {
	ts := now()
	if t.Status == "" {
		t.Status = store.DownloadQueued
	}
	if t.StartedAt == nil && t.Status == store.DownloadRunning {
		t.StartedAt = &ts
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO download_tasks (
		work_id, platform_id, status, total_episodes, completed_episodes, failed_episodes,
		quality, output_dir, error_message, started_at, finished_at, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.WorkID, t.PlatformID, t.Status, t.TotalEpisodes, t.CompletedEpisodes, t.FailedEpisodes,
		clip(t.Quality, 16), clip(t.OutputDir, 1000), nullStr(clip(t.ErrorMessage, 500)), nullTime(t.StartedAt), nullTime(t.FinishedAt), ts, ts)
	if err != nil {
		return store.DownloadTask{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return store.DownloadTask{}, err
	}
	return s.GetDownloadTask(ctx, id)
}

func (s *Store) GetDownloadTask(ctx context.Context, id int64) (store.DownloadTask, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+downloadTaskCols+` FROM download_tasks WHERE id=?`, id)
	t, err := scanDownloadTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return store.DownloadTask{}, store.ErrNotFound
	}
	if err != nil {
		return store.DownloadTask{}, err
	}
	items, err := s.ListDownloadItems(ctx, id)
	if err != nil {
		return store.DownloadTask{}, err
	}
	t.Items = items
	return t, nil
}

func (s *Store) ListDownloadTasks(ctx context.Context, workID int64, page, pageSize int) ([]store.DownloadTask, int, error) {
	page, size := normPage(page, pageSize)
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM download_tasks WHERE (?=0 OR work_id=?)`, workID, workID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+downloadTaskCols+` FROM download_tasks WHERE (?=0 OR work_id=?) ORDER BY id DESC LIMIT ? OFFSET ?`,
		workID, workID, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []store.DownloadTask
	for rows.Next() {
		t, err := scanDownloadTask(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, t)
	}
	return out, total, rows.Err()
}

func (s *Store) UpdateDownloadTask(ctx context.Context, t store.DownloadTask) error {
	res, err := s.db.ExecContext(ctx, `UPDATE download_tasks SET status=?, total_episodes=?, completed_episodes=?,
		failed_episodes=?, quality=?, output_dir=?, error_message=?, started_at=?, finished_at=?, updated_at=? WHERE id=?`,
		t.Status, t.TotalEpisodes, t.CompletedEpisodes, t.FailedEpisodes, clip(t.Quality, 16),
		clip(t.OutputDir, 1000), nullStr(clip(t.ErrorMessage, 500)), nullTime(t.StartedAt), nullTime(t.FinishedAt), now(), t.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) HasRunningDownload(ctx context.Context, workID int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM download_tasks WHERE work_id=? AND status IN (?, ?)`,
		workID, store.DownloadQueued, store.DownloadRunning).Scan(&n)
	return n > 0, err
}

func (s *Store) FailStaleDownloads(ctx context.Context, reason string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE download_tasks SET status=?, error_message=?, finished_at=?, updated_at=?
		WHERE status IN (?, ?)`,
		store.DownloadFailed, reason, now(), now(), store.DownloadQueued, store.DownloadRunning)
	return err
}

func (s *Store) AddDownloadItem(ctx context.Context, it store.DownloadItem) (store.DownloadItem, error) {
	ts := now()
	if it.Status == "" {
		it.Status = store.DownloadQueued
	}
	var episodeID any
	if it.EpisodeID > 0 {
		episodeID = it.EpisodeID
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO download_items (
		task_id, episode_id, episode_no, title, page_url, stream_url, file_path,
		size_bytes, duration_seconds, status, error_message, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		it.TaskID, episodeID, it.EpisodeNo, clip(it.Title, 240), clip(it.PageURL, 1000),
		clip(it.StreamURL, 2000), clip(it.FilePath, 1000), it.SizeBytes, it.DurationSeconds,
		it.Status, nullStr(clip(it.ErrorMessage, 500)), ts, ts)
	if err != nil {
		return store.DownloadItem{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return store.DownloadItem{}, err
	}
	it.ID = id
	it.CreatedAt, it.UpdatedAt = ts, ts
	return it, nil
}

func (s *Store) UpdateDownloadItem(ctx context.Context, it store.DownloadItem) error {
	var episodeID any
	if it.EpisodeID > 0 {
		episodeID = it.EpisodeID
	}
	res, err := s.db.ExecContext(ctx, `UPDATE download_items SET episode_id=?, title=?, page_url=?, stream_url=?,
		file_path=?, size_bytes=?, duration_seconds=?, status=?, error_message=?, updated_at=? WHERE id=?`,
		episodeID, clip(it.Title, 240), clip(it.PageURL, 1000), clip(it.StreamURL, 2000),
		clip(it.FilePath, 1000), it.SizeBytes, it.DurationSeconds, it.Status,
		nullStr(clip(it.ErrorMessage, 500)), now(), it.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) ListDownloadItems(ctx context.Context, taskID int64) ([]store.DownloadItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+downloadItemCols+` FROM download_items WHERE task_id=? ORDER BY episode_no, id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.DownloadItem
	for rows.Next() {
		it, err := scanDownloadItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func scanDownloadTask(row rower) (store.DownloadTask, error) {
	var t store.DownloadTask
	var errMsg sql.NullString
	var started, finished sql.NullTime
	err := row.Scan(&t.ID, &t.WorkID, &t.PlatformID, &t.Status, &t.TotalEpisodes, &t.CompletedEpisodes,
		&t.FailedEpisodes, &t.Quality, &t.OutputDir, &errMsg, &started, &finished, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return store.DownloadTask{}, err
	}
	t.ErrorMessage = errMsg.String
	t.StartedAt = timePtr(started)
	t.FinishedAt = timePtr(finished)
	return t, nil
}

func scanDownloadItem(row rower) (store.DownloadItem, error) {
	var it store.DownloadItem
	var episodeID sql.NullInt64
	var pageURL, streamURL, filePath, errMsg sql.NullString
	err := row.Scan(&it.ID, &it.TaskID, &episodeID, &it.EpisodeNo, &it.Title, &pageURL, &streamURL,
		&filePath, &it.SizeBytes, &it.DurationSeconds, &it.Status, &errMsg, &it.CreatedAt, &it.UpdatedAt)
	if err != nil {
		return store.DownloadItem{}, err
	}
	if episodeID.Valid {
		it.EpisodeID = episodeID.Int64
	}
	it.PageURL, it.StreamURL, it.FilePath, it.ErrorMessage = pageURL.String, streamURL.String, filePath.String, errMsg.String
	return it, nil
}
