# 内容投稿接口指南（给文章生成智能体）

本文档面向“写文章的智能体”：你只负责生成 Markdown 并提交到内容中台。**平台和账号不在投稿时指定**，由发布器在发布时决定，发布成功后的回执写回文章；你在文章详情里可以查到实际平台、账号和链接。

- 服务地址：`https://your-domain.example`
- API 前缀：`/api/v1`
- 字符集：UTF-8；时间：RFC3339（UTC，如 `2026-11-13T08:00:00Z`）
- 认证：`Authorization: Bearer <你的 Token>`

> 最近一次接口变化（2026-11-13）：
> 1. 投稿不再接收 `platform`，传了也会被忽略；平台在发布回执后才有值。
> 2. 文章详情新增只读字段 `result_account`、`result_url`、`result_post_id`（发布成功后返回）。
> 3. 列表接口新增 `page` / `page_size` 页码模式，保留原 `cursor` 模式；内容客户端只能看到自己的文章。
> 4. 归档、草稿、已发布改稿的行为按下文表格为准。

---

## 1. 准备 Token

在后台 **API 客户端** 页面创建一个“内容智能体”客户端，类型选 `content`，会自动获得：

- `articles:write` 创建 / 修改文章
- `articles:read` 查询文章
- `media:write` 上传图片、抓取外链图片

Token 形如 `wk_live_xxxxxxxx`，永久有效；后台可随时查看、复制，停用或轮换后旧 Token 立即失效。

---

## 2. 最短提交流程

1. （可选）上传封面 / 正文插图，拿到图片 `uid`
2. `POST /api/v1/articles` 提交文章
3. 记录返回的 `article.uid`；需要时轮询 `GET /api/v1/articles/{uid}` 看 `publish_status`

一次完整提交：

```bash
curl -X POST https://your-domain.example/api/v1/articles \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json; charset=utf-8" \
  -H "Idempotency-Key: article-2026-11-13-001" \
  -d '{
    "external_id": "geo-2026-11-13-001",
    "language": "zh-CN",
    "topic": "外贸获客",
    "tags": ["GEO", "外贸", "内容营销"],
    "title": "外贸企业为什么需要 GEO",
    "summary": "一句话说明文章讲什么，120 字以内更稳妥。",
    "body_md": "# 外贸企业为什么需要 GEO\n\n正文第一段。\n\n![配图](media:01M79GENWP5088AGCA3ASYNK4T)\n\n## 小标题\n\n更多内容。",
    "cover": {"uid": "01M79GENWP5088AGCA3ASYNK4T"},
    "platform_account": "微功夫公众号",
    "publish_mode": "agent"
  }'
```

成功返回 `201`：

```json
{
  "article": {
    "uid": "art_01M79...",
    "external_id": "geo-2026-11-13-001",
    "platform": "",
    "platform_account": "微功夫公众号",
    "status": "ready",
    "publish_status": "queued",
    "job_uid": "job_01M79...",
    "title": "外贸企业为什么需要 GEO",
    "created_at": "2026-11-13T08:00:00Z"
  },
  "created": true
}
```

`publish_status = queued` 表示已进入发布队列。若 `publish_mode=manual` 或 `status=draft`，则不会入队，此时 `publish_status` 为 `none`。

同一 `external_id` 再次提交时必须带上最近查询所得 `revision`；成功返回 `200` 和 `"created": false`，表示更新已有文章。

---

## 3. 字段说明

### 3.1 文章字段（POST /articles）

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `external_id` | string | 否 | 你的文章唯一 ID（≤128 字）。同一客户端下重复提交会**更新**这篇 |
| `platform_account` | string | 否 | 目标账号提示，≤100 字，仅作提示；实际账号以发布回执为准 |
| `language` | string | 否 | BCP-47，如 `zh-CN`、`en`；默认 `zh-CN` |
| `topic` | string | 否 | 内容主题，自由文本 |
| `tags` | string[] | 否 | 标签，最多 20 个，每个 1–32 字 |
| `title` | string | 是 | 标题，1–200 字 |
| `summary` | string | 否 | 摘要，≤500 字 |
| `body_md` | string | 是 | Markdown 正文，不能为空 |
| `cover` | object | 否 | 封面：`{"uid": "..."}` 或 `{"url": "https://..."}` |
| `publish_mode` | string | 否 | `agent`（默认，自动进发布队列）或 `manual`（等人手动发） |
| `status` | string | 否 | `ready`（默认，按模式入队）或 `draft`（存草稿，不进队列） |
| `scheduled_at` | string | 否 | RFC3339 定时发布时间；不传则立即入队 |

> **平台不在投稿时指定。** 服务端会忽略投稿里的 `platform`（旧客户端传 `auto` / `any` / 任意值也一样），文章创建后 `platform` 为空；发布成功后由回执写入实际平台。

### 3.2 封面与插图

- 图片先上传拿 `uid`，再在正文里用 `media:<uid>` 占位：

```markdown
![外贸展会展位](media:01M79GENWP5088AGCA3ASYNK4T)
```

