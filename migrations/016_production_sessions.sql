CREATE TABLE IF NOT EXISTS user_sessions (
 token_hash CHAR(64) NOT NULL PRIMARY KEY,
 user_uid VARCHAR(64) NOT NULL,
 credential_hash CHAR(64) NOT NULL,
 expires_at DATETIME NOT NULL,
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 KEY idx_sessions_user (user_uid),
 KEY idx_sessions_expiry (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci ROW_FORMAT=DYNAMIC;
