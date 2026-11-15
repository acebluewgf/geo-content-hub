# 发布器智能体接入指南（租约协议 2）

本指南对应 2026-11-15 生产修复版本，数据库迁移 014–020。API Base 为部署站点的 `/api/v1`。升级步骤见 [生产修复与升级](../deployment.md)。

## 认证与领取

使用 `Authorization: Bearer <发布器 Token>`，需要 `publish:claim`、`publish:result`。平台登录态由发布器管理。

`POST /publish/claim`：

```json
{"lease_protocol":2,"worker_id":"publisher-01","limit":1,"lease_seconds":1800}
```

`platform` 可选，空值代表不筛选；limit 为 1–5。省略 lease_seconds 使用后台设置，实际范围 300–3600 秒。未声明协议 2 返回 426，不能在领到任务前执行平台发布。

返回 `{"lease_protocol":2,"jobs":[...]}`，jobs 中每项直接包含以下字段，**没有旧文档中的嵌套 job/article 对象**：

```json
{
  "uid":"job_...","article_uid":"art_...","title":"标题",
  "status":"claimed","attempts":1,"lease_token":"本轮随机凭证",
  "lease_expires_at":"2026-11-15T06:30:00Z","snapshot_available":true,
  "body_md":"正文","body_html":"<p>正文</p>","body_text":"正文",
  "summary":"摘要","language":"zh-CN","tags":[],"images":[]
}
```

封面存在时为 `cover:{uid,url,mime,width,height,size}`。正文图片以 `media:<uid>` 引用，由 images 的 uid/url 映射解析。内容取自任务快照；领取后不要重新读取文章当前正文代替快照。普通 GET 查询不返回租约凭证，必须保留 claim 响应。

领取响应丢失时不要猜测任务归属，也不要执行平台动作；这轮租约超时后会进入待核对。claim 不建议加幂等键，避免重放过期的领取结果。

## 心跳

`POST /publish/{uid}/heartbeat`：

```json
{"worker_id":"publisher-01","lease_token":"本轮凭证","lease_seconds":1800}
```

必须使用同一 API 客户端、worker_id、lease_token，且租约尚未过期。建议间隔不超过租约时长的三分之一，耗时任务单独续租。409 lease_lost 后停止后续平台动作；如果已产生外部结果，保留证据并交由后台核对。

## 结果回执

`POST /publish/{uid}/result` **必须**携带 `Idempotency-Key`，缺失返回 428。每次确定的回执使用一个新 key；请求或响应失败时，保留同一 key 和完全相同的 JSON 请求体重试。

成功：

```json
{
  "status":"success","worker_id":"publisher-01","lease_token":"本轮凭证",
  "platform":"wechat","platform_account":"实际账号",
  "url":"https://example.com/post/123","post_id":"123"
}
```

platform、platform_account、http(s) url 必填；账号最多 100 字，URL 最多 500 字节，post_id 最多 100 字节。published_at 可选 RFC3339；省略使用数据库时间。成功后发布状态为 succeeded/published，平台审核状态为 pending。

失败：

```json
{
  "status":"failure","worker_id":"publisher-01","lease_token":"本轮凭证",
  "error_code":"rate_limited","error_message":"平台明确拒绝了发布请求",
  "not_published":true
}
```

只有明确确认平台**未发布**时才能设置 not_published=true。该条件下 network_error、rate_limited、captcha_required、unknown 在尝试次数未达上限时进入退避重试，其余情况转 failed_permanent。所有错误码共用配置的最大尝试次数；人工重试保留累计尝试记录，并增加一轮重试额度。

not_published 省略/false 时一律进入 uncertain，自动重试停止。租约超时也进入 uncertain。超时不代表外部平台失败，不能直接再次发布。

收到成功平台结果后，先在执行器本地持久化结果、租约凭证和幂等键，再回传。**回执网络异常不能改写成“发布失败”或再次调用平台发布**；先重试原回执或查询任务。

## 查询与恢复

- `GET /publish/queue`：待发布任务概览。该列表不授予执行权，实际资格以 claim 为准。
- `GET /publish/{uid}`：`{"job":{...}}`，包含快照、当前状态和最近 100 条 attempt_history；历史未保存快照时 snapshot_available=false、正文为空。
- `409 lease_lost`：租约过期、状态不符或凭证/客户端不匹配。
- `409 idempotency_mismatch`：同 key 携带了不同内容或路径。
- `503 commit_unknown`：提交响应不确定，使用原 key/body 重试或先查询。
- `uncertain`：到后台“发布队列 → 登记核对结果”，由用户登记平台证据。确认已发布会登记结果；确认未发布后才能按需重新入队。这里不代替用户操作外部平台。

幂等有效期 24 小时，超过后应先查询业务对象，不把已过期 key 当作永久去重承诺。数据库事务保证本站状态一致；外部平台与本站之间无法通过本地事务实现严格一次发布。

平台审核使用独立审核租约，见 [审核监控指南](reviewer.md)。
