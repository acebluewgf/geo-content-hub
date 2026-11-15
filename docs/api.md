# 03 API 契约（v1）

> 2026-11-15 更新：本文件保留初版设计背景；当前运行协议、并发约束与错误码以 [发布器协议 2](agents/publisher.md)、[审核协议 2](agents/reviewer.md) 为准。上线与升级顺序见 [部署指南](deployment.md)。旧 worker_id-only 示例不能用于新版本。

> 配套：`architecture.md`、`data-model.md`
> 读者：投稿智能体、发布器智能体、后台实现者。
> 原则：**契约先于实现**；字段名定稿后不再改。
> v2.0 删除了：人工内容审核、版本、平台档案、发布回执表、素材包下载、公开平台列表。
> 2026-11-13 增补：**发布后的平台审核**（`review_status` 与审核队列/回传接口），它指平台侧审核，不是编辑侧人工审核。

---

## 1. 通用约定

### 1.1 基础

| 项 | 值 |
|---|---|
| Base URL | `https://{域名}/api/v1` |
| 协议 | 仅 HTTPS |
| 编码 | UTF-8；`Content-Type: application/json`（上传除外） |
| 时间 | ISO 8601 UTC，`2026-11-13T02:30:00Z`；服务端只存 UTC |
| ID | ULID（26 位），例 `01JC8Z3K4M5N6P7Q8R9S0T1V2W` |
| 请求体上限 | JSON 1MB；正文 ≤500KB；图片 ≤10MB |
| 分页 | 列表支持 cursor（`?limit=20&cursor=...` 返回 `next_cursor`）与页码（`?page=1&page_size=20` 返回 `total/page/page_size`）两种模式 |
| 数据库 | MySQL 5.7.30+ / 8.0.x，行为不随版本变化 |

### 1.2 认证

