-- Download tasks: one row per "download every episode of a work" request.
-- Items are per episode files. Datetimes are UTC.

CREATE TABLE download_tasks (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  work_id BIGINT UNSIGNED NOT NULL,
  platform_id BIGINT UNSIGNED NOT NULL,
  status VARCHAR(16) NOT NULL,
  total_episodes INT NOT NULL DEFAULT 0,
  completed_episodes INT NOT NULL DEFAULT 0,
  failed_episodes INT NOT NULL DEFAULT 0,
  quality VARCHAR(16) NOT NULL DEFAULT 'best',
  output_dir VARCHAR(1024) NOT NULL DEFAULT '',
  error_message VARCHAR(512) NULL,
  started_at DATETIME(6) NULL,
  finished_at DATETIME(6) NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  PRIMARY KEY (id),
  KEY idx_download_tasks_work (work_id, id),
  KEY idx_download_tasks_status (status),
  CONSTRAINT fk_download_tasks_work FOREIGN KEY (work_id) REFERENCES works (id) ON DELETE CASCADE,
  CONSTRAINT fk_download_tasks_platform FOREIGN KEY (platform_id) REFERENCES platforms (id) ON DELETE CASCADE,
  CONSTRAINT chk_download_tasks_status CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'canceled'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE download_items (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  task_id BIGINT UNSIGNED NOT NULL,
  episode_id BIGINT UNSIGNED NULL,
  episode_no INT NOT NULL,
  title VARCHAR(255) NOT NULL,
  page_url VARCHAR(1024) NOT NULL DEFAULT '',
  stream_url VARCHAR(2048) NOT NULL DEFAULT '',
  file_path VARCHAR(1024) NOT NULL DEFAULT '',
  size_bytes BIGINT NOT NULL DEFAULT 0,
  duration_seconds DOUBLE NOT NULL DEFAULT 0,
  status VARCHAR(16) NOT NULL,
  error_message VARCHAR(512) NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  PRIMARY KEY (id),
  KEY idx_download_items_task (task_id, episode_no),
  CONSTRAINT fk_download_items_task FOREIGN KEY (task_id) REFERENCES download_tasks (id) ON DELETE CASCADE,
  CONSTRAINT fk_download_items_episode FOREIGN KEY (episode_id) REFERENCES episodes (id) ON DELETE SET NULL,
  CONSTRAINT chk_download_items_status CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'canceled'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
