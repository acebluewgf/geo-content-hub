# 02 数据模型与 DDL（MySQL 5.7 / 8.0）

> 配套：`architecture.md`
> 目标：v2.0 精简后的 **8 张表**，一套 DDL 在 MySQL 5.7.30+ 与 8.0.x 都能跑；照此建库即可。
> 实际部署：腾讯云 CynosDB **MySQL 8.0.30**（库 `contenthub`，内网 `127.0.0.1:3306`）。
> 已删除：审核、版本、平台档案、发布回执、主题/标签库、媒体库关联表、审计/请求日志。

---

## 1. 设计约定

- 引擎 `InnoDB`，字符集 `utf8mb4`，排序规则一律 `utf8mb4_unicode_ci`，行格式 `ROW_FORMAT=DYNAMIC`。
- 主键统一 `BIGINT UNSIGNED AUTO_INCREMENT`；对外 ID 用 ULID：`users`/`media` 为 26 位，带前缀的 `art_`/`job_`/`cli_` 为 `CHAR(32)`（迁移 008 已放宽）。
- 时间统一 `DATETIME`，应用层存 UTC；不用 `TIMESTAMP`（2038）。
- 金额/布尔不建：状态用 `ENUM` 或 `TINYINT`，应用层校验。
- **不使用外键**，引用完整性由事务与应用层保证（5.7/8.0 行为一致，迁移简单）。
- 标签用 JSON 数组；MySQL 5.7 的 JSON 不做索引，列表筛选不按标签查（需要时 P1 加生成列 + 索引）。
- 表间关系：
  - `articles` 1:N `publish_jobs`（历史任务），同时最多 1 个活跃任务。
  - `media` 被 `articles.cover_media_id` 和正文占位 `media:{uid}` 引用。
  - 发布结果直接存 `publish_jobs.result_*`，**没有 publications 表**。

### 1.1 双版本兼容

基线：MySQL 5.7.30–5.7.x 与 8.0.x（建议 8.0.30+），同一套 migrations。实际部署为腾讯云 CynosDB **8.0.30**，库 `contenthub`。

建库（云库已创建，此处仅作说明）：

```sql
CREATE DATABASE IF NOT EXISTS contenthub
  DEFAULT CHARACTER SET utf8mb4
  DEFAULT COLLATE utf8mb4_unicode_ci;
```

禁用（8.0 独有）：CTE/窗口函数、`SKIP LOCKED`/`NOWAIT`、函数/表达式索引、`DEFAULT (expr)`、`CHECK`、`JSON_TABLE`、`REGEXP_REPLACE`、`INTERSECT/EXCEPT`、`utf8mb4_0900_*` 排序规则。

8.0 必须显式处理：默认排序规则改为 `utf8mb4_0900_ai_ci` → 每张表写明 `COLLATE=utf8mb4_unicode_ci`；整数显示宽度弃用 → 不写宽度（`TINYINT(1)` 除外）；默认认证 `caching_sha2_password` → `go-sql-driver/mysql` 支持，两个版本各自建用户。

连接层（Go）：

```
DSN: user:pass@tcp(host:3306)/contenthub?charset=utf8mb4&collation=utf8mb4_unicode_ci
     &parseTime=true&loc=UTC&timeout=5s&readTimeout=30s&writeTimeout=30s
     &sql_mode=%27STRICT_TRANS_TABLES,NO_ZERO_IN_DATE,NO_ZERO_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION,ONLY_FULL_GROUP_BY%27
     &time_zone=%27%2B00%3A00%27
连接池：MaxOpenConns=20 / MaxIdleConns=10 / ConnMaxLifetime=30m
```

**云库实测默认值（CynosDB 8.0.30，2026-11-13）**：`character_set_server=utf8mb3`、`collation_server=utf8mb3_general_ci` → 所以每张表、每个连接都必须显式 utf8mb4；`lower_case_table_names=1` → 表名全部小写；`transaction_isolation=READ-COMMITTED` → 条件 UPDATE 认领仍安全；`time_zone=+08:00` → 连接后必须 `SET time_zone='+00:00'`。

