# 第三方组件

本项目直接依赖以下开源组件，均与 Apache License 2.0 兼容。分发时请保留各自的许可证与版权声明。

| 组件 | 许可证 | 用途 |
|---|---|---|
| [go-sql-driver/mysql](https://github.com/go-sql-driver/mysql) | MPL-2.0 | MySQL 驱动（文件级 copyleft，作为依赖使用不影响本项目许可） |
| [joho/godotenv](https://github.com/joho/godotenv) | MIT | 读取 .env 文件 |
| [microcosm-cc/bluemonday](https://github.com/microcosm-cc/bluemonday) | BSD-3-Clause | HTML 净化 |
| [yuin/goldmark](https://github.com/yuin/goldmark) | MIT | Markdown 渲染 |
| [oklog/ulid](https://github.com/oklog/ulid) | Apache-2.0 | 有序 ID |
| [disintegration/imaging](https://github.com/disintegration/imaging) | MIT | 图片处理 |
| [golang.org/x/crypto](https://pkg.go.dev/golang.org/x/crypto) | BSD-3-Clause | bcrypt 等 |
| React / Ant Design 及前端依赖 | MIT | 后台界面（见 web/package-lock.json） |

完整依赖树以 `go.mod`、`go.sum`、`web/package-lock.json` 为准。