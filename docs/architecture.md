# 社媒内容仓库 + 发布队列 · 总体方案 v2.0

> 2026-11-13 · 技术栈：Go 1.22+ 单二进制 + MySQL 5.7/8.0 · 后台：React 18 + Ant Design 5（白色办公风）
> 实际部署：`203.0.113.10`，云数据库腾讯云 CynosDB **MySQL 8.0.30**（库 `contenthub`）。
> **v2.0 按最新要求大幅收窄**：去掉审核、版本、平台档案、回执、媒体库、主题标签库、审计日志；发布结果直接记在发布队列任务上。
> 配套文档：`data-model.md`、`api.md`、`admin-ui.md`、`state-machine.md`。
> 智能体接入：`agents/contributor.md`、`agents/publisher.md`、`agents/reviewer.md`。

---

## 0. 决策记录

| 项 | 决定 | 影响 |
|---|---|---|
| 技术路线 | Go 单静态二进制 + MySQL | 无 PHP/Composer；交叉编译上传即用 |
| 后台 UI | React 18 + Ant Design 5，白色办公风 | 构建产物 `go:embed`；服务器不需要 Node |
| 公开展示站 | 不做 | 只做后台 + API；`body_html` 仅预览与发布用 |
| 内容与平台 | 一条内容只发一个平台 | 跨平台用「复制为新内容」；`platform` 自由文本 |
| **审核** | **不做人工审核、不做审核队列** | 无 `submitted/approved/rejected`；投稿即进入待发布 |
| **版本** | **不做版本管理** | 无 `article_versions`、无版本号、无版本对比 |
| **平台档案** | **不做** | 无 `platform_profiles`；平台只是字符串 + 前端颜色 |
| **发布回执** | **不单独记录** | 无 `publications` 表；URL/账号/时间写回发布任务 |
| 发布队列 | 保留，列表直接显示发布状态 | 队列行 = 文章 + 任务 + 结果；详情弹窗查看 |
| **媒体库** | **不单独做** | 保留 `media` 存储；只在编辑器/接口上传 |
| **主题与标签** | **不单独做** | `articles.topic` 文本 + `articles.tags` JSON 数组 |
| **审计日志** | **不做** | 无审计表；仅应用文件日志 |
| 平台账号 | 只记录不管理 | 文章上是目标提示，任务结果是实际账号，纯文本 |
| 数据库版本 | MySQL 5.7 与 8.0 都兼容；实际部署云库 **8.0.30** | 一套 DDL/SQL；`db:check` 自检 |

---

## 1. 一页结论

系统 = **文章仓库 + 发布队列 + 发布结果记录**。

```
写作智能体 ──POST /articles──> 文章（ready）
                                  │ 到点/立即
                                  ▼
                          发布队列（queued）
                                  │ 发布器认领（租约）
                                  ▼
                          claimed ──发布──> 成功/失败
                                  │
                    result 写回：状态、账号、URL、时间
                                  ▼
                          文章 publish_status=published
```

- 后台只有一件事要盯：**队列里哪些没发出去、为什么、发到哪了**。
- 平台用颜色区分（微信绿、小红书红、知乎蓝、CSDN 橙、LinkedIn 蓝、X 黑……），未知平台用哈希色。
- 点队列行 → 弹窗看正文摘要、平台、账号、URL、失败原因、尝试次数。
- 不做审核、不做版本、不做平台档案、不做回执表、不做媒体库、不做标签库、不做审计。

---

## 2. 范围

### 做

| 模块 | 内容 |
|---|---|
| 文章 | 新建/编辑/列表/详情、草稿、排期、复制为新内容、归档 |
| 内容字段 | 标题、主题、摘要、Markdown 正文、封面、插图、标签、语言、目标平台、目标账号提示 |
| 媒体 | 编辑器粘贴/拖拽上传、外链抓取归档、sha256 去重、重编码去 EXIF |
| 发布队列 | 入队、认领、心跳、租约回收、重试、取消、结果回写 |
| 平台颜色 | 前端固定映射 + 未知平台哈希 |
| 后台 | 登录、仪表盘、文章列表、编辑器、发布队列、API 客户端、设置 |
| API | 投稿/更新/查询、媒体、队列/认领/心跳/结果 |
| 部署 | 宝塔/Nginx + systemd，内置调度器，不用系统 cron |

### 不做