版本自检 `./contenthub db:check`：版本区间、所有表 `ROW_FORMAT=Dynamic`、`TABLE_COLLATION=utf8mb4_unicode_ci`、无 `utf8mb4_0900_%`、`sql_mode` 含 `ONLY_FULL_GROUP_BY`、会话时区 `+00:00`。

---

## 2. 表清单

| # | 表 | 作用 | 预估行数 |
|---|---|---|---|
| 1 | `users` | 后台用户 | < 50 |
| 2 | `api_clients` | 智能体 Token/scopes | < 100 |
| 3 | `media` | 图片文件 | 10 万/年 |
| 4 | `articles` | 文章 | 3 千/年 |
| 5 | `publish_jobs` | 发布任务 + 结果 | 6 千/年 |
| 6 | `idempotency_keys` | 幂等重放（24h） | 1 万/天滚动 |
| 7 | `settings` | 运行参数 | < 100 |
| 8 | `schema_migrations` | 迁移记录 | < 50 |

---

## 3. DDL

### 3.1 users

```sql
CREATE TABLE users (
  id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  uid             CHAR(26)        NOT NULL,
  username        VARCHAR(64)     NOT NULL,
  password_hash   VARCHAR(255)    NOT NULL COMMENT 'bcrypt $2a$12$...',
  display_name    VARCHAR(64)     NOT NULL DEFAULT '',
  role            ENUM('admin','editor','viewer') NOT NULL DEFAULT 'editor',
  status          TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '1=启用 0=停用',
  login_fail_count TINYINT UNSIGNED NOT NULL DEFAULT 0,
  locked_until    DATETIME        NULL,
  last_login_at   DATETIME        NULL,
  created_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_uid (uid),
  UNIQUE KEY uk_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci ROW_FORMAT=DYNAMIC COMMENT='后台用户';
```

### 3.2 api_clients

```sql
CREATE TABLE api_clients (
  id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  uid             CHAR(32)        NOT NULL,
  name            VARCHAR(100)    NOT NULL,
  type            ENUM('content','publisher','admin') NOT NULL,
  token_prefix    CHAR(8)         NOT NULL,
  token_hash      CHAR(64)        NOT NULL COMMENT 'sha256(token)',
  token_cipher    VARBINARY(512)  NULL COMMENT 'AES-GCM 密文，仅后台查看用',
  scopes          JSON            NOT NULL,
  status          TINYINT UNSIGNED NOT NULL DEFAULT 1,
  last_used_at    DATETIME        NULL,
  created_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_uid (uid),
  UNIQUE KEY uk_token_hash (token_hash),
  KEY idx_type (type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci ROW_FORMAT=DYNAMIC COMMENT='API 客户端';
```

> 没有 `trust_level`：不做审核，所有投稿都直接进入待发布。

### 3.3 media

```sql
CREATE TABLE media (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  uid           CHAR(26)        NOT NULL,
  sha256        CHAR(64)        NOT NULL,
  path          VARCHAR(255)    NOT NULL COMMENT 'storage/uploads/2026/11/<sha256>.jpg',
  mime          VARCHAR(64)     NOT NULL,
  size          INT UNSIGNED    NOT NULL,
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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci ROW_FORMAT=DYNAMIC COMMENT='图片文件';
```

> 不再生成多尺寸变体：编辑器与发布直接用原图（上限 10MB）；需要缩略图时前端 `object-fit` 即可。

### 3.4 articles