- 正文里所有 `media:<uid>` 会被解析成真实图片 URL；`images` 按出现顺序交给发布器。
- 封面通过 `cover` 单独传，也参与发布数据。
- 支持外链封面 `{"url": "https://..."}`，服务端会抓取、校验并转存（有 SSRF 防护）。

### 3.3 上传图片

原始二进制（推荐）：

```bash
curl -X POST https://your-domain.example/api/v1/media \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: image/png" \
  --data-binary @cover.png
```

或 multipart：

```bash
curl -X POST https://your-domain.example/api/v1/media \
  -H "Authorization: Bearer $TOKEN" \
  -F "file=@cover.png"
```

外链抓取：

```bash
curl -X POST https://your-domain.example/api/v1/media/from-url \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"url":"https://example.com/cover.jpg"}'
```

返回：

```json
{
  "media": {
    "uid": "01M79GENWP5088AGCA3ASYNK4T",
    "url": "https://your-domain.example/media/2026/11/xxxx.jpg",
    "mime": "image/jpeg",
    "width": 1200,
    "height": 800,
    "size": 182733,
    "duplicate": false
  }
}
```

单图上限默认 10MB；支持 JPEG/PNG/WebP/GIF，服务端统一重编码为 JPEG 或 PNG（透明保留 PNG）并去掉 EXIF；同一张图按 SHA-256 去重，重复上传返回已有 `uid` 且 `duplicate=true`。

---

## 4. 状态与时间

### 4.1 编辑状态 `status`

| 值 | 含义 |
|---|---|
| `draft` | 草稿：不进队列，可随时改；之后再改成 `ready` 才会入队 |
| `ready` | 待发布：按 `publish_mode` 入队或等待手动发布 |
| `archived` | 已归档：只读；需先恢复为 `draft` / `ready` 才能再编辑或发布 |

### 4.2 发布状态 `publish_status`

| 值 | 含义 |
|---|---|
| `none` | 未安排发布（草稿、manual 模式或已取消） |
| `scheduled` | 已排期，未到时间 |
| `queued` | 已入队，等发布器领取 |
| `publishing` | 发布器已领取，正在发布 |
| `published` | 已发布成功，可读 `result_account` / `result_url` |
| `failed` | 永久失败（需后台重试或改稿） |
| `cancelled` | 任务被取消（改稿、归档或手动取消） |

### 4.3 平台审核状态 `review_status`

发布成功不等于平台已放行。发布器回执成功时，中台会自动把文章置为「等待平台审核」，由专门的审核智能体定时检查平台结果：

| 值 | 含义 |
|---|---|
| `none` | 还没进入平台审核（未发布或审核未开始） |
| `pending` | 等待平台审核 / 审核中 |
| `approved` | 平台审核通过 |
| `rejected` | 平台审核不通过，`review_note` 里是原因 |

- 文章详情会返回 `review_status`、`review_note`（审核备注）、`review_checked_at`（最近检查时间）。
- 审核由发布器一侧的审核智能体推进，内容智能体不需要调用审核接口；只需要在投稿后查看详情确认最终状态。
- `review_status=rejected` 通常需要人工处理（改稿、换平台或后台重试），不会自动重发。

### 4.4 定时发布

`scheduled_at` 传未来时间：文章立即保存为 `ready`，发布状态为 `scheduled`，到点自动入队。提前改稿会取消旧任务并按新时间重新排期；传 `null` 或空串可清除排期（无活跃任务时立即入队）。

---

## 5. 更新已提交的文章

`revision` 缺失返回 428，版本过期返回 409。不要自动用新版本号覆盖重试，应重新读取并合并修改。`cover:null` 明确清空封面；省略 cover 保留原封面；PATCH 清空排期使用 `clear_schedule:true`。

先读取文章详情并保留 `revision`。以下两种方式都必须携带该版本号：

1. 再次 `POST /api/v1/articles`，带同一个 `external_id`
2. `PATCH /api/v1/articles/{uid}`，传 `revision` 和要改的字段

```bash
curl -X PATCH https://your-domain.example/api/v1/articles/art_01M79... \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"revision":1,"title":"新标题","summary":"新摘要","body_md":"# 新正文"}'
```

规则：

- 投稿和改稿都不需要传 `platform`，传了也会被忽略。
- 只改封面、`platform_account`、`topic` 也会被视为内容变化，会正常保存。
- `ready` 且 `agent` 模式的未发布文章改稿：取消尚未领取的旧任务并重新入队；claimed/uncertain 阶段禁止改稿。
- **已发布文章改稿**：只更新文章内容和预览，不会自动重发，`publish_status` 仍为 `published`，`result_url` 仍指向原发布结果。需要重新发布时请新建一篇（新的 `external_id`）或在后台走手动发布。
- 改成 `draft` / `archived` 会取消尚未领取的任务并清空排期；claimed/uncertain 阶段返回 409，需先完成或核对当前任务；`archived` 文章要先恢复才能改。
- `platform_account` 只是提示，发布回执里的 `result_account` 才是实际账号。

---

## 6. 查询

### 6.1 列表（cursor 模式，默认）

```bash
curl "https://your-domain.example/api/v1/articles?limit=20&cursor=" \
  -H "Authorization: Bearer $TOKEN"
```

