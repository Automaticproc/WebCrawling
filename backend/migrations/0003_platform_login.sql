-- Per-platform login settings for the optional download feature.
-- login_password stores an AES-GCM token (see internal/secretbox), never plaintext.
ALTER TABLE platforms
  ADD COLUMN login_url VARCHAR(1024) NULL,
  ADD COLUMN login_username VARCHAR(255) NULL,
  ADD COLUMN login_password TEXT NULL,
  ADD COLUMN login_user_selector VARCHAR(255) NULL,
  ADD COLUMN login_pass_selector VARCHAR(255) NULL,
  ADD COLUMN login_submit_selector VARCHAR(255) NULL,
  ADD COLUMN browser_profile VARCHAR(1024) NULL,
  ADD COLUMN cookie TEXT NULL;