```
Authorization: Bearer wk_live_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

- Token 永久有效，可在后台「API 客户端」随时查看；服务端以 `sha256` 校验，并以 AES-GCM 密文留存明文副本（仅 admin 可读）。
- scopes：`articles:read`、`articles:write`、`media:write`、`publish:claim`、`publish:result`、`review:queue`、`review:report`、`admin:read`。审核接口接受 `publish:*` 或 `review:*`（兼容发布器 Token）。
- 失败：`401 unauthorized`；scope 不足：`403 forbidden_scope`。

### 1.3 请求头

| 头 | 说明 |
|---|---|
| `Authorization` | 必填 |
| `Idempotency-Key` | 8–128 位；`POST /articles`、`POST /publish/{uid}/result` 必填 |
| `X-Request-Id` | 可选；服务端不回传则自动生成 |

### 1.4 幂等

- `POST /articles` 与 `POST /publish/{uid}/result` 必须带 `Idempotency-Key`，保留 24h。
- 同 key 同 body → 重放原响应（`Idempotent-Replay: true`）。
- 同 key 不同 body → `409 idempotency_mismatch`。

### 1.5 限流

每客户端 120 次/分钟；文章写入 20 篇/小时；上传 60 次/小时。超限 `429`，返回 `Retry-After`。

### 1.6 错误格式

```json
{
  "error": {
    "code": "validation_failed",
    "message": "标题不能为空",
    "details": [{"field": "title", "code": "required"}]
  },
  "request_id": "01JC8Z..."
}
```

| HTTP | code | 含义 |
|---|---|---|
| 400 | `bad_request` | JSON 解析失败/字段类型错 |
| 401 | `unauthorized` | Token 无效/停用 |
| 403 | `forbidden_scope` | scope 不足 |
| 404 | `not_found` | 对象不存在 |
| 409 | `conflict` / `idempotency_mismatch` / `job_cancelled` | 状态冲突 |
| 422 | `validation_failed` | 字段校验失败 |
| 429 | `rate_limited` | 限流 |
| 500 | `internal_error` | 未预期错误 |

### 1.7 平台

- `platform` **不由投稿方指定**：创建/改稿时传了也会被忽略；文章在发布成功前 `platform` 为空。
- 发布器在成功回执里传实际平台 code，服务端规范化（去空格、转小写，`[a-z0-9_-]`，1–50）后写回文章和任务。
- **没有平台档案、没有平台列表接口、没有能力预检**；标题/正文长度用通用上限。
- 颜色由前端映射（见 04），服务端不返回颜色。

---

## 2. 对象

### 2.1 Article

```json
{
  "uid": "art_01JC8Z3K4M5N6P7Q8R9S0T1V2W",
  "external_id": "post-2026-11-13-001",
  "platform": "",
  "platform_account": "weigongfu-main",
  "language": "zh-CN",
  "topic": "外贸获客",
  "tags": ["外贸", "AI", "获客"],
  "title": "外贸企业如何用 AI 做内容",
  "summary": "从选题到分发的完整流程。",
  "content_md": "# 标题\n\n正文...",
  "content_html": "<h1>标题</h1><p>正文...</p>",
  "cover": {
    "uid": "01JC8Z...",
    "url": "https://example.com/media/2026/11/ab12.jpg",
    "width": 1200,
    "height": 800
  },
  "char_count": 1820,
  "reading_time": 8,
  "status": "ready",
  "publish_status": "none",
  "review_status": "none",
  "review_note": "",
  "review_checked_at": null,
  "publish_mode": "agent",
  "scheduled_at": null,
  "first_published_at": null,
  "last_published_at": null,
  "job_uid": "job_01JC8Z...",
  "result_account": "",
  "result_url": "",
  "result_post_id": "",
  "created_at": "2026-11-13T02:00:00Z",
  "updated_at": "2026-11-13T02:00:00Z"
}
```

- `platform` 在发布成功前为空；发布后由回执写入。
- `review_status`：`none` 未进入审核 / `pending` 等待平台审核 / `approved` 审核通过 / `rejected` 审核不通过；由发布成功回执置为 `pending`，审核智能体回传后更新。
- `review_note` 为审核备注（≤500 字），`review_checked_at` 为最近一次审核检查时间。
- `result_account/result_url/result_post_id` 仅在 `publish_status=published` 的详情中返回；列表不返回。
- 列表项省略 `content_md/content_html`，其余相同。

### 2.2 PublishJob

```json
{
  "uid": "01JC8Z...",
  "article_uid": "01JC8Z...",
  "platform": "csdn",
  "status": "claimed",
  "attempts": 1,
  "max_attempts": 3,
  "lease_expires_at": "2026-11-13T03:00:00Z",
  "review_status": "none",
  "review_attempts": 0,
  "review_note": "",
  "review_url": "",
  "review_next_at": null,
  "review_checked_at": null,
  "created_at": "2026-11-13T02:20:00Z",
  "result": null
}
```

`result` 在成功后出现：

```json
{
  "account": "weigongfu-main",
  "url": "https://blog.csdn.net/xxx/article/details/123",
  "post_id": "123",
  "published_at": "2026-11-13T02:35:00Z"
}
```

### 2.3 ClaimPayload（认领返回）

```json
{
  "job": {"uid": "...", "platform": "", "attempts": 1, "lease_expires_at": "..."},
  "article": {
    "uid": "...",
    "title": "...",
    "summary": "...",
    "topic": "外贸获客",
    "tags": ["外贸", "AI"],
    "language": "zh-CN",
    "platform": "",
    "platform_account": "weigongfu-main",
    "body_md": "...",
    "body_html": "<h1>...</h1>",
    "body_text": "...",
    "cover_url": "https://.../ab12.jpg",
    "images": [
      {"uid": "01JC8Z...", "url": "https://.../cd34.jpg", "width": 1200, "height": 800}
    ],
    "scheduled_at": null
  }
}
```

`images` 是 `body_md` 中 `media:{uid}` 解析出的图片，按出现顺序；发布器可直接下载/上传。

---

## 3. 文章接口

### 3.1 新建 `POST /articles`

必填：`title`、`body_md`。可选：`external_id`、`summary`、`topic`、`tags`、`language`、`cover_media_uid`、`platform_account`、`publish_mode`、`scheduled_at`、`status`。`platform` 不再由投稿方指定（传了会忽略）。

```json
{
  "external_id": "post-2026-11-13-001",
  "platform_account": "weigongfu-main",
  "title": "外贸企业如何用 AI 做内容",
  "summary": "从选题到分发的完整流程。",
  "topic": "外贸获客",
  "tags": ["外贸", "AI"],
  "language": "zh-CN",
  "body_md": "# 标题\n\n正文...",
  "cover_media_uid": "01JC8Z...",
  "publish_mode": "agent",
  "scheduled_at": "2026-11-14T01:00:00Z",
  "status": "ready"
}
```

处理：

1. 认证/限流/幂等；
2. 忽略投稿方传入的 `platform`（由发布回执写入）；
3. 校验字段（见 3.5）；
4. 解析 `body_md` 中 `media:{uid}`，确认媒体存在；
5. goldmark 渲染 + bluemonday 净化，生成 `body_html`、`body_text`、字数；
6. 入库：`status` 默认 `ready`，`publish_status` 由 `scheduled_at` 决定（未来→`scheduled`，否则 `none`）；
7. 若同 client 的 `external_id` 已存在 → 走更新（返回 200），内容未变不更新 `updated_at`。

响应 `201`（新建）/`200`（更新）返回完整 Article。

**更新语义（external_id 命中）**：

- 仅更新请求中出现的字段；
- 若当前有活跃任务：取消任务（`cancelled`，原因 `content_updated`），文章回到 `ready`，`publish_status` 重算；
- 已发布文章改稿：`publish_status` 保持 `published`，**不自动重发**；需要重新发布时调 `/schedule`。

### 3.2 修改 `PATCH /articles/{uid}`

请求体同新建（全部可选）。`platform` 由发布回执写入，PATCH 传了也会忽略，不提供换平台接口。

规则：

- 有 `queued/claimed/failed_retryable` 任务 → 取消旧任务，文章 `ready`，`publish_status=none`（下次 tick 重新入队）；
- `published` → 只改内容，不改发布状态，不重发；
- `archived` → 不可改，`409 archived`。

响应 `200` 完整 Article。

### 3.3 查询单篇 `GET /articles/{uid}`

`200` 完整 Article；不存在 `404`。

### 3.4 列表 `GET /articles`

Query：

| 参数 | 说明 |
|---|---|
| `platform` | 精确匹配 |
| `status` | `draft/ready/archived` |
| `publish_status` | 七态 |
| `language` | 如 `zh-CN` |
| `q` | 标题模糊（`LIKE`，P1 全文） |
| `from` / `to` | 创建时间范围（UTC） |
| `limit` | 1–100，默认 20（cursor 模式） |
| `cursor` | 上一页 `next_cursor`（cursor 模式） |
| `page` | 页码（≥1）；传 `page` 即进入页码模式，忽略 `cursor` |
| `page_size` | 1–100，默认 20（页码模式） |

响应（cursor 模式）：

```json
{
  "items": [{"uid": "...", "title": "...", "platform": "csdn", "publish_status": "queued", "...": "..."}],
  "next_cursor": "115"
}
```

响应（页码模式）：

```json
{
  "items": ["..."],
  "total": 24,
  "page": 1,
  "page_size": 20
}
```

### 3.5 字段校验

| 字段 | 规则 |
|---|---|
| `platform` | 投稿/改稿忽略；回执时 1–50，`[a-z0-9_-]` |
| `title` | 1–200 字符 |
| `summary` | ≤500 字符 |
| `body_md` | 1–500KB；必须含非空白 |
| `tags` | 数组，≤20 个，每个 1–32 字符 |
| `topic` | ≤100 字符 |
| `language` | 2–8 字符，BCP-47 粗校验 |
| `platform_account` | ≤100 字符 |
| `scheduled_at` | ISO8601；过去时间按「立即」处理并提示 `warning` |
| `publish_mode` | `agent` / `manual` |

错误用 `422 validation_failed`，`details` 列出字段。

### 3.6 排期 `POST /articles/{uid}/schedule`

```json
{"scheduled_at": "2026-11-14T01:00:00Z", "platform_account": "weigongfu-main"}
```

- `scheduled_at=null` → 取消排期，立即入队（若已有活跃任务则 409）。
- 未来时间 → `publish_status=scheduled`；到点由 tick 入队。
- 过去时间 → 视作立即。
- 已发布文章调用 → 重新入队（重新发布），旧任务保留历史。

响应 `200` Article。

### 3.7 取消 `POST /articles/{uid}/cancel`

```json
{"reason": "内容需要修改"}
```

- 仅 `queued/claimed/failed_retryable` 可取消；`claimed` 取消后发布器回执会得到 `409 job_cancelled`。
- 成功：任务 `cancelled`，文章 `publish_status=cancelled`。

---

## 4. 媒体接口

### 4.1 上传 `POST /media`

`multipart/form-data`：`file`（jpg/png/webp/gif，≤10MB）。

处理：魔数校验 → 重编码（去 EXIF）→ `sha256` 去重 → 落库。

```json
{
  "uid": "01JC8Z...",
  "url": "https://example.com/media/2026/11/ab12.jpg",
  "mime": "image/jpeg",
  "width": 1200,
  "height": 800,
  "size": 153600,
  "duplicate": false
}
```

`duplicate=true` 表示命中已有文件，不再写盘。

### 4.2 外链归档 `POST /media/from-url`

```json
{"url": "https://example.com/a.png"}
```

服务端抓取（SSRF 防护：拒私网/回环/链路本地/云元数据，重定向逐跳校验，超时 10s，≤15MB，Content-Type 必须图片），随后同上传。

响应同上。

---

## 5. 发布接口

### 5.1 查看队列 `GET /publish/queue`

Query：`status`、`platform`、`limit`（1–100）、`cursor`。

返回任务 + 文章摘要：

```json
{
  "items": [
    {
      "job_uid": "01JC8Z...",
      "article_uid": "01JC8Z...",
      "title": "外贸企业如何用 AI 做内容",
      "platform": "csdn",
      "status": "queued",
      "attempts": 0,
      "next_attempt_at": null,
      "result": null
    }
  ],
  "next_cursor": null
}
```

### 5.2 认领 `POST /publish/claim`

```json
{"worker_id": "publisher-01", "platform": "csdn", "limit": 2, "lease_seconds": 1800}
```

| 字段 | 规则 |
|---|---|
| `worker_id` | 必填，≤100 |
| `platform` | 可选；你计划发布的平台。中台会同时下发未分配平台的任务与该平台的重试任务 |
| `limit` | 1–5，默认 1 |
| `lease_seconds` | 300–3600，默认 1800 |

返回：

```json
{"jobs": [ClaimPayload...]}
```

无任务时 `{"jobs": []}`（200，不报错）。

### 5.3 心跳 `POST /publish/{uid}/heartbeat`

```json
{"worker_id": "publisher-01", "lease_seconds": 1800}
```

`200`：`{"lease_expires_at": "..."}`；任务不是自己的或已终态 → `409 lease_lost`。

### 5.4 任务详情 `GET /publish/{uid}`

返回 PublishJob（含 result）。不存在 `404`。

### 5.5 结果 `POST /publish/{uid}/result`

**成功**：

```json
{
  "status": "success",
  "worker_id": "publisher-01",
  "platform": "csdn",
  "platform_account": "weigongfu-main",
  "url": "https://blog.csdn.net/xxx/article/details/123",
  "post_id": "123",
  "published_at": "2026-11-13T02:35:00Z"
}
```

- `worker_id` 必填，必须与 claim 时一致，否则 `409 lease_lost`。
- `platform` 必填，实际平台 code，写回任务与文章。
- `platform_account` 必填、≤100 字符，只校验长度与字符，**不与任何清单比对**。
- `url` 必填、`http/https`、≤500。
- `published_at` 可选，默认服务端当前时间；不能晚于现在 +10 分钟。
- 成功回执会把任务与文章的 `review_status` 置为 `pending`，并按 `settings.review.first_delay_seconds`（默认 300 秒）安排首次平台审核检查；发布器无需再调用其他接口。

**失败**：

```json
{
  "status": "failure",
  "worker_id": "publisher-01",
  "error_code": "rate_limited",
  "message": "平台返回 429",
  "retryable": true
}
```

| error_code | 可重试 | 处理 |
|---|---|---|
| `network_error` | 是 | 退避 60×2^n，封顶 1h |
| `rate_limited` | 是 | 同上 |
| `captcha_required` | 是（限 2 次） | 之后转人工 |
| `unknown` | 是（限 1 次） | 之后转人工 |
| `auth_expired` | 否 | 永久失败，人工更新发布器登录态 |
| `content_rejected` | 否 | 永久失败，人工改稿 |
| `invalid_payload` | 否 | 永久失败，检查契约 |

- `attempts >= max_attempts` 时无论是否可重试都转 `failed_permanent`。
- 任务已取消/已终态 → `409 job_cancelled` / `409 job_finished`。
- 同 `Idempotency-Key` 重放返回首次结果。

响应 `200`：

```json
{
  "job": {"uid": "...", "status": "succeeded"},
  "article": {"uid": "...", "publish_status": "published"}
}
```

### 5.6 审核队列 `GET /publish/review/queue`

scope：`publish:claim`。只读，不占用任务。

```json
{"counts": {"pending": 3, "approved": 10, "rejected": 1},
 "items": [{"uid": "job_...", "article_uid": "art_...", "title": "...",
            "platform": "sohu", "platform_account": "weigongfu",
            "result_url": "https://...", "review_status": "pending",
            "review_attempts": 0, "review_next_at": "2026-11-13T03:20:00Z"}]}
