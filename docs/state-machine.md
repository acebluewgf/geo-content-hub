# 05 流程状态机与测试

> 2026-11-15 更新：本文件保留初版设计背景；当前运行协议、并发约束与错误码以 [发布器协议 2](agents/publisher.md)、[审核协议 2](agents/reviewer.md) 为准。上线与升级顺序见 [部署指南](deployment.md)。旧 worker_id-only 示例不能用于新版本。

> 配套：`architecture.md`、`data-model.md`、`api.md`、`admin-ui.md`
> 范围：v2.0（8 张表、7 个页面、18 个端点，无人工审核/版本/回执表；2026-11-13 增补发布后的平台审核接口与状态）。
> 目标：把状态、时序、边界和验收讲清楚，实现时不用再猜。

---

## 1. 三张状态机

### 1.1 文章编辑状态 `articles.status`

```
                 ┌─────────────┐
     新建 ───────▶│   draft     │──── 保存并排队 ───┐
                 │   (草稿)     │                   │
                 └──────┬──────┘                   ▼
                        │ 保存为待发布          ┌─────────┐
                        └─────────────────────▶│  ready  │
                                               │ (待发布) │
                                               └────┬────┘
                                                    │ 归档
                                                    ▼
                                               ┌──────────┐
                                               │ archived │
                                               └──────────┘
```

| 当前 | 事件 | 目标 | 备注 |
|---|---|---|---|
| — | 新建 `status=draft` | draft | 不进队列 |
| — | 新建默认 | ready | 立即参与排队 |
| draft | 保存并排队 | ready | 有 `scheduled_at` 则排期 |
| ready | 归档 | archived | 有活跃任务先取消 |
| archived | 恢复 | draft | 不自动入队 |
| 任意 | 软删 | deleted_at | 列表不可见 |

### 1.2 发布状态 `articles.publish_status`

不手工维护，由任务推导后物化：

```go
func derivePublishStatus(a *Article, latest, active *PublishJob) PublishStatus {
    if active != nil {
        switch active.Status {
        case JobQueued, JobFailedRetryable:
            return PubQueued
        case JobClaimed:
            return PubPublishing
        }
    }
    if latest != nil && latest.Status == JobSucceeded {
        return PubPublished
    }
    if latest != nil && latest.Status == JobCancelled && latest.CancelReason == "article_deleted" {
        return PubCancelled
    }
    if latest != nil && (latest.Status == JobFailedPermanent || latest.Status == JobFailedRetryable) {
        return PubFailed
    }
    if latest != nil && latest.Status == JobCancelled {
        return PubCancelled
    }
    if a.ScheduledAt != nil && a.ScheduledAt.After(time.Now().UTC()) {
        return PubScheduled
    }
    return PubNone
}
```

| 值 | 含义 | 前台展示 |
|---|---|---|
| none | 未发布 | 未发布 |
| scheduled | 已排期 | 已排期 |
| queued | 排队中 | 排队中 |
| publishing | 发布中 | 发布中 |
| published | 已发布 | 已发布 |
| failed | 失败 | 失败 |
| cancelled | 已取消 | 已取消 |

### 1.3 任务状态 `publish_jobs.status`

```
              ┌──────────┐   claim    ┌──────────┐  success  ┌───────────┐
   tick 入队 ─▶│  queued  │───────────▶│ claimed  │──────────▶│ succeeded │
              └────┬─────┘            └────┬─────┘           └───────────┘
                   │                       │ failure
                   │ cancel                ├──────────────▶ failed_retryable
                   ▼                       │                 （可重试）
              ┌───────────┐                │
              │ cancelled │                └──────────────▶ failed_permanent
              └───────────┘                                 （不可重试/次数用尽）
     failed_retryable ── 到 next_attempt_at ──▶ queued
     claimed ── 租约过期且 attempts>=max ──▶ failed_permanent
     claimed ── 租约过期且 attempts<max ──▶ queued
```

| 当前 | 事件 | 目标 | 动作 |
|---|---|---|---|
| — | tick 选中 | queued | 写 `active_key=1`，`UNIQUE(article_id, active_key)` 防重复 |
| queued | claim | claimed | 条件 UPDATE，`attempts+1`，写 lease |
| queued | cancel | cancelled | 无租约，直接取消 |
| claimed | success | succeeded | 写 result_*，清空 active_key，更新文章 published |
| claimed | failure（可重试） | failed_retryable | 记录错误，按退避写 `next_attempt_at` |
| claimed | failure（不可重试） | failed_permanent | 清空 active_key，文章 failed |
| claimed | 租约过期 | queued / failed_permanent | 见上 |
| failed_retryable | 到点 | queued | tick 重置 lease/active_key 不变 |
| queued | 归档文章 | cancelled | 原因 `article_archived` |
| queued/claimed/failed_retryable | 文章删除 | cancelled | 原因 `article_deleted` |
| queued | 改稿 | cancelled | 原因 `content_updated`，文章 ready |