```sql
CREATE TABLE articles (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  uid                CHAR(32)        NOT NULL,
  client_id          BIGINT UNSIGNED NULL COMMENT '投稿客户端；后台创建为 NULL',
  external_id        VARCHAR(128)    NULL COMMENT '投稿方幂等键',
  platform           VARCHAR(50)     NOT NULL DEFAULT '' COMMENT '实际发布平台 code，由发布回执写入；发布前为空',
  platform_account   VARCHAR(100)    NULL COMMENT '目标账号提示（纯文本）',
  language           CHAR(8)         NOT NULL DEFAULT 'zh-CN',
  topic              VARCHAR(100)    NULL COMMENT '内容主题（自由文本）',
  tags               JSON            NULL COMMENT '字符串数组',
  title              VARCHAR(200)    NOT NULL,
  summary            VARCHAR(500)    NOT NULL DEFAULT '',
  body_md            MEDIUMTEXT      NOT NULL,
  body_html          MEDIUMTEXT      NOT NULL,
  body_text          MEDIUMTEXT      NOT NULL,
  cover_media_id     BIGINT UNSIGNED NULL,
  content_hash       CHAR(64)        NULL,
  word_count         INT UNSIGNED    NOT NULL DEFAULT 0,
  reading_time       SMALLINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '分钟',
  status             ENUM('draft','ready','archived') NOT NULL DEFAULT 'draft',
  publish_status     ENUM('none','scheduled','queued','publishing','published','failed','cancelled')
                     NOT NULL DEFAULT 'none',
  publish_mode       ENUM('agent','manual') NOT NULL DEFAULT 'agent',
  scheduled_at       DATETIME        NULL,
  first_published_at DATETIME        NULL,
  last_published_at  DATETIME        NULL,
  review_status      ENUM('none','pending','approved','rejected') NOT NULL DEFAULT 'none' COMMENT '平台审核状态',
  review_note        VARCHAR(500)    NOT NULL DEFAULT '' COMMENT '审核备注',
  review_checked_at  DATETIME        NULL,
  created_at         DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at         DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  deleted_at         DATETIME        NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_uid (uid),
  UNIQUE KEY uk_client_external (client_id, external_id),
  KEY idx_status_created (status, created_at),
  KEY idx_publish_status (publish_status),
  KEY idx_review (review_status, review_checked_at),
  KEY idx_platform (platform),
  KEY idx_language (language),
  KEY idx_schedule (status, publish_mode, scheduled_at),
  KEY idx_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci ROW_FORMAT=DYNAMIC COMMENT='文章';
```

说明：

- 没有 `version_no`、没有 `article_versions`；改稿直接覆盖当前行，历史不留。
- `tags` 用 JSON 数组，不建标签表；筛选不按标签。
- `publish_status` 由任务状态推导后物化，列表直接读。
- `scheduled_at` 为 UTC；为空表示尽快。

### 3.5 publish_jobs

```sql
CREATE TABLE publish_jobs (
  id                BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  uid               CHAR(32)        NOT NULL,
  article_id        BIGINT UNSIGNED NOT NULL,
  platform          VARCHAR(50)     NOT NULL DEFAULT '' COMMENT '实际发布平台；领取时为空，成功回执写入',
  status            ENUM('queued','claimed','succeeded','failed_retryable','failed_permanent','cancelled')
                    NOT NULL DEFAULT 'queued',
  active_key        BIGINT UNSIGNED NULL COMMENT '活跃任务=1；终态=NULL，配合唯一键保证一文一活跃',
  scheduled_at      DATETIME        NULL,
  next_attempt_at   DATETIME        NULL,
  attempts          TINYINT UNSIGNED NOT NULL DEFAULT 0,
  max_attempts      TINYINT UNSIGNED NOT NULL DEFAULT 3,
  lease_owner       VARCHAR(100)    NULL COMMENT 'worker_id',
  lease_expires_at  DATETIME        NULL,
  last_error_code   VARCHAR(32)     NULL,
  last_error_message VARCHAR(500)   NULL,
  result_account    VARCHAR(100)    NULL COMMENT '实际发布账号',
  result_url        VARCHAR(500)    NULL,
  result_post_id    VARCHAR(100)    NULL,
  result_at         DATETIME        NULL,
  result_meta       JSON            NULL,
  review_status     ENUM('none','pending','approved','rejected') NOT NULL DEFAULT 'none',
  review_attempts   TINYINT UNSIGNED NOT NULL DEFAULT 0,
  review_note       VARCHAR(500)    NOT NULL DEFAULT '',
  review_url        VARCHAR(1000)   NULL,
  review_checked_at DATETIME        NULL,
  review_next_at    DATETIME        NULL COMMENT '下次审核检查时间',
  review_lease_owner VARCHAR(100)   NULL,
  review_lease_expires_at DATETIME  NULL,
  finished_at       DATETIME        NULL,
  cancel_reason     VARCHAR(200)    NULL,
  created_at        DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at        DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_uid (uid),
  UNIQUE KEY uk_article_active (article_id, active_key),
  KEY idx_queue (status, platform, scheduled_at),
  KEY idx_review (review_status, review_next_at),
  KEY idx_lease (status, lease_expires_at),
  KEY idx_article (article_id, id),
  KEY idx_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci ROW_FORMAT=DYNAMIC COMMENT='发布任务与结果';
```

