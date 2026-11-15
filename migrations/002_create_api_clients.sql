CREATE TABLE IF NOT EXISTS api_clients (
  id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  uid          CHAR(26)        NOT NULL,
  name         VARCHAR(100)    NOT NULL,
  type         ENUM('content','publisher','admin') NOT NULL,
  token_prefix CHAR(8)         NOT NULL,
  token_hash   CHAR(64)        NOT NULL,
  scopes       JSON            NOT NULL,
  status       TINYINT UNSIGNED NOT NULL DEFAULT 1,
  last_used_at DATETIME        NULL,
  created_at   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_uid (uid),
  UNIQUE KEY uk_token_hash (token_hash),
  KEY idx_type (type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci ROW_FORMAT=DYNAMIC;