同一篇永远最多一个活跃任务：`active_key=1` 时唯一键生效；终态置 NULL（5.7 允许多 NULL）。

---

## 2. 关键时序

### 2.1 投稿 → 发布 → 回执

```
智能体            API                      MySQL                发布器
  │  POST /articles │                         │                    │
  │────────────────▶│  校验/渲染/入库 ready    │                    │
  │◀────────────────│  201 Article            │                    │
  │                 │                         │                    │
  │                 │  每 60s tick            │                    │
  │                 │────────────────────────▶│  INSERT job queued │
  │                 │                         │  publish_status=queued
  │                 │                         │                    │
  │                 │  POST /publish/claim ◀──│────────────────────│
  │                 │  条件 UPDATE claimed    │                    │
  │                 │────────────────────────▶│                    │
  │                 │  jobs[] payload ───────▶│                    │
  │                 │                         │   发布到平台        │
  │                 │  POST /publish/{uid}/result ◀─────────────────│
  │                 │────────────────────────▶│ job succeeded      │
  │                 │                         │ article published  │
  │                 │  200 ──────────────────▶│                    │
```

### 2.2 排期

- `POST /articles/{uid}/schedule`，`scheduled_at > now` → `publish_status=scheduled`。
- tick 只选 `scheduled_at <= now` 的 ready 文章。
- 到点后与立即发布相同。
- 改期：再次调用 schedule，取消旧排期（无任务则只改字段）。

### 2.3 可重试失败

```
job claimed ──result failure network_error──▶ failed_retryable
   next_attempt_at = now + 60×2^(attempts-1)，上限 1h
   attempts < max  → tick 到点转 queued（重新入队，active_key=1）
   attempts >= max → failed_permanent
```

### 2.4 租约回收

```
每分钟 tick：
  SELECT id FROM publish_jobs
   WHERE status='claimed' AND lease_expires_at < NOW()
   LIMIT 50;

  若 attempts < max_attempts → UPDATE queued，清 lease，next_attempt_at=now+60s
  否则                       → UPDATE failed_permanent，清 active_key
```

### 2.5 并发认领

两个发布器同时 `POST /publish/claim`：

```sql
UPDATE publish_jobs
   SET status='claimed', lease_owner=?, lease_expires_at=?,
       attempts=attempts+1, claimed_at=NOW()
 WHERE uid=? AND status='queued';
-- 只有一个 affected_rows=1
```

另一个拿到 0 行，继续下一条；全部失败返回 `{"jobs":[]}`。

### 2.6 已发布改稿（不重发）

```
PATCH /articles/{uid}（publish_status=published）
  → 更新内容
  → publish_status 保持 published
  → 不创建任务
需要再发：POST /articles/{uid}/schedule {"scheduled_at": null}
  → 取消旧任务（若有）
  → 新任务入队 → 重新发布
```

### 2.7 队列中改稿

```
PATCH /articles/{uid}（publish_status=queued 或 publishing）
  → 取消当前 job（cancel_reason=content_updated）
  → 文章 ready，publish_status=none
  → 下一次 tick 用新内容重新入队
  → 已 claim 的发布器回执得到 409 job_cancelled
```

### 2.8 取消

```
POST /articles/{uid}/cancel
  queued → cancelled，文章 cancelled
  claimed → cancelled；发布器心跳/回执得到 409
  不允许取消 succeeded / failed_permanent
```

### 2.9 手动发布（publish_mode=manual）

- tick 跳过 manual 文章，`publish_status=none`。
- 后台在发布队列页点「标记已发布」，填账号与 URL（`POST /admin/api/articles/{uid}/mark-published`，后台内部接口）。
- 记录方式与 agent 回执相同：写 `publish_jobs` 的 result_*（`source=manual`）。

### 2.10 幂等重放

```
POST /articles 同 Idempotency-Key 同 body
  → 命中 idempotency_keys
  → 返回首次响应，Idempotent-Replay: true
同 key 不同 body → 409 idempotency_mismatch
```

---

## 3. 边界决策

