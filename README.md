# BunkrDownloader · 桌面版

把 [Web 版](../blob/gin/README.md) 的能力搬进 **Wails v3** 桌面应用：
同一个 Go 领域层（`store` / `auth` / `bunkr` / `aria2` / `downloads`），
前端从 REST + WebSocket 换成 **Wails 服务绑定 + Wails 事件**，
界面与功能保持一致。

---

## 与 Web 版的关系

| | `gin` 分支 | `wails` 分支（本分支） |
| --- | --- | --- |
| 形态 | Web 服务 + 内嵌 SPA | 桌面应用 |
| 传输 | REST `/api/*` + WebSocket | Wails 服务绑定 + Wails 事件 |
| 入口 | `cmd/bunkr-web` | `main.go` |
| 界面 | Vue 3 + TS + Tailwind | **同一套**（仅替换传输层） |
| 下载引擎 | aria2c | aria2c（同一个 `internal/aria2`） |
| 账号/会员 | 有 | 有（同一套 `store` + 配额逻辑） |
| 额外观赠 | — | 一键打开下载目录、定位文件、重启 aria2 |

被复用的包在两个分支上**逐字相同**，因此行为天然一致：
`internal/store`、`internal/auth`、`internal/bunkr`、`internal/aria2`、
`internal/downloads`、`internal/bunkrtest`。

唯一差异：`internal/hub` 增加了 `AddSink`，让下载事件可以同时喂给
WebSocket 客户端（Web 版）和 Wails 事件（桌面版）。

---

## 快速开始

### 前置条件

| 依赖 | 版本 | 备注 |
| --- | --- | --- |
| Go | 1.24+ | |
| Node.js | 18+ | |
| wails3 CLI | `v3.0.0-beta.26` | `make install-cli` |
| aria2c | 1.37+ | 见下方说明 |

Linux 上构建还需要 `libgtk-3-dev libwebkit2gtk-4.0-dev`（Wails 的 WebView 依赖）。

```bash
# 1. 安装 wails3 CLI
make install-cli

# 2. 安装依赖
make setup

# 3. 开发模式（前端热更新）
make dev

# 4. 生产构建 → bin/BunkrDownloader
make build
```

### aria2c

| 平台 | 方式 |
| --- | --- |
| Windows / macOS | 首次启动自动下载官方二进制到 `%APPDATA%/BunkrDownloader/aria2/` |
| Linux | 用包管理器安装：`apt install aria2` / `apk add aria2` / `dnf install aria2` |
| 任意平台 | 把 `aria2c` 放在**可执行文件同级目录**或加入 `PATH` |
| 任意平台 | `BUNKR_ARIA2_BIN=/path/to/aria2c` |

启动顺序：可执行文件同级目录 → `PATH` → 自动下载。
应用启动时即使 aria2 不可用也能正常打开（账号、会员、历史任务仍可用），
`GET /api/health` 等价的 `SystemService.Health()` 会返回 `degraded`。

---

## 数据存放位置

| 内容 | Windows | macOS | Linux |
| --- | --- | --- | --- |
| 数据库 + 日志 + aria2 会话 | `%APPDATA%\BunkrDownloader` | `~/Library/Application Support/BunkrDownloader` | `$XDG_CONFIG_HOME/bunkrdownloader` |
| 下载文件 | `%USERPROFILE%\Downloads\BunkrDownloader` | `~/Downloads/BunkrDownloader` | `~/Downloads/BunkrDownloader` |

全部可用环境变量覆盖（见 `.env.example`），便于便携模式或调试。

---

## 架构

```
main.go                       Wails 应用：窗口、服务注册、事件桥、优雅退出
internal/services/            ← 本分支新增：前端绑定层
  app.go                      共享依赖、错误 → APIError、会话与配额
  auth_service.go             Register / Login / Logout / Me / ChangePassword
  task_service.go             任务的增删改查与生命周期操作
  membership_service.go       套餐、订单、支付、兑换码
  system_service.go           健康、统计、设置、打开目录、重启 aria2
internal/store|auth|bunkr|aria2|downloads|hub|bunkrtest
                             ← 与 Web 版逐字相同
frontend/src/                 Vue 3 + TS + Tailwind（与 Web 版相同）
frontend/src/api/client.ts    ← 本分支改写：REST → Wails 绑定
frontend/src/api/ws.ts        ← 本分支改写：WebSocket → Wails 事件
frontend/bindings/            wails3 generate bindings 产物
```

### 事件桥

`main.go` 把下载事件注册为 hub sink，转换成 Wails 事件：

```go
events.AddSink(func(userID, taskID int64, frame hub.Frame) {
    app.Event.Emit("bunkr:"+frame.Type, map[string]any{
        "type": frame.Type, "ts": frame.TS, "data": frame.Data,
        "user_id": userID, "task_id": taskID,
    })
})
```

前端 `src/api/ws.ts` 订阅 `bunkr:*` 并还原成与 Web 版同构的 `WsFrame`，
因此 `stores/tasks.ts` 等业务代码**一行未改**。

事件类型：`hello`（桌面版由首屏拉取代替）、`task_created`、`task_updated`、
`task_progress`、`task_completed`、`file_created`、`file_progress`、
`file_updated`、`log`、`quota`、`stats`、`auth`、`error`。

---

## 账号与会员

与 Web 版完全相同的一套语义：

- 注册 / 登录 / 改密，密码用 scrypt 哈希（由 `store` 统一处理，绝不明文落库）
- 免费版：**5 个链接 / 50 个文件 / 1 个并发**
- 会员：无限链接、无限文件、5 个并发
- 提交任务时整批校验链接额度；爬取后按剩余文件额度截断
- 模拟支付网关（`BUNKR_PAYMENT_AUTO=1` 时点击即开通）+ 兑换码
  （默认种子 `BUNKR-MEMBER-2024` / `BUNKR-MEMBER-2025`）

---

## 开发

```bash
make bindings        # 改了 Go 服务后重新生成前端绑定
make lint            # gofmt + go vet
make test            # vet + 单元 + 集成测试
make test-integration # 全链路集成测试（会拉起真实 aria2c）
```

改动了 `internal/services/*.go` 的方法签名后，**必须**重新执行
`make bindings`，否则前端类型会不一致。

---

## 打包

```bash
make package         # Windows NSIS / macOS dmg / Linux AppImage
```

`build/icons/` 里是源图标（`make` 不需要它，`wails3` 使用
`build/appicon.png` 与 `build/windows/icon.ico`）。重新生成：

```bash
go run .icon/main.go build/icons
```

---

## 已知限制

- Wails v3 仍处于 beta（`v3.0.0-beta.26`），API 可能在小版本间调整。
- Linux 无头环境构建需要 GTK/WebKit 开发包；纯 `go build` 只能验证编译。
- 支付为模拟网关，不接真实支付渠道。

---

## 许可证

MIT，见 [LICENSE](LICENSE)。