审核与审核队列、版本与版本对比、平台档案与能力校验、发布回执表与回执页、独立媒体库页、主题/标签库与页面、审计日志、公开展示站、多租户、评论、统计报表（P2 再说）。

---

## 3. 角色

| 角色 | 做什么 | 怎么接 |
|---|---|---|
| 写作智能体 | 把写好的 Markdown 文章推进入库，指定平台/排期 | `POST /api/v1/articles`，Bearer Token |
| 发布器智能体 | 轮询/认领任务，登录各平台发布，回报结果 | claim → 发布 → result |
| 管理员 | 看队列、改文章、处理失败、建客户端 | 后台 |
| 编辑 | 写/改文章、排期、重试 | 后台（无客户端/设置权限） |
| 只读 | 看 | 后台 |

发布器自己持有平台账号与登录态，服务端**不存任何平台凭据**。

---

## 4. 技术栈

| 层 | 选择 |
|---|---|
| 语言 | Go 1.22+（建议 1.23），`CGO_ENABLED=0` 交叉编译单二进制 `contenthub` |
| HTTP | 标准库 `net/http` + ServeMux 路由 + 自研中间件（请求 ID/鉴权/CSRF/限流/日志/panic 恢复） |
| 数据 | `database/sql` + `go-sql-driver/mysql`，手写 SQL，无 ORM |
| 迁移 | 自研 runner（`go:embed` + `schema_migrations` + checksum） |
| Markdown | `yuin/goldmark` + GFM；净化 `microcosm-cc/bluemonday` |
| 图片 | `disintegration/imaging` + 标准库重编码（JPEG/PNG），无 cgo |
| ID | `oklog/ulid/v2` |
| 密码 | `golang.org/x/crypto/bcrypt` cost 12 |
| 日志 | `log/slog` JSON + `lumberjack` 轮转 |
| 后台 | React 18 + TypeScript + Vite + Ant Design 5 + React Router + React Query |
| 队列 | MySQL 表 + 进程内 ticker（60s），`GET_LOCK` 防重入 |
| 部署 | Nginx 反代 `127.0.0.1:8080`；systemd 或宝塔「通用项目」 |

---

## 5. 数据模型（8 张表）

| 表 | 作用 |
|---|---|
| `users` | 后台用户（admin/editor/viewer） |
| `api_clients` | 智能体 Token、scopes |
| `media` | 图片文件（封面/插图），sha256 去重 |
| `articles` | 文章主体 + 状态 + 平台/语言/主题/标签 |
| `publish_jobs` | 发布任务 + 租约 + **发布结果**（账号/URL/时间） |
| `idempotency_keys` | API 幂等重放 |
| `settings` | 运行参数 |
| `schema_migrations` | 迁移记录 |

完整 DDL 见 `data-model.md`。

关键关系：

- `articles` 1 : N `publish_jobs`（历史），但**同时最多 1 个活跃任务**，由 `UNIQUE(article_id, active_key)` 保证。
- `publish_jobs.result_*` 就是发布结果，不再有 `publications` 表。
- 没有 `article_versions`、`platform_profiles`、`tags`、`topics`、`article_media`、`audit_logs`、`request_logs`。

---

## 6. 内容模型

### 6.1 文章字段

| 字段 | 类型 | 说明 |
|---|---|---|
| `uid` | ULID | 对外 ID |
| `external_id` | VARCHAR(128) NULL | 投稿方幂等键，同 client 内唯一 |
| `platform` | VARCHAR(50) | 实际发布平台 code，**由成功回执写入**；发布前为空 |
| `platform_account` | VARCHAR(100) NULL | 目标账号提示（可选，纯文本） |
| `language` | CHAR(8) | BCP-47，如 zh-CN/en |
| `topic` | VARCHAR(100) NULL | 内容主题（自由文本） |
| `tags` | JSON | 标签数组，如 `["外贸","AI"]` |
| `title` | VARCHAR(200) | 标题 |
| `summary` | VARCHAR(500) | 摘要 |
| `body_md` | MEDIUMTEXT | Markdown 唯一真源 |
| `body_html` | MEDIUMTEXT | 服务端渲染 + 净化后的预览/分发 HTML |
| `body_text` | MEDIUMTEXT | 纯文本，保底 |
| `cover_media_id` | BIGINT NULL | 封面 |
| `word_count` / `reading_time` | INT | 自动计算 |
| `status` | ENUM | `draft` / `ready` / `archived` |
| `publish_status` | ENUM | `none/scheduled/queued/publishing/published/failed/cancelled` |
| `publish_mode` | ENUM | `agent` / `manual` |
| `scheduled_at` | DATETIME NULL | 排期（UTC） |
| `content_hash` | CHAR(64) | 正文+标题+摘要哈希，用于同 `external_id` 更新判定 |