```

- 只返回 `review_status=pending`、`review_next_at<=now`、审核租约空闲或已过期的任务。
- `counts` 是该发布器可见任务按审核状态的数量。

### 5.7 审核认领 `POST /publish/review/claim`

scope：`publish:claim`。

```json
{"worker_id": "review-01", "limit": 5, "lease_seconds": 600}
```

| 字段 | 规则 |
|---|---|
| `worker_id` | 必填，≤100 |
| `limit` | 1–20，默认 5 |
| `lease_seconds` | 120–3600，默认 600 |

返回 `{"jobs": [...]}`。认领会写审核租约并把 `review_attempts` 加 1；同一任务在租约内不会被其他审核终端领走。无到期待检任务时返回空数组。

### 5.8 审核结果 `POST /publish/{uid}/review`

scope：`publish:result`。

```json
{"worker_id": "review-01", "status": "approved",
 "note": "已通过，页面正常", "url": "https://mp.sohu.com/s/..."}
```

| 字段 | 规则 |
|---|---|
| `worker_id` | 必填，必须与审核认领时一致，否则 `409 lease_lost` |
| `status` | `approved` / `rejected` / `pending`（pending 表示仍在审核，稍后再查） |
| `note` | 可选，≤500 字，写回任务与文章 |
| `url` | 可选，`http/https`，审核后的最终链接 |

- `approved/rejected` 是终态：写回任务与文章，之后重复提交返回 `409 review_final`。
- `pending`：保留 `pending`，按 `settings.review.interval_seconds`（默认 21600 秒）重排 `review_next_at`。
- 只有发布成功（`succeeded`）且审核租约属于自己的任务才能回传。
- 成功后文章详情的 `review_status/review_note/review_checked_at` 同步更新。

### 5.9 健康检查 `GET /healthz`

匿名仅返回：

```json
{"status": "ok"}
```

带 `admin:read` 时返回：

```json
{
  "status": "ok",
  "db": true,
  "storage": {"writable": true, "free_gb": 42.1},
  "queue": {"queued": 3, "claimed": 1, "failed": 0},
  "scheduler": {"last_tick_at": "2026-11-13T02:29:12Z", "healthy": true},
  "version": "2.0.0"
}
```

`scheduler.healthy = now - last_tick_at < 5 分钟`。

---

## 6. 两个智能体流程

### 6.1 投稿智能体

```bash
# 1) 上传封面
curl -X POST https://example.com/api/v1/media \
  -H "Authorization: Bearer $TOKEN" \
  -F "file=@cover.jpg"

