ALTER TABLE articles
  ADD COLUMN revision BIGINT UNSIGNED NOT NULL DEFAULT 1,
  MODIFY language VARCHAR(16) NOT NULL DEFAULT 'zh-CN',
  MODIFY publish_status ENUM('none','scheduled','queued','publishing','published','failed','cancelled','uncertain') NOT NULL DEFAULT 'none';
