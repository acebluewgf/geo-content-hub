ALTER TABLE publish_jobs
  MODIFY status ENUM('queued','claimed','succeeded','failed_retryable','failed_permanent','cancelled','uncertain') NOT NULL DEFAULT 'queued',
  MODIFY attempts INT UNSIGNED NOT NULL DEFAULT 0,
  MODIFY max_attempts INT UNSIGNED NOT NULL DEFAULT 3,
  ADD COLUMN content_snapshot JSON NULL,
  ADD COLUMN lease_token VARCHAR(64) NOT NULL DEFAULT '',
  ADD COLUMN lease_client_id BIGINT UNSIGNED NULL,
  ADD COLUMN review_lease_token VARCHAR(64) NOT NULL DEFAULT '',
  ADD COLUMN review_client_id BIGINT UNSIGNED NULL;