# 2) 投稿（幂等）
curl -X POST https://example.com/api/v1/articles \
  -H "Authorization: Bearer $TOKEN" \
  -H "Idempotency-Key: post-2026-11-13-001" \
  -H "Content-Type: application/json" \
  -d '{
    "external_id": "post-2026-11-13-001",
    "platform": "csdn",
    "title": "外贸企业如何用 AI 做内容",
    "summary": "从选题到分发的完整流程。",
    "topic": "外贸获客",
    "tags": ["外贸", "AI"],
    "body_md": "# 标题\n\n正文 ![图](media:01JC8Z...)",
    "cover_media_uid": "01JC8Z...",
    "scheduled_at": "2026-11-14T01:00:00Z"
  }'

# 3) 查询状态
curl https://example.com/api/v1/articles/01JC8Z... \
  -H "Authorization: Bearer $TOKEN"
```

### 6.2 发布器智能体

```bash
# 1) 认领
curl -X POST https://example.com/api/v1/publish/claim \
  -H "Authorization: Bearer $PUBLISHER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"worker_id":"publisher-01","platform":"csdn","limit":1,"lease_seconds":1800}'

# 2) 长任务心跳（每 5 分钟）
curl -X POST https://example.com/api/v1/publish/01JC8Z.../heartbeat \
  -H "Authorization: Bearer $PUBLISHER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"worker_id":"publisher-01","lease_seconds":1800}'