关键点：

- `active_key`：入队时写 `1`，成功/永久失败/取消时置 `NULL`。MySQL 5.7/8.0 唯一索引允许多个 `NULL`，因此历史任务不冲突；并发 tick 重复插入会 `1062`，捕获忽略。
- 结果字段就是「回执」：不再有 `publications` 表，也没有独立回执 API；`POST /publish/{uid}/result` 直接写这里。
- 没有 `article_version_id`：发布的是文章当前内容。

### 3.6 idempotency_keys

```sql
CREATE TABLE idempotency_keys (
  client_id      BIGINT UNSIGNED NOT NULL,
  idem_key       VARCHAR(128)    NOT NULL,
  endpoint       VARCHAR(120)    NOT NULL,
  request_hash   CHAR(64)        NOT NULL,
  response_code  SMALLINT UNSIGNED NOT NULL,
  response_body  MEDIUMTEXT      NULL,
  created_at     DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
  expires_at     DATETIME        NOT NULL,
  PRIMARY KEY (client_id, idem_key),
  KEY idx_expires (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci ROW_FORMAT=DYNAMIC COMMENT='API 幂等';
```

### 3.7 settings

```sql
CREATE TABLE settings (
  setting_key   VARCHAR(100) NOT NULL,
  setting_value JSON         NOT NULL,
  updated_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (setting_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci ROW_FORMAT=DYNAMIC COMMENT='运行参数';
```

### 3.8 schema_migrations

```sql
CREATE TABLE schema_migrations (
  version    VARCHAR(20) NOT NULL,
  checksum   CHAR(64)    NOT NULL,
  applied_at DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (version)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci ROW_FORMAT=DYNAMIC COMMENT='迁移记录';
```

---

## 4. 种子数据

### 4.1 设置项

```sql
INSERT INTO settings (setting_key, setting_value) VALUES
('publish.tick_limit',        '20'),
('publish.jitter_minutes',    '10'),
('publish.timezone',          '"Asia/Shanghai"'),
('publish.max_attempts',      '3'),
('publish.lease_seconds',     '1800'),
('publish.retry_base_seconds','60'),
('media.max_mb',              '10'),
('media.fetch_max_mb',        '15'),
('api.rate_per_min',          '120'),
('api.articles_per_hour',     '20'),
('api.idempotency_hours',     '24');
```

### 4.2 管理员

不写死任何哈希。部署后执行：

```bash
./contenthub user:create --username=admin --role=admin
# 交互输入密码，bcrypt(cost 12) 写入 password_hash
```

### 4.3 平台颜色

不在数据库里。前端固定映射（见 `04`）：

```
wechat #07c160 · xiaohongshu #ff2442 · zhihu #1677ff · csdn #fc5531
linkedin #0a66c2 · x #111111 · facebook #1877f2 · instagram #e4405f
telegram #229ed9 · wordpress #21759b · douyin #fe2c55 · bilibili #fb7299
其他：按 code 的 hash 从调色板取稳定颜色
```

---

## 5. 常用 SQL

### 5.1 入队（tick，每 60s）

