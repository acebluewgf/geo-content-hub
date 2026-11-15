# 04 后台原型与交互（Ant Design 5）

> 配套：`architecture.md`、`api.md`
> 范围：**7 个页面** + 2 个弹层（文章详情 Drawer、发布详情 Modal）。
> 已删除：审核队列、版本对比、平台档案、回执页、媒体库、主题/标签页、审计日志。
> 风格：白色办公风——浅灰画布、纯白卡片、蓝色主强调、细边框、弱阴影。

---

## 1. 技术形态

| 项 | 选择 |
|---|---|
| 框架 | React 18 + TypeScript + Vite 5 |
| 组件库 | Ant Design 5 + `@ant-design/icons` |
| 路由 | React Router 6 |
| 数据 | `@tanstack/react-query` + `api/client.ts` |
| 图表 | `@ant-design/plots`（7 天柱状图） |
| 时间 | `dayjs`（zh-cn） |
| 编辑器 | antd `Input.TextArea` + 工具栏 + 服务端预览 |
| 构建 | `web/dist` → `go:embed`；服务器不需要 Node |

Go 只提供 `/admin/api/*` JSON；其余 `/admin/*` 返回 SPA 的 `index.html`。

---

## 2. 设计 token

```ts
export const theme = {
  token: {
    colorPrimary: '#2b5cf0',
    colorInfo: '#2b5cf0',
    colorSuccess: '#00a870',
    colorWarning: '#faad14',
    colorError: '#e5484d',
    colorBgLayout: '#f5f7fa',
    colorBgContainer: '#ffffff',
    colorBorder: '#eceff5',
    colorText: '#1f2329',
    colorTextSecondary: '#5c6370',
    colorTextTertiary: '#8a919f',
    borderRadius: 8,
    borderRadiusLG: 10,
    fontSize: 14,
    controlHeight: 36,
  },
  components: {
    Layout: { siderBg: '#fff', headerBg: '#fff', bodyBg: '#f5f7fa' },
    Menu: { itemHeight: 40, itemBorderRadius: 8, itemSelectedBg: '#eef3ff', itemSelectedColor: '#2b5cf0' },
    Table: { headerBg: '#fafbfc', rowHoverBg: '#f7f9fc', cellPaddingBlock: 12 },
  },
};
```

| 元素 | 规格 |
|---|---|
| 侧栏 | 232px，白底，右侧 1px；折叠 64px |
| 顶栏 | 56px，白底，底部 1px；面包屑 + 帮助 + 用户 |
| 内容 | padding 24；标题 20/600；卡片间距 16 |
| 统计卡 | 标签 13px 灰；数字 26px/600 |
| 表格 | 表头 13px 灰；行高 48；无斑马纹 |

---

## 3. 平台颜色

固定映射，未知平台用哈希从调色板取稳定色：

```ts
export const PLATFORM: Record<string, { label: string; color: string }> = {
  wechat:       { label: '公众号',   color: '#07c160' },
  xiaohongshu:  { label: '小红书',   color: '#ff2442' },
  zhihu:        { label: '知乎',     color: '#1677ff' },
  csdn:         { label: 'CSDN',     color: '#fc5531' },
  linkedin:     { label: 'LinkedIn', color: '#0a66c2' },
  x:            { label: 'X',        color: '#111111' },
  facebook:     { label: 'Facebook', color: '#1877f2' },
  instagram:    { label: 'Instagram',color: '#e4405f' },
  telegram:     { label: 'Telegram', color: '#229ed9' },
  wordpress:    { label: 'WordPress',color: '#21759b' },
  douyin:       { label: '抖音',     color: '#fe2c55' },
  bilibili:     { label: 'B 站',     color: '#fb7299' },
};
const PALETTE = ['#2b5cf0', '#00a870', '#fa8c16', '#722ed1', '#13c2c2', '#eb2f96', '#a0d911', '#faad14'];
```

- 渲染：`<Tag color={...}>` 用浅色底（antd `Tag` 传 hex 时自动白字，改用 `style={{color, background: color+'14', borderColor: color+'33'}}` 自定义浅底）。
- 未知平台：`hash(code) % PALETTE.length`，同一 code 永远同色。
- 颜色表只在前端；API 只返回 `platform` 字符串。

