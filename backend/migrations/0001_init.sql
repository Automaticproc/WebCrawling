-- Domain tables: users, platforms, works, episodes, sync_jobs, sync_job_items, skip_rules.
-- Datetimes are UTC. The admin UI and the scheduler use Asia/Hong_Kong.

CREATE TABLE users (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  username VARCHAR(64) NOT NULL,
  password_hash VARCHAR(255) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_users_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE platforms (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  code VARCHAR(64) NOT NULL,
  name VARCHAR(128) NOT NULL,
  base_url VARCHAR(255) NOT NULL,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  cron_expr VARCHAR(64) NOT NULL DEFAULT '0 8 * * *',
  sitemap_path VARCHAR(255) NOT NULL DEFAULT '/sitemap.xml',
  last_synced_at DATETIME(6) NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_platforms_code (code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE works (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  platform_id BIGINT UNSIGNED NOT NULL,
  external_id VARCHAR(64) NOT NULL,
  title VARCHAR(512) NOT NULL,
  description TEXT NULL,
  cover_url VARCHAR(1024) NULL,
  category VARCHAR(128) NULL,
  tags JSON NULL,
  author VARCHAR(128) NULL,
  play_url VARCHAR(1024) NOT NULL,
  published_at DATETIME(6) NULL,
  view_count BIGINT NOT NULL DEFAULT 0,
  like_count BIGINT NOT NULL DEFAULT 0,
  favorite_count BIGINT NOT NULL DEFAULT 0,
  comment_count BIGINT NOT NULL DEFAULT 0,
  episode_count INT NOT NULL DEFAULT 0,
  absent_count INT NOT NULL DEFAULT 0,
  last_seen_at DATETIME(6) NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_works_platform_external (platform_id, external_id),
  KEY idx_works_platform_updated (platform_id, updated_at),
  CONSTRAINT fk_works_platform FOREIGN KEY (platform_id) REFERENCES platforms (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE episodes (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  work_id BIGINT UNSIGNED NOT NULL,
  platform_id BIGINT UNSIGNED NOT NULL,
  episode_no INT NOT NULL,
  title VARCHAR(255) NOT NULL,
  play_url VARCHAR(1024) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_episodes_work_no (work_id, episode_no),
  CONSTRAINT fk_episodes_work FOREIGN KEY (work_id) REFERENCES works (id) ON DELETE CASCADE,
  CONSTRAINT fk_episodes_platform FOREIGN KEY (platform_id) REFERENCES platforms (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE sync_jobs (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  platform_id BIGINT UNSIGNED NOT NULL,
  mode VARCHAR(16) NOT NULL,
  trigger_type VARCHAR(16) NOT NULL,
  status VARCHAR(16) NOT NULL,
  skip_reason VARCHAR(255) NULL,
  started_at DATETIME(6) NULL,
  finished_at DATETIME(6) NULL,
  discovered_count INT NOT NULL DEFAULT 0,
  created_count INT NOT NULL DEFAULT 0,
  updated_count INT NOT NULL DEFAULT 0,
  skipped_count INT NOT NULL DEFAULT 0,
  failed_count INT NOT NULL DEFAULT 0,
  absent_count INT NOT NULL DEFAULT 0,
  error_message VARCHAR(512) NULL,
  created_at DATETIME(6) NOT NULL,
  PRIMARY KEY (id),
  KEY idx_sync_jobs_platform_status (platform_id, status),
  KEY idx_sync_jobs_created (created_at),
  CONSTRAINT fk_sync_jobs_platform FOREIGN KEY (platform_id) REFERENCES platforms (id) ON DELETE CASCADE,
  CONSTRAINT chk_sync_jobs_mode CHECK (mode IN ('full', 'incremental')),
  CONSTRAINT chk_sync_jobs_trigger CHECK (trigger_type IN ('manual', 'schedule')),
  CONSTRAINT chk_sync_jobs_status CHECK (status IN ('running', 'succeeded', 'failed', 'skipped'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE sync_job_items (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  job_id BIGINT UNSIGNED NOT NULL,
  external_id VARCHAR(64) NOT NULL,
  item_title VARCHAR(512) NOT NULL,
  action VARCHAR(16) NOT NULL,
  reason VARCHAR(255) NULL,
  play_url VARCHAR(1024) NULL,
  created_at DATETIME(6) NOT NULL,
  PRIMARY KEY (id),
  KEY idx_sync_job_items_job (job_id, id),
  CONSTRAINT fk_sync_job_items_job FOREIGN KEY (job_id) REFERENCES sync_jobs (id) ON DELETE CASCADE,
  CONSTRAINT chk_sync_job_items_action CHECK (action IN ('created', 'updated', 'skipped', 'failed', 'absent'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE skip_rules (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  platform_id BIGINT UNSIGNED NULL,
  name VARCHAR(128) NOT NULL,
  rule_type VARCHAR(16) NOT NULL,
  target VARCHAR(16) NOT NULL,
  pattern VARCHAR(512) NOT NULL,
  reason VARCHAR(255) NOT NULL,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  locked TINYINT(1) NOT NULL DEFAULT 0,
  priority INT NOT NULL DEFAULT 100,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  PRIMARY KEY (id),
  KEY idx_skip_rules_platform (platform_id, priority),
  CONSTRAINT fk_skip_rules_platform FOREIGN KEY (platform_id) REFERENCES platforms (id) ON DELETE CASCADE,
  CONSTRAINT chk_skip_rules_type CHECK (rule_type IN ('builtin', 'keyword', 'regex')),
  CONSTRAINT chk_skip_rules_target CHECK (target IN ('title', 'tags', 'description', 'all'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