# 3) 成功回执
curl -X POST https://example.com/api/v1/publish/01JC8Z.../result \
  -H "Authorization: Bearer $PUBLISHER_TOKEN" \
  -H "Idempotency-Key: result-01JC8Z..." \
  -H "Content-Type: application/json" \
  -d '{"status":"success","platform_account":"weigongfu-main","url":"https://blog.csdn.net/xxx/article/details/123"}'
```

---

## 7. 并发与幂等语义

| 场景 | 结果 |
|---|---|
| 同 `Idempotency-Key` 同 body 并发 | 一个写库，其余重放 |
| 同 `Idempotency-Key` 不同 body | `409 idempotency_mismatch` |
| 两个 worker 同时 claim 同一任务 | 条件 UPDATE 只有一个 `affected_rows=1` |
| 结果到达时租约已过期但未被回收 | 仍接受（`status=claimed` 即可） |
| 结果到达时任务已被回收并重新入队 | `409 job_cancelled` |
| tick 并发重复入队 | 唯一键 `1062`，忽略 |
| 同 `external_id` 重复投稿 | 更新同一篇，不新建 |

---

## 8. 版本与兼容

- 路径 `/v1`；只做向后兼容新增字段。
- 删除字段/改语义必须 `/v2`。
- 投稿方与发布器按本文档实现，联调以 `05` 的测试用例为准。
