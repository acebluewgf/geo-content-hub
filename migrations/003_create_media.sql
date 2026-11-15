CREATE TABLE IF NOT EXISTS media (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  uid           CHAR(26)        NOT NULL,
  sha256        CHAR(64)        NOT NULL,
  path          VARCHAR(255)    NOT NULL,
  mime          VARCHAR(64)     NOT NULL,
  size          INT UNSIGNED    NOT NULL DEFAULT 0,
  width         SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  height        SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  source_type   ENUM('upload','url') NOT NULL DEFAULT 'upload',
  status        ENUM('active','deleted') NOT NULL DEFAULT 'active',
  created_by_client_id BIGINT UNSIGNED NULL,
  created_at    DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_uid (uid),
  UNIQUE KEY uk_sha256 (sha256),
  KEY idx_created (created_at),
  KEY idx_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci ROW_FORMAT=DYNAMIC;