---

## 4. 布局与导航

```
┌──────────────┬──────────────────────────────────────────────────────────────┐
│ 微功夫 内容中台 │ 工作台 / 文章                      帮助与说明  ◉ 管理员  超级管理员 ▾ │
│              ├──────────────────────────────────────────────────────────────┤
│ 概览          │                                                              │
│   仪表盘      │   页面标题                                          [主操作]   │
│ 内容          │   一句话说明                                                  │
│   文章        │                                                              │
│   发布队列 ●5 │   ┌────────────────────────────────────────────────────┐   │
│ 配置          │   │  卡片                                                │   │
│   API 客户端  │   └────────────────────────────────────────────────────┘   │
│   设置        │                                                              │
│              │                                                              │
│ 版本 2.0.0    │                                                              │
└──────────────┴──────────────────────────────────────────────────────────────┘
```

- 侧栏只有两组：内容（文章、发布队列）、配置（API 客户端、设置）。
- 「发布队列」角标 = 积压 + 失败数量。
- 顶栏右侧：帮助链接、用户 `Dropdown`（角色 `Tag`、退出）。

---

## 5. 页面

### 5.1 登录 `/admin/login`

- 浅灰底，居中 `Card` 400、圆角 12；logo + 「微功夫内容中台」。
- `Form`：用户名、密码、记住用户名、登录按钮（`block`）。
- 失败 `Alert type="error"`「用户名或密码不正确」；5 次失败锁 15 分钟。
- 底部版权与版本号。

### 5.2 仪表盘 `/admin`

- 页头「概览」+ 右侧 `DatePicker`（本月）。
- 五张统计卡：待发布、队列中、今日已发、失败任务、覆盖平台。
- 「7 天发布」`Card` + `@ant-design/plots` 柱状图（x 日期，y 篇数）。
- 「平台分布」列表：平台色点 + 名称 + 本月数量 + `Progress`（相对最大值）。
- 「队列健康」`Alert`：
  - 绿：调度器 < 5 分钟且积压 < 10；
  - 黄：积压 10–50 或最老任务 > 2 小时；
  - 红：调度器 > 5 分钟或有永久失败。
- 「最近发布」`Table` 5 行：时间、文章、平台、账号、URL（图标外链）。

### 5.3 文章列表 `/admin/articles`

- 顶部统计条（白卡）：4 组可点击的小计，既是数据概览也是快捷筛选：
  - 文章状态：全部 / 草稿 / 待发布 / 归档
  - 发布状态：全部 / 未发布 / 排队中 / 发布中 / 已发布 / 失败
  - 平台审核：全部 / 审核中 / 通过 / 不通过
  - 平台：全部 / 待定（平台未回执）+ 各实际平台及数量
  - 计数基于关键词、语言、时间范围，不随状态筛选变化；点击切换筛选，再点一次取消；当前筛选高亮。
- 筛选一行：关键词 `Input.Search`、平台 `Select`（选项来自常用平台，可手输）、平台审核 `Select`、语言、时间范围、重置。
- `Table` 列：
  - 封面：52×52 缩略图（圆角）；无封面显示「无图」占位
  - 标题（链接到编辑器；副行摘要 40 字灰字）
  - 平台：颜色标签（见 §3）；发布成功前显示「待定」
  - 编辑状态：草稿/待发布/已归档
  - 发布状态：待排期/排队中/发布中/已发布/失败/已取消
  - 平台审核：未审核 / 审核中 / 通过 / 不通过（不通过时附原因）
  - 账号提示
  - 更新时间
  - 操作：编辑、详情、复制为新内容、归档
- 多选批量：归档、复制为新内容；`rowSelection`。
- 行点击标题进编辑器；点「详情」开 `Drawer`（§5.4）。
- 服务端分页：默认每页 20，可选 50/100，显示总数；筛选变化回到第 1 页；筛选写 URL。
- 空态：「还没有文章。可以让智能体通过 API 投稿，或手动新建。」

### 5.4 文章详情 Drawer（宽 720）