### 6.2 状态

- 文章编辑状态只有三个：草稿、待发布、已归档。**没有审核态**。
- 发布状态由任务推导后物化：无任务 `none`；有未来排期 `scheduled`；活跃任务 `queued/publishing`；最近任务成功 `published`、永久失败 `failed`、取消 `cancelled`。
- 任务状态：`queued/claimed/succeeded/failed_retryable/failed_permanent/cancelled`。

### 6.3 标签与主题

- 主题是一个字符串，不做树。
- 标签是 JSON 数组，不做标签表、不做使用计数、不做合并。
- 前端把标签渲染成 antd `Tag`；接口只做长度和数量限制（≤20 个，每个 ≤32 字）。

---

## 7. API 概览

Base：`/api/v1`，Bearer Token，UTF-8/UTC/ULID。完整契约见 `03`。

| 方法 | 路径 | 作用 |
|---|---|---|
| POST | `/articles` | 新建；同 `external_id` 存在则更新 |
| PATCH | `/articles/{uid}` | 修改（无版本号） |
| GET | `/articles/{uid}` | 单篇 |
| GET | `/articles` | 列表（cursor） |
| POST | `/articles/{uid}/schedule` | 排期/取消排期 |
| POST | `/articles/{uid}/cancel` | 取消队列任务 |
| POST | `/media` | 上传图片 |
| POST | `/media/from-url` | 外链图片归档 |
| GET | `/publish/queue` | 查看队列（发布器/后台） |
| POST | `/publish/claim` | 认领任务（租约） |
| POST | `/publish/{uid}/heartbeat` | 续租 |
| GET | `/publish/{uid}` | 任务详情 |
| POST | `/publish/{uid}/result` | 成功/失败结果回写 |
| GET | `/publish/review/queue` | 待平台审核任务（审核智能体） |
| POST | `/publish/review/claim` | 认领审核任务（审核租约） |
| POST | `/publish/{uid}/review` | 回传平台审核结果 |
| GET | `/healthz` | 健康检查 |

投稿文章默认 `status=ready`；`scheduled_at` 为空即尽快入队，有值则到点入队。

---

## 8. 发布队列机制

1. **入队**：调度器每 60s 扫描 `status=ready`、`publish_mode=agent`、`scheduled_at` 已到（或无）且无活跃任务的文章，创建 `publish_jobs`（status=queued，attempts=0）。
2. **认领**：发布器 `POST /publish/claim`，服务端用「短事务 + `SELECT ... FOR UPDATE` + 条件 UPDATE + affected_rows」原子抢占，租约默认 1800s，返回任务与文章 payload。
3. **心跳**：长任务每 5 分钟续租；租约过期由 tick 回收，`attempts<max` 回队列（+60s），否则永久失败。
4. **结果**：
   - 成功：写 `result_account`、`result_url`、`result_post_id`、`result_at`，任务 `succeeded`，文章 `published`；同时任务与文章进入 `review_status=pending`，安排平台审核检查；
   - 可重试失败：`failed_retryable` + 退避（60×2^n，封顶 1h），到点回队列；
   - 永久失败：`failed_permanent`，文章 `failed`。
5. **平台审核**：审核智能体通过 `GET /publish/review/queue` 看到期任务、`POST /publish/review/claim` 领审核租约、`POST /publish/{uid}/review` 回传 `approved/rejected/pending`；终态同步到文章详情与后台列表。
6. **取消**：仅有活跃任务的文章可取消；取消后文章 `cancelled`，可重新入队。
7. **手动发布**：`publish_mode=manual` 的文章不进自动队列；管理员在后台点「标记已发布」，填平台/账号/URL，系统补一条 `succeeded` 任务并更新文章。

平台节奏由发布器自己控制（它持有账号）；服务端不做平台配额、不做发布时段。

---

## 9. 后台（7 个页面）