| # | 场景 | 决定 |
|---|---|---|
| 1 | 已发布文章换平台 | 不允许；平台由成功回执写入，PATCH 传 platform 会被忽略 |
| 2 | 回执账号与实际不一致 | 以回执为准，只记文本，不校验 |
| 3 | 排期时间已过 | 视作立即，返回 `warning` |
| 4 | 标题为空 | 422；后台草稿也要求标题非空 |
| 5 | 正文中图片抓取失败 | 仅 warning，不阻断入库 |
| 6 | 第 max 次失败后可重试 | 直接 `failed_permanent` |
| 7 | 取消后发布器回执 success | 409 `job_cancelled`，不写结果 |
| 8 | 同一文章重复入队 | 唯一键 1062，忽略 |
| 9 | 同 external_id 内容未变 | 不更新 `updated_at`，返回 200 |
| 10 | 多语言同文 | 允许，各自独立文章 |
| 11 | 调度器重启 | 租约到期自动回收，无需人工 |
| 12 | tick 并发 | `GET_LOCK` 拿不到直接跳过 |
| 13 | 平台 code 大小写 | 统一小写；`CSDN` → `csdn` |
| 14 | 平台颜色 | 前端映射；未知平台哈希取色 |
| 15 | 媒体删除 | 软删；被引用时不允许删 |
| 16 | 客户端停用 | 立即 401 |
| 17 | 手动标记已发布 | 必须选平台；URL 只校验 `http/https` |
| 18 | 文章归档后恢复 | 回 `draft`，不自动入队 |
| 19 | 队列中文章被归档 | 先取消任务再归档 |
| 20 | 数据库切换 5.7/8.0 | 行为一致；启动 `db:check` 校验 |

---

## 4. 测试

### 4.1 单元测试（Go `testing`，`go test ./internal/... -race`）

| # | 用例 |
|---|---|
| U1 | `derivePublishStatus` 全状态矩阵（7×任务组合） |
| U2 | 字段校验：平台/标题/摘要/标签/语言/URL |
| U3 | 平台规范化：大小写/空格/非法字符 |
| U4 | `media:{uid}` 解析与替换（正文→图片列表） |
| U5 | goldmark 渲染 + bluemonday 净化（脚本/事件属性被移除） |
| U6 | 退避计算 60×2^n 封顶 1h |
| U7 | 租约回收判定（attempts<max / >=max） |
| U8 | 媒体魔数识别与 MIME 判定 |
| U9 | sha256 去重命中 |
| U10 | ULID 生成唯一、时间有序 |
| U11 | 幂等 key 冲突判定 |
| U12 | 平台颜色哈希稳定性（前端工具函数） |

### 4.2 集成测试（`httptest` + `TEST_MYSQL_DSN` 一次性库）

| # | 用例 |
|---|---|
| I1 | 创建文章 201，字段回读一致 |
| I2 | 同 external_id 再次提交 → 200 更新，不新增 |
| I3 | Idempotency-Key 重放 → 同响应 + `Idempotent-Replay` |
| I4 | 同 key 不同 body → 409 |
| I5 | PATCH 改稿 → 任务取消、文章 ready |
| I6 | 排期未来 → scheduled；到点 tick 入队 |
| I7 | 排期过去 → 立即入队 |
| I8 | 取消 queued 任务 → cancelled |
| I9 | claim → 200 返回 payload；再 claim 为空 |
| I10 | heartbeat 续租；非 owner / 缺 worker_id → 409 |
| I11 | success 回执 → job succeeded + 文章 published + platform 写回 |
| I12 | 回执缺 platform / platform_account → 422 |
| I13 | failure 可重试 → failed_retryable + next_attempt_at |
| I14 | failure 不可重试 → failed_permanent |
| I15 | 租约过期回收 → queued（attempts<max） |
| I16 | 租约过期回收 → failed_permanent（attempts>=max） |
| I17 | 结果重复提交 → 409 job_finished |
| I18 | 已取消任务回执 → 409 job_cancelled |
| I19 | 归档 → 任务取消 + 文章 archived |
| I20 | 删除 → 软删 + 队列消失 |
| I21 | 媒体上传 → 重编码、去 EXIF、sha256 |
| I22 | 外链归档 → SSRF 拦截（127.0.0.1/169.254/内网） |
| I23 | 非图片 Content-Type → 422 |
| I24 | 列表筛选/分页 cursor 正确 |
| I25 | scope 不足 → 403；无 token → 401 |
| I26 | 限流 429 + `Retry-After` |
| I27 | healthz 匿名仅 status；admin 带详情 |
| I28 | 手动标记已发布 → publish_status=published |

### 4.3 并发测试

