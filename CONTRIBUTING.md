# 参与贡献

## 提 Issue

- Bug 请附：版本（`contenthub --version` 或 Release 标签）、复现步骤、期望与实际、相关日志。
- 功能建议请先说明使用场景，而不是只给方案。

## 提 Pull Request

1. Fork 后从 `main` 切分支，分支名用 `fix/xxx`、`feat/xxx`。
2. 提交前本地跑：`go vet ./... && go test ./...`，前端改动跑 `cd web && npm run build`。
3. 数据库结构变更必须新增迁移文件（`migrations/NNN_xxx.sql`），不要改已有迁移。
4. 一个 PR 只做一件事；涉及行为变更请同步更新 `docs/` 与 `CHANGELOG.md`。
5. 提交信息用中文或英文均可，但要说清「为什么改」。

## 代码风格

- Go：`gofmt` + `go vet` 必须通过；错误信息小写、不带标点结尾。
- 前端：沿用现有 antd 组件与命名，不引入第二套 UI 库。

## 许可

提交即表示你同意以 Apache License 2.0 授权你的贡献。