```sql
-- 1) 找可入队文章
SELECT a.id, a.uid, a.platform, a.scheduled_at
FROM articles a
WHERE a.status = 'ready'
  AND a.publish_mode = 'agent'
  AND a.deleted_at IS NULL
  AND a.publish_status IN ('none','scheduled','failed','cancelled')
  AND (a.scheduled_at IS NULL OR a.scheduled_at <= UTC_TIMESTAMP())
  AND NOT EXISTS (
    SELECT 1 FROM publish_jobs j
    WHERE j.article_id = a.id
      AND j.status IN ('queued','claimed','failed_retryable')
  )
ORDER BY a.scheduled_at IS NULL, a.scheduled_at
LIMIT 20;

-- 2) 建任务（应用层生成 ULID；active_key=1）
INSERT INTO publish_jobs
  (uid, article_id, platform, status, active_key, scheduled_at, attempts, max_attempts, created_at, updated_at)
VALUES (?, ?, ?, 'queued', 1, ?, 0, 3, UTC_TIMESTAMP(), UTC_TIMESTAMP());

-- 3) 物化发布状态
UPDATE articles SET publish_status = 'queued' WHERE id = ?;
```

> `1062` 表示别的 tick 已入队，跳过即可。

### 5.2 认领

```sql
START TRANSACTION;

SELECT j.id, j.uid, j.attempts
FROM publish_jobs j
JOIN articles a ON a.id = j.article_id
WHERE j.status = 'queued'
  AND (j.next_attempt_at IS NULL OR j.next_attempt_at <= UTC_TIMESTAMP())
  AND (j.platform = ? OR ? = '' OR j.platform = '')
  AND a.status = 'ready'
ORDER BY j.created_at
LIMIT 1
FOR UPDATE;

-- 条件更新，靠 affected_rows 判断是否抢到
UPDATE publish_jobs
SET status = 'claimed',
    lease_owner = ?,
    lease_expires_at = DATE_ADD(UTC_TIMESTAMP(), INTERVAL ? SECOND),
    attempts = attempts + 1,
    updated_at = UTC_TIMESTAMP()
WHERE id = ? AND status = 'queued';

COMMIT;
-- affected_rows = 0 → 被抢走，重试下一条（最多 3 次）
```

拿到任务后 `UPDATE articles SET publish_status='publishing'`。

### 5.3 心跳

```sql
UPDATE publish_jobs
SET lease_expires_at = DATE_ADD(UTC_TIMESTAMP(), INTERVAL ? SECOND),
    updated_at = UTC_TIMESTAMP()
WHERE id = ? AND status = 'claimed' AND lease_owner = ?;
```

### 5.4 回收过期租约（tick 内）

```sql
UPDATE publish_jobs
SET status = IF(attempts < max_attempts, 'queued', 'failed_permanent'),
    active_key = IF(attempts < max_attempts, 1, NULL),
    lease_owner = NULL,
    lease_expires_at = NULL,
    next_attempt_at = IF(attempts < max_attempts, DATE_ADD(UTC_TIMESTAMP(), INTERVAL 60 SECOND), NULL),
    last_error_code = 'lease_expired',
    last_error_message = '租约过期，自动回收',
    finished_at = IF(attempts < max_attempts, NULL, UTC_TIMESTAMP()),
    updated_at = UTC_TIMESTAMP()
WHERE status = 'claimed' AND lease_expires_at < UTC_TIMESTAMP();
```

之后重算受影响文章的 `publish_status`。

### 5.5 成功结果

```sql
START TRANSACTION;

UPDATE publish_jobs
SET status = 'succeeded',
    active_key = NULL,
    result_account = ?,
    result_url = ?,
    result_post_id = ?,
    result_at = UTC_TIMESTAMP(),
    finished_at = UTC_TIMESTAMP(),
    last_error_code = NULL,
    last_error_message = NULL,
    updated_at = UTC_TIMESTAMP()
WHERE id = ? AND status = 'claimed';

UPDATE articles
SET publish_status = 'published',
    first_published_at = IFNULL(first_published_at, UTC_TIMESTAMP()),
    last_published_at = UTC_TIMESTAMP(),
    updated_at = UTC_TIMESTAMP()
WHERE id = ?;

COMMIT;
```

