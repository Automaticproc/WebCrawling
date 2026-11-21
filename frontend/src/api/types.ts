export interface Platform {
  id: number;
  code: string;
  name: string;
  base_url: string;
  enabled: boolean;
  cron_expr: string;
  sitemap_path: string;
  supported: boolean;
  last_synced_at: string | null;
  next_run_at: string | null;
  created_at: string;
  updated_at: string;
  login_url: string;
  login_username: string;
  login_user_selector: string;
  login_pass_selector: string;
  login_submit_selector: string;
  browser_profile: string;
  has_login_password: boolean;
  has_cookie: boolean;
  // 仅写入时使用，接口不会回传
  login_password?: string;
  cookie?: string;
  clear_login_password?: boolean;
  clear_cookie?: boolean;
}

export interface Episode {
  id: number;
  episode_no: number;
  title: string;
  play_url: string;
}

export interface Work {
  id: number;
  platform_id: number;
  external_id: string;
  title: string;
  description: string;
  cover_url: string;
  category: string;
  tags: string[];
  author: string;
  play_url: string;
  published_at: string | null;
  view_count: number;
  like_count: number;
  favorite_count: number;
  comment_count: number;
  episode_count: number;
  absent_count: number;
  last_seen_at: string | null;
  created_at: string;
  updated_at: string;
  episodes?: Episode[];
}

export interface SyncJob {
  id: number;
  platform_id: number;
  mode: "full" | "incremental";
  trigger_type: "manual" | "schedule";
  status: "running" | "succeeded" | "failed" | "skipped";
  skip_reason: string;
  started_at: string | null;
  finished_at: string | null;
  discovered_count: number;
  created_count: number;
  updated_count: number;
  skipped_count: number;
  failed_count: number;
  absent_count: number;
  error_message: string;
  created_at: string;
}

export interface SyncJobItem {
  id: number;
  job_id: number;
  external_id: string;
  item_title: string;
  action: "created" | "updated" | "skipped" | "failed" | "absent";
  reason: string;
  play_url: string;
  created_at: string;
}

export interface SkipRule {
  id: number;
  platform_id: number | null;
  name: string;
  rule_type: "builtin" | "keyword" | "regex";
  target: "title" | "tags" | "description" | "all";
  pattern: string;
  reason: string;
  enabled: boolean;
  locked: boolean;
  priority: number;
  created_at: string;
  updated_at: string;
}

export interface Page<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

export interface DownloadItem {
  id: number;
  task_id: number;
  episode_no: number;
  title: string;
  page_url: string;
  stream_url: string;
  size_bytes: number;
  duration_seconds: number;
  status: "queued" | "running" | "succeeded" | "failed" | "canceled";
  error_message: string;
  file_url?: string;
}

export interface DownloadProgress {
  task_id: number;
  episode_no: number;
  done: number;
  total: number;
  percent: number;
}

export interface DownloadTask {
  id: number;
  work_id: number;
  platform_id: number;
  status: "queued" | "running" | "succeeded" | "failed" | "canceled";
  total_episodes: number;
  completed_episodes: number;
  failed_episodes: number;
  quality: string;
  output_dir: string;
  error_message: string;
  started_at: string | null;
  finished_at: string | null;
  created_at: string;
  updated_at: string;
  items: DownloadItem[];
  progress?: DownloadProgress;
}