- 顶部：标题 + 平台色标签 + 发布状态。
- `Descriptions column=2`：UID、external_id、语言、主题、标签、创建/更新时间、首次/最近发布。
- 封面 `Image` 120px（有则显示）。
- 摘要段落 + 正文渲染（`body_html`）。
- 发布结果（如有）：账号、URL、发布时间；平台审核状态与备注。
- 底部按钮：编辑、复制为新内容、归档。

### 5.5 新建 / 编辑 `/admin/articles/new`、`/admin/articles/:uid/edit`

左右分栏 `Row gutter=24`，左 15 右 9。

左列：

- 标题 `Input showCount maxLength=200`。
- 正文编辑器：
  - 工具栏：H2、加粗、斜体、链接、引用、代码块、图片、分隔线。
  - 左右分栏：左 `Input.TextArea`（等宽、minRows 18）；右预览（服务端 `body_html`），顶部 `Segmented` 桌面/手机。
  - 粘贴/拖拽图片 → `POST /admin/api/media` → 插入 `media:{uid}`。
  - `Ctrl/Cmd+S` 保存；每 10 秒 localStorage 草稿；未保存离开 `Modal.confirm`。
- 摘要 `TextArea rows=3 showCount maxLength=500`。
- 标签 `Select mode="tags"`（≤20 个）。

右列（sticky）：

- 封面 `Upload listType="picture-card"`：上传/替换/删除。
- 平台：**不在编辑器里选择**；详情/列表在发布成功前显示「待定」，由发布回执写入实际平台。
- 语言 `Select`（zh-CN/en/ja/…）。
- 主题 `Input`（自由文本）。
- 目标账号提示 `Input`（可选）。
- 发布方式 `Radio`：自动发布（agent）/ 手动发布（manual）。
- 排期 `DatePicker showTime`，留空=立即。
- 底部固定操作条：保存草稿、保存并排队、复制为新内容。

### 5.6 发布队列 `/admin/queue`

- 顶部：健康 `Alert` + 自动刷新 `Switch`（15s，默认开）+ 「立即检查」按钮（`Popconfirm`）。
- 服务端分页：默认每页 20，可选 50/100，显示总数；自动刷新保持当前页。
- `Table` 列（这是核心页）：
  - 文章标题（点击开详情 `Modal`）
  - 平台：**颜色标签**
  - 发布状态：`Tag`（排队中/发布中/已发布/失败/已取消）
  - 尝试：`1/3`
  - 账号：`result_account` 或「—」
  - URL：图标外链（成功后有）
  - 更新时间 / 下次重试
  - 操作：详情、重试（失败时）、取消（队列中）
- 行点击 → **发布详情 `Modal`（宽 760）**：
  - 标题、平台色标签、状态、尝试次数、租约到期（倒计时）
  - `Descriptions`：任务 UID、文章 UID、创建/完成时间、目标账号提示
  - 发布结果：实际账号、URL（可点）、post_id、发布时间
  - 平台审核：状态、审核次数、审核备注、最终链接
  - 失败信息：错误码、错误信息
  - 正文摘要（前 300 字）
  - 底部：重试、取消、关闭
- 空态：「队列已清空。」

### 5.7 API 客户端 `/admin/clients`

- `Table`：名称（含 uid，行内编辑图标可改名）、类型 `Tag`（Tooltip 展示 scopes）、Token（前 16 + 后 6 位，复制按钮 + 「查看」）、状态、最近使用、操作（轮换/停用）。
- 修改名称 `Modal`：Input ≤100 字；仅影响后台展示，不影响 Token 鉴权。
- 新建 `Modal`：名称、类型 `Radio`（scopes 按类型自动分配）。
- 创建成功：`Modal` 展示完整 Token；列表可随时查看/复制（AES-GCM 密文留存、仅 admin 可读）。
- Token 永久有效、无租期；轮换后旧 Token 立即失效，停用 `Switch`。

### 5.8 设置 `/admin/settings`

- `Tabs`：发布、媒体、接口、健康。
- 发布：tick 上限、抖动分钟、时区（`Select`）、最大尝试次数、租约秒数、重试基数。
- 媒体：单图上限 MB、抓取上限 MB。
- 接口：每分钟限流、每小时文章数、幂等保留小时。
- 健康：`Descriptions`（DB 版本 + `db:check`、存储可写、调度器最近执行、队列积压），刷新按钮。
- 每张卡一个保存；脏表单离开提示。