### 6.2 列表（页码模式）

传 `page` 即启用页码模式：

```bash
curl "https://your-domain.example/api/v1/articles?page=1&page_size=20" \
  -H "Authorization: Bearer $TOKEN"
```

```json
{
  "items": [ { "uid": "art_01M79...", "title": "...", "publish_status": "queued" } ],
  "total": 42,
  "page": 1,
  "page_size": 20
}
```

支持参数：`q`（标题/摘要关键词）、`platform`（**仅在发布成功后才有值**，未发布为 `待定` 即空）、`status`、`publish_status`、`language`、`from` / `to`（创建时间，RFC3339）、`limit`（1–100，cursor 模式）、`page` / `page_size`（页码模式）。

内容客户端只能看到自己创建的文章；访问别人的文章返回 `404`。

### 6.3 详情

```bash
curl https://your-domain.example/api/v1/articles/art_01M79... \
  -H "Authorization: Bearer $TOKEN"
```

详情额外返回：

- `content_md` / `content_html`：Markdown 原文与服务端渲染后的 HTML
- `cover`：封面对象（uid / url / 尺寸）
- 发布成功后：`result_account`、`result_url`、`result_post_id`
- 平台审核：`review_status`、`review_note`、`review_checked_at`
- 若仍有活跃任务：`job_uid`

```json
{
  "uid": "art_01M79...",
  "platform": "wechat",
  "platform_account": "微功夫公众号",
  "publish_status": "published",
  "review_status": "approved",
  "review_note": "平台审核通过",
  "result_account": "微功夫公众号",
  "result_url": "https://mp.weixin.qq.com/s/xxxx",
  "result_post_id": "123456"
}
```

---

## 7. 幂等与错误

### 7.1 Idempotency-Key（创建必填）

对 `POST /api/v1/articles` 带上 `Idempotency-Key`：24 小时内同 key 同 body 会直接重放原响应，不会重复建文章；同 key 不同 body 返回 `409 idempotency_mismatch`。

### 7.2 限流

默认每客户端 120 次/分钟；触发时返回 `429 rate_limited`，响应头带 `Retry-After: 60`。

### 7.3 错误格式

```json
{
  "error": {
    "code": "validation_failed",
    "message": "参数校验失败",
    "details": {"title": "标题不能为空"}
  },
  "request_id": "01M79..."
}
```

| HTTP | code | 场景 |
|---|---|---|
| 400 | `invalid_json` | JSON 不合法 / 字段类型错误 |
| 401 | `unauthorized` | Token 缺失、失效或已停用 |
| 403 | `forbidden` | Token 没有对应 scope |
| 404 | `not_found` | 文章不存在，或不属于当前客户端 |
| 409 | `archived` | 已归档文章不可编辑，需先恢复 |
| 409 | `idempotency_mismatch` | 同 key 不同请求体 |
| 422 | `validation_failed` | 字段校验失败（详见 `details`） |
| 422 | `media_invalid` | 封面/图片处理失败 |
| 429 | `rate_limited` | 超过限流 |
| 500 | `internal_error` | 服务端异常，可稍后重试 |

排查时带上 `request_id`，服务端日志可按它检索。

---

## 8. 接入检查清单

- [ ] Token 的 scope 包含 `articles:write`、`articles:read`、`media:write`
- [ ] 文章有 `title`、`body_md`；**不传 `platform`**
- [ ] 正文图片先上传，占位符写成 `media:<uid>`
- [ ] 所有创建带 `Idempotency-Key`，更新带当前 `revision`
- [ ] 用 `external_id` 保证同一篇不会重复创建
- [ ] 提交后记录 `article.uid`，需要时轮询 `publish_status`
- [ ] 发布成功后从详情读 `result_account` / `result_url`，不要用 `platform_account`
- [ ] 收到 `422` 时读 `error.details` 定位字段，不要盲目重试

---

## 9. 常见问题

**Q：可以直接把图片 URL 写进 Markdown 吗？**
可以，但外链可能失效，部分平台不接受外链图。建议先上传，用 `media:<uid>`。

**Q：可以传 `platform` 吗？**
会被忽略。旧的 `auto` / `any` / `random` 也按空处理；平台只在发布回执后出现。

**Q：发布失败了怎么办？**
`publish_status=failed` 表示永久失败（账号登录失效、内容被拒等）。可修改文章后重新提交，或让后台在发布队列点“重新入队”。网络、限流、验证码属可重试错误，服务端会自动退避。

**Q：一条内容能发多个平台吗？**
一条文章最终只对应一个平台。多平台请复制成多篇、用不同 `external_id` 分别提交；也可以一篇发布后再新建一篇。

**Q：已发布的文章改了会重发吗？**
不会。改稿只更新内容与预览，`result_url` 指向原发布结果。需要重发请新建文章或走后台手动发布。

**Q：怎么知道发布到哪个账号、什么链接？**
`GET /api/v1/articles/{uid}` 在 `publish_status=published` 后返回 `result_account`、`result_url`、`result_post_id`；后台发布队列也能看到。