| # | 用例 |
|---|---|
| C1 | 10 个 goroutine 同时 claim 同一任务，只有 1 个成功 |
| C2 | 两个 tick 同时运行，只入队 1 条 |
| C3 | 同一结果并发提交，只有 1 次写库 |
| C4 | 改稿与 claim 并发：要么旧任务取消，要么新任务未建，无中间态 |
| C5 | 租约回收与回执并发：结果按 `status=claimed` 判定 |

### 4.4 安全测试

| # | 用例 |
|---|---|
| S1 | 正文 `<script>`/`onerror` 被净化 |
| S2 | 标题/摘要 XSS 转义 |
| S3 | 外链抓取 SSRF（私网/回环/链路本地/重定向） |
| S4 | 上传伪装 `.php`/`.sh`/双扩展名被拒 |
| S5 | 图片炸弹（超大像素）拒绝 |
| S6 | SQL 注入：全部参数化，`' OR 1=1` 无效 |
| S7 | 越权：普通 client 不能 claim；publisher 不能改文章 |
| S8 | Token 停用立即失效 |
| S9 | 登录 CSRF：后台写接口校验 `X-CSRF-Token` |
| S10 | 目录遍历：媒体路径只按 uid 查库 |

### 4.5 性能

| # | 指标 |
|---|---|
| P1 | 文章列表 1000 篇，P95 < 300ms |
| P2 | claim 单次 < 50ms |
| P3 | 单篇渲染（50KB）< 100ms |
| P4 | 图片上传 5MB < 2s |
| P5 | tick 20 篇 < 1s |

---

## 5. 验收映射（10 条）

| # | 验收 | 对应测试 |
|---|---|---|
| 1 | 智能体能投稿并在后台看到 | I1、I3 |
| 2 | 同 external_id 不重复 | I2 |
| 3 | 队列按平台/时间出队 | I6、I7、I9 |
| 4 | 发布器只认领一次 | C1、C2 |
| 5 | 回执记录账号/URL/时间 | I11、I12 |
| 6 | 失败可重试、永久失败可区分 | I13、I14 |
| 7 | 租约过期自动回收 | I15、I16 |
| 8 | 已发布改稿不自动重发 | I5 变体 |
| 9 | 后台能看平台颜色与详情 | 04 验收 1–5 |
| 10 | 5.7/8.0 行为一致 | 双版本矩阵 |

---

## 6. 故障演练

| # | 故障 | 预期 |
|---|---|---|
| F1 | 停调度器 10 分钟 | 仪表盘红；恢复后补入队 |
| F2 | 发布器崩溃（租约未释放） | 到期回收，重新入队 |
| F3 | 发布器重复回执 | 幂等，只记一次 |
| F4 | 平台登录失效 | `auth_expired` → 永久失败，人工处理 |
| F5 | 数据库只读 | 写入 500，日志可定位 |
| F6 | 磁盘满 | 上传 507/500，媒体不丢已有 |
| F7 | 旧 Token | 401，提示轮换 |
| F8 | 限流触发 | 429 + Retry-After，客户端退避 |

---

## 7. 上线检查清单

### 环境

- [ ] Go 1.22+ 本机；`CGO_ENABLED=0 go build` 通过
- [ ] MySQL 5.7.30+ 或 8.0.x；`db:check` 通过
- [ ] Nginx 反代 `127.0.0.1:8080` + `/media/`
- [ ] 上传目录独立、不可执行
- [ ] systemd/宝塔守护二进制

### 数据

- [ ] 7 个迁移执行成功
- [ ] 初始管理员创建（bcrypt）
- [ ] 建 API 客户端（content + publisher）
- [ ] 设置项齐全（时区/限流/租约）
- [ ] 两版本各跑一次完整测试

### 运行

- [ ] tick 每 60s，`last_tick_at` 正常
- [ ] 队列健康卡片绿
- [ ] 试发一篇到测试平台
- [ ] 回执 URL 可打开

### 安全

- [ ] HTTPS 强制
- [ ] `APP_ENV=production`；panic 恢复
- [ ] Token 以 SHA-256 哈希校验、AES-GCM 密文留存供后台查看
- [ ] 上传/抓取限制生效
- [ ] 备份脚本可恢复

### 验收

- [ ] 10 条验收全部通过
- [ ] 两个智能体按 03 文档联调成功

---

## 8. 遗留决策

1. **平台颜色**：固定 12 个 + 哈希调色板，是否够用？
2. **手动发布**：是否保留「标记已发布」后台入口？
3. **草稿**：投稿方是否需要 `status=draft` 先存不排队？
4. **全文搜索**：P1 再加 MySQL ngram，还是先 `LIKE`？
5. **对象存储**：图片先本地；超过 50GB 再切 S3/OSS。
6. **撤回已发布**：不做（平台侧删除由人工）。
