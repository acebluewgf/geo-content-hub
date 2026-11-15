# 安全政策

## 报告安全问题

请不要在公开 Issue 中披露安全问题。请发送邮件至 shaofeng@weigongfu.com（主题注明 `[SECURITY]`），
我们会在 3 个工作日内确认收到；修复发布后会在 CHANGELOG 中致谢（如你愿意署名）。

## 支持范围

| 版本 | 安全更新 |
|---|---|
| 最新 Release | ✅ |
| 更早版本 | 建议升级到最新版 |

## 部署侧提醒

- `SESSION_SECRET` 至少 32 位随机值，轮换后所有会话失效（这是预期行为）。
- 生产环境请置于 HTTPS 反向代理之后，并确保 `APP_BASE_URL` 使用 https。
- 数据库账号只授予本库所需权限，不要使用 root。
- `.env.local` 不要提交、不要打进 Docker 镜像。