| 页面 | 关键能力 |
|---|---|
| 登录 | 用户名密码、失败锁定、记住用户名 |
| 仪表盘 | 待发布、队列中、今日已发、失败数、7 天趋势、平台分布、队列健康 |
| 文章列表 | 平台颜色标签、状态、发布状态、筛选、批量归档、复制为新内容 |
| 编辑器 | 左右分栏 Markdown + 服务端预览、粘贴上传、封面、摘要、标签、排期 |
| 发布队列 | 平台颜色 + 发布状态 + 账号/URL；行点击弹窗看详情、重试、取消 |
| API 客户端 | 创建 Token（一次）、scopes、停用/轮换 |
| 设置 | 调度参数、媒体上限、会话、健康检查 |

文章详情用列表行点击的 `Drawer`；发布详情用 `Modal`。页面细节见 `04`。

---

## 10. 媒体

- 只在编辑器与 API 上传；**没有媒体库页面**。
- 入库即下载、`sha256` 去重、重编码去 EXIF、纠正方向；照片输出 JPEG(q=82)，透明输出 PNG。
- 存储 `storage/uploads/yyyy/mm/<sha256>.jpg`，Nginx `/media/` 直出。
- Markdown 中用 `media:{uid}` 占位，渲染时替换为绝对 URL。
- SSRF 防护：拒私网/回环/链路本地/云元数据，重定向逐跳校验，超时 10s，≤15MB，Content-Type 必须是图片。

---

## 11. 安全

- Token 以 `sha256` 校验、AES-GCM 密文留存供后台随时查看；列表显示完整 Token 与复制按钮。
- API 限流：每客户端 120 次/分钟、20 篇文章/小时。
- 幂等：`POST /articles` 与 `POST /publish/{uid}/result` 必须带 `Idempotency-Key`，24h。
- 后台：HttpOnly + Secure + SameSite=Lax Cookie，CSRF Token，登录失败 5 次锁 15 分钟。
- XSS：goldmark 渲染 + bluemonday 白名单；回执 HTML 不再存在，避免二次注入面。
- SQL 注入：全部参数化；上传目录禁止执行；`.env` 不可访问。
- 服务端不保存任何平台密码/Cookie。

---

## 12. 部署

**环境**：Nginx / 静态二进制 / MySQL 5.7.30+ 或 8.0.x / systemd 或宝塔「通用项目」。构建机需 Go 1.22+、Node 20+。

```bash
cd web && npm ci && npm run build && cd ..
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o contenthub ./cmd/contenthub
```

步骤：建库 → 上传二进制与 `.env` → `./contenthub db:check` → `./contenthub migrate` → `./contenthub user:create` → 启动 → Nginx 反代 + `/media/` → HTTPS → `/healthz`。

内置调度器每分钟 tick；不依赖系统 cron。备份用宝塔计划任务每日 `mysqldump` + 上传目录打包。

---

## 13. 实施计划

### P0（6–8 人日）

建库迁移 → Go 内核（路由/中间件/鉴权/限流/错误格式）→ 文章 CRUD + 渲染净化 → 媒体上传/抓取 → 发布队列（tick/claim/heartbeat/result/回收）→ React + antd 后台（登录/仪表盘/列表/编辑器/队列/客户端/设置）→ 部署脚本与文档。

### P1（3–4 人日）

复制为新内容、批量归档、失败重试的快捷操作、健康检查告警、CSV 导出、图片外链抓取重试。

### P2（按需）

撤回、统计、对象存储、全文检索、多发布器调度。

---

## 14. 验收

1. 同一 `Idempotency-Key` 重试 3 次 → 1 篇。
2. 正文含 `<script>` → 预览与 HTML 均不执行。
3. 外链封面入库后变本地文件，断网可显示。
4. 两个 worker 并发认领同一任务 → 只有一个成功。
5. 租约过期 → 自动回队，`attempts` 递增；超限永久失败。
6. 成功结果回写后，队列行显示账号/URL/时间，文章变为已发布。
7. 队列里不同平台显示不同颜色，未知平台有稳定哈希色。
8. 已发布文章改稿 → 需要重新入队才发布（不自动重发）。
9. 投稿不指定平台；发布回执写回平台后，列表才显示平台标签。
10. 后台无审核、无版本、无平台档案、无回执、无媒体库、无标签库、无审计入口。

---

## 15. 仍待你拍板

1. 平台颜色用固定映射（微信绿/小红书红/知乎蓝…），未知平台哈希色，可以吗？
2. 手动发布（`publish_mode=manual`）保留在文章操作里，不做独立页面，可以吗？
3. 文章需要「草稿」吗，还是所有投稿直接 `ready` 入队？