### 5.6 失败结果

```sql
-- 可重试
UPDATE publish_jobs
SET status = 'failed_retryable',
    attempts = attempts,
    next_attempt_at = DATE_ADD(UTC_TIMESTAMP(), INTERVAL ? SECOND),
    last_error_code = ?,
    last_error_message = ?,
    lease_owner = NULL,
    lease_expires_at = NULL,
    updated_at = UTC_TIMESTAMP()
WHERE id = ? AND status = 'claimed';
UPDATE articles SET publish_status = 'queued' WHERE id = ?;

-- 不可重试 / 超过上限
UPDATE publish_jobs
SET status = 'failed_permanent',
    active_key = NULL,
    last_error_code = ?,
    last_error_message = ?,
    lease_owner = NULL,
    lease_expires_at = NULL,
    finished_at = UTC_TIMESTAMP(),
    updated_at = UTC_TIMESTAMP()
WHERE id = ? AND status = 'claimed';
UPDATE articles SET publish_status = 'failed' WHERE id = ?;
```

### 5.7 发布状态推导

应用层函数，优先级从高到低：

1. 有 `queued` / `failed_retryable` → `queued`
2. 有 `claimed` → `publishing`
3. 最近任务 `succeeded` → `published`
4. 最近任务 `failed_permanent` → `failed`
5. 最近任务 `cancelled` → `cancelled`
6. 无任务且 `scheduled_at > now` → `scheduled`
7. 否则 `none`

---

## 6. 迁移文件规划

```
migrations/
  001_create_users.sql
  002_create_api_clients.sql
  003_create_media.sql
  004_create_articles.sql
  005_create_publish_jobs.sql
  006_create_misc.sql        -- idempotency_keys + settings + schema_migrations
  007_seed_settings.sql
  008_widen_uids.sql         -- uid CHAR(32)
  009_seed_settings_more.sql
  010_add_client_token_cipher.sql
  011_platform_optional.sql  -- articles/publish_jobs.platform 允许空
  012_platform_review.sql   -- 平台审核字段
```

执行器（`./contenthub migrate`）：

1. 先跑 `db:check`，版本/排序规则/`sql_mode` 不符直接退出；
2. 读 `go:embed` 的 SQL，按文件名排序；
3. 已应用的版本校验 checksum，不一致报错（防手改）；
4. 每个文件单事务执行，成功后写 `schema_migrations`；
5. 跑完再执行一次 `db:check`。

---

## 7. 容量估算

按每天 10 篇、每篇 5 张图、图片重编码后 ~150KB：

| 项 | 1 年 | 5 年 |
|---|---|---|
| 文章行（含 HTML/纯文本） | ~150MB | ~750MB |
| 图片 | ~2.7GB | ~13.5GB |
| 任务与幂等 | < 200MB | < 1GB |

- 数据库 5 年 < 5GB，单机 MySQL 足够。
- 图片是主要增长项；超过 50GB 时把 `media.path` 切到对象存储，URL 不变。
- 建议磁盘 ≥ 50GB，保留 30% 余量。

---

## 8. 自检清单

- [ ] 8 张表全部 `ROW_FORMAT=Dynamic`、`utf8mb4_unicode_ci`。
- [ ] 5.7 与 8.0 空库各跑一遍 `migrate`。
- [ ] `db:check` 两端通过。
- [ ] `SHOW CREATE TABLE` 两端一致（除 8.0 的 `DEFAULT` 显示差异）。
- [ ] 代码与 SQL 中无 8.0-only 语法。
- [ ] `publish_jobs` 唯一键 `(article_id, active_key)` 能阻止一文两个活跃任务。
- [ ] `external_id` 在同一 client 下唯一，重复 POST 走更新。
- [ ] 5.7 的 dump 导入 8.0 后 `db:check` 通过。
- [ ] 初始管理员通过 CLI 创建，库内无明文密码。
