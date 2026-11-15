ALTER TABLE articles
  ADD COLUMN review_status VARCHAR(16) NOT NULL DEFAULT 'none' AFTER publish_mode,
  ADD COLUMN review_note VARCHAR(500) NOT NULL DEFAULT '' AFTER review_status,
  ADD COLUMN review_checked_at DATETIME NULL AFTER review_note,
  ADD KEY idx_articles_review (review_status, review_checked_at);

ALTER TABLE publish_jobs
  ADD COLUMN review_status VARCHAR(16) NOT NULL DEFAULT 'none' AFTER result_meta,
  ADD COLUMN review_attempts INT UNSIGNED NOT NULL DEFAULT 0 AFTER review_status,
  ADD COLUMN review_note VARCHAR(500) NOT NULL DEFAULT '' AFTER review_attempts,
  ADD COLUMN review_url VARCHAR(1000) NOT NULL DEFAULT '' AFTER review_note,
  ADD COLUMN review_checked_at DATETIME NULL AFTER review_url,
  ADD COLUMN review_next_at DATETIME NULL AFTER review_checked_at,
  ADD COLUMN review_lease_owner VARCHAR(100) NOT NULL DEFAULT '' AFTER review_next_at,
  ADD COLUMN review_lease_expires_at DATETIME NULL AFTER review_lease_owner,
  ADD KEY idx_jobs_review (review_status, review_next_at);