### 5.9 弹层汇总

| 弹层 | 触发 | 内容 |
|---|---|---|
| 文章详情 Drawer | 列表「详情」/队列标题 | 文章字段 + 正文 + 发布结果 |
| 发布详情 Modal | 队列行点击 | 任务 + 结果 + 失败信息 |
| 复制为新内容 Modal | 列表操作 | 选目标平台、是否复制图片 |
| 归档确认 | 列表操作 | `Popconfirm` |

---

## 6. 交互规范

| 场景 | 规范 |
|---|---|
| 成功 | `message.success` 3 秒 |
| 失败 | `message.error` 6 秒，可复制 request_id |
| 删除/归档/取消 | `Popconfirm` |
| 加载 | 表格 `loading`；详情 `Skeleton` |
| 空态 | `Empty` + 主操作 |
| 权限不足 | 隐藏入口；直达显示 403 `Result` |
| 未保存离开 | `Modal.confirm` |
| 自动刷新 | 队列 15s；页面隐藏暂停 |
| 复制 | `navigator.clipboard`，失败弹可选文本 |
| URL 状态 | 筛选/分页写 query |

---

## 7. 权限

| 能力 | admin | editor | viewer |
|---|---|---|---|
| 查看文章/队列 | ✓ | ✓ | ✓ |
| 新建/编辑/复制 | ✓ | ✓ | — |
| 排期/取消/重试 | ✓ | ✓ | — |
| 归档 | ✓ | — | — |
| API 客户端 | ✓ | — | — |
| 设置 | ✓ | — | — |

服务端二次校验；前端隐藏只是体验。

---

## 8. 状态色与文案

| 状态 | 文案 | 颜色 |
|---|---|---|
| draft | 草稿 | 灰 |
| ready | 待发布 | 蓝 |
| archived | 已归档 | 灰 |
| scheduled | 已排期 | geekblue |
| queued | 排队中 | gold |
| publishing | 发布中 | processing 蓝 |
| published | 已发布 | 绿 |
| failed | 失败 | 红 |
| cancelled | 已取消 | 灰 |

空态文案：

- 文章：「还没有文章。可以让智能体投稿，或手动新建。」
- 队列：「队列已清空。」
- 客户端：「还没有 API 客户端。创建一个给投稿或发布器使用。」
- 403：「当前账号无权访问这个页面。」
- 500：「服务出了点问题，请把 request_id 发给管理员。」

---

## 9. 响应式

| 宽度 | 行为 |
|---|---|
| ≥1440 | 统计卡 5 列；编辑器左右分栏 |
| 1024–1279 | 侧栏折叠；统计卡 2 列；表格横向滚动 |
| < 768 | 侧栏 Drawer；编辑器上下堆叠；表格改卡片 |

---

## 10. 验收（12 条）

1. 登录页与参考截图风格一致：浅灰底、白卡片、蓝色主按钮。
2. 侧栏只有「文章 / 发布队列 / API 客户端 / 设置」等入口，无审核、版本、平台档案、回执、媒体库、标签、审计。
3. 队列平台显示不同颜色，未知平台颜色稳定。
4. 队列行点击弹出详情 Modal，含状态、账号、URL、失败原因。
5. 文章列表平台颜色与状态标签正确。
6. 编辑器左右分栏，预览与详情一致。
7. 粘贴图片能上传并插入 `media:{uid}`。
8. Ctrl/Cmd+S 保存；未保存离开有确认。
9. 创建客户端后 Token 只展示一次。
10. 设置页能改参数并看到健康检查。
11. 403/404/500 与空态齐全。
12. 手机宽度可完成「复制正文 + 标记已发布」（P1 可延后）。

---

## 11. 与旧稿差异

- 页面 16 → 7；删除审核、版本、平台档案、回执、媒体库、主题标签、审计。
- 发布结果并入队列详情 Modal，不再有独立回执页。
- 文章详情由独立页改为 Drawer。
- 平台颜色由前端固定映射 + 哈希，不依赖平台档案表。
