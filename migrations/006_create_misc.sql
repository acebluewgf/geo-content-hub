CREATE TABLE IF NOT EXISTS idempotency_keys (
  client_id     BIGINT UNSIGNED NOT NULL,
  idem_key      VARCHAR(128)    NOT NULL,
  endpoint      VARCHAR(120)    NOT NULL,
  request_hash  CHAR(64)        NOT NULL,
  response_code SMALLINT UNSIGNED NOT NULL,
  response_body MEDIUMTEXT      NULL,
  created_at    DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
  expires_at    DATETIME        NOT NULL,
  PRIMARY KEY (client_id, idem_key),
  KEY idx_expires (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci ROW_FORMAT=DYNAMIC;

CREATE TABLE IF NOT EXISTS settings (
  setting_key   VARCHAR(100) NOT NULL,
  setting_value JSON         NOT NULL,
  updated_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (setting_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci ROW_FORMAT=DYNAMIC;
