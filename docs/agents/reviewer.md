# 平台审批监控智能体接入指南（租约协议 2）

对应 2026-11-15 修复版本。API Base 为 `/api/v1`；使用 reviewer 客户端的 `review:queue`、`review:report` 权限，现有 publisher 权限仍兼容审核接口。审核只读取平台结果、回传台账，不执行再次发布。

## 待检与认领

`GET /publish/review/queue` 返回 counts/items。只有已成功发布、review_status=pending、已到复查时间且没有有效审核租约的任务可被领取。

`POST /publish/review/claim`：

```json
{"lease_protocol":2,"worker_id":"reviewer-01","limit":5,"lease_seconds":600}
```

limit 为 1–20；lease_seconds 小于 120 时使用 600，上限 3600。platform 可选。未声明协议 2 返回 426。

返回 jobs 数组，每项直接包含 uid、result_url、review_status、review_attempts、review_lease_expires_at 和 **lease_token**。保存本轮凭证，并用同一个 API 客户端和 worker_id 续租、回传。发布租约与审核租约不能混用。

## 批量心跳

`POST /publish/review/heartbeat`：

```json
{
  "worker_id":"reviewer-01","uids":["job_a","job_b"],"lease_seconds":600,
  "lease_tokens":{"job_a":"a 的审核凭证","job_b":"b 的审核凭证"}
}
```

任一任务租约无效返回 409，整批不提交。只续租仍有效的租约，过期后需重新认领。

## 回传审核

`POST /publish/{uid}/review`：

```json
{
  "worker_id":"reviewer-01","lease_token":"本轮审核凭证",
  "status":"approved","note":"平台页面已公开可见",
  "url":"https://example.com/post/123","http_status":200
}
```

status 为 pending / approved / rejected。note 最多 500 字，URL 最多 1000 字节且为 http(s)。可携带 Idempotency-Key，使响应丢失时同 key/body 的重试得到原响应；不带幂等键时，已释放的审核租约再次回传返回 409 lease_lost。

pending 释放租约并按 review.interval_seconds 安排复查；approved/rejected 结束本轮审核。旧发布任务的审核回执不会覆盖该文章更新一次发布的审核状态。

后台设置的 review.first_delay_seconds 控制首次等待，范围 30–86400 秒，默认 300；review.interval_seconds 控制复查间隔，范围 300–604800 秒，默认 21600。所有保存项同时校验并原子写入。

## 执行器要求

1. 领取并保存 uid、worker_id、lease_token、截止时间，再访问平台。
2. 在租约有效期内续租；409 后停止当前轮次的回传。
3. 保留观察依据，无法确认时回传 pending，避免把暂时性访问失败判为平台拒绝。
4. 回执请求内容确定后使用固定幂等键重试；不要因为响应丢失直接生成相反结论。
5. 不依赖旧任务详情重建不存在的历史正文；snapshot_available=false 表示没有可证明的内容快照。

升级步骤见 [生产修复与升级](../deployment.md)。
