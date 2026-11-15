CREATE TABLE IF NOT EXISTS users (
  id               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  uid              CHAR(26)        NOT NULL,
  username         VARCHAR(64)     NOT NULL,
  password_hash    VARCHAR(255)    NOT NULL,
  display_name     VARCHAR(64)     NOT NULL DEFAULT '',
  role             ENUM('admin','editor','viewer') NOT NULL DEFAULT 'editor',
  status           TINYINT UNSIGNED NOT NULL DEFAULT 1,
  login_fail_count TINYINT UNSIGNED NOT NULL DEFAULT 0,
  locked_until     DATETIME        NULL,
  last_login_at    DATETIME        NULL,
  created_at       DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at       DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_uid (uid),
  UNIQUE KEY uk_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci ROW_FORMAT=DYNAMIC;
