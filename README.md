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
| 额外观赠 | — | 一键打开下载目录、修改下载目录、定位文件、重启 aria2 |

被复用的包在两个分支上**逐字相同**，因此行为天然一致：
`internal/store`、`internal/auth`、`internal/bunkr`、`internal/aria2`、
`internal/downloads`、`internal/bunkrtest`。

唯一差异：`internal/hub` 增加了 `AddSink`，让下载事件可以同时喂给
WebSocket 客户端（Web 版）和 Wails 事件（桌面版）。

---

## 快速开始（不需要 make）

本分支用 Wails v3 自带的任务运行器，**直接使用 `wails3` / `npm` / `go` 命令**，
不依赖 `make`。

### 前置条件

| 依赖 | 版本 | 备注 |
| --- | --- | --- |
| Go | 1.24+ | |
| Node.js | 18+ | |
| wails3 CLI | `v3.0.0-beta.26` | 见下方安装 |
| aria2c | 1.37+ | 见下方说明 |

Linux 上构建还需要 `libgtk-3-dev libwebkit2gtk-4.0-dev`（Wails 的 WebView 依赖）。

### 安装 wails3 CLI

```bash
# 安装到 GOBIN（通常是 ~/go/bin，需在 PATH 中；Windows 为 %USERPROFILE%\go\bin）
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.26

# 验证
wails3 version
```

> 也可以装 `@latest`，但本仓库按 `v3.0.0-beta.26` 开发，建议固定该版本：
> `go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.26`。

### 安装依赖

```bash
# 前端（npm）
cd frontend && npm install && cd ..

# Go 模块
go mod download
```

### 开发模式（前端热更新）

```bash
wails3 dev -config ./build/config.yml
```

`wails3 dev` 从 `build/config.yml` 的 `dev_mode` 段读取配置（`root_path`
必填，否则报 `root path is required`）。它会：后台启动 Vite（默认
<http://localhost:5173>）→ 编译并启动桌面应用 → 监视 `*.go` 改动自动重建重启。

> beta.26 的 `wails3 dev` 只支持 `-config` / `-port` / `-nocolour` / `-s`，
> **没有** `-loglevel`（日志级别在 `build/config.yml` 的 `dev_mode.log_level` 里设）。
>
> 等价方式：`wails3 task dev`（Taskfile 已封装同样的命令）。

### 生产构建（含前端，产物在 bin/）

```bash
wails3 build -nocolour
# 产物：bin/BunkrDownloader（Windows 为 bin/BunkrDownloader.exe）
```

> `wails3 build` 会先执行 `build/*/Taskfile.yml` 里的前端构建并把
> `frontend/dist` 嵌入二进制，无需手动 `npm run build`。

### 直接运行已构建产物

```bash
# 先 build，再运行
wails3 task run
# 或直接运行产物
bin/BunkrDownloader
```

Wails 所有构建参数（版本、tags、输出名）都集中在 `Taskfile.yml` 与
`build/<os>/Taskfile.yml`，`wails3 build` 不接受 `-o/-ldflags/-clean` 参数。

---

## aria2c

| 平台 | 方式 |
| --- | --- |
| Windows / macOS | 首次启动自动下载官方二进制到数据目录的 `aria2/` 子目录 |
| Linux | 用包管理器安装：`apt install aria2` / `apk add aria2` / `dnf install aria2` |
| 任意平台 | 把 `aria2c` 放在**可执行文件同级目录**或加入 `PATH` |
| 任意平台 | `BUNKR_ARIA2_BIN=/path/to/aria2c` |

启动顺序：可执行文件同级目录 → `PATH` → 自动下载。
应用启动时即使 aria2 不可用也能正常打开（账号、会员、历史任务仍可用），
`SystemService.Health()` 会返回 `degraded`。

---

## 数据存放位置

| 内容 | Windows | macOS | Linux |
| --- | --- | --- | --- |
| 数据库 + 日志 + aria2 会话 + 下载目录偏好 | `%APPDATA%\BunkrDownloader` | `~/Library/Application Support/BunkrDownloader` | `$XDG_CONFIG_HOME/bunkrdownloader` |
| 下载文件（默认） | `%USERPROFILE%\Downloads\BunkrDownloader` | `~/Downloads/BunkrDownloader` | `~/Downloads/BunkrDownloader` |

关于存储：
- **下载目录可在“设置 → 运行环境”中修改**，选择会持久化到
  `desktop.json`（位于数据目录），重启后仍然生效；只有桌面版提供此能力，
  Web/gin 版的下载目录是服务端配置，浏览器用户无法修改。
- 全部可覆盖项见 `.env.example`（`BUNKR_DATA_DIR` / `BUNKR_DOWNLOAD_DIR` 等），
  便于便携模式或调试。

---

## 架构

```
main.go                       Wails 应用：窗口、服务注册、事件桥、优雅退出
internal/services/            ← 本分支新增：前端绑定层
  app.go                      共享依赖、错误 → APIError、会话与配额
  auth_service.go             Register / Login / Logout / Me / ChangePassword
  task_service.go             任务的增删改查与生命周期操作
  membership_service.go       套餐、订单、支付、兑换码
  system_service.go           健康、统计、设置、下载目录、打开目录、重启 aria2
  prefs.go                    桌面偏好（下载目录）持久化
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
- 免费版：**5 个链接 / 50 个文件**，并发不限
- 会员：无限链接、无限文件，并发不限
- 只按**链接数**与**文件（下载）数**做额度限制；并发任务数不再设上限
- 提交任务时整批校验链接额度；爬取后按剩余文件额度截断
- 模拟支付网关（`BUNKR_PAYMENT_AUTO=1` 时点击即开通）+ 兑换码
  （默认种子 `BUNKR-MEMBER-2024` / `BUNKR-MEMBER-2025`）
- 购买任意一档会员后，**另一个套餐仍可继续购买/升级/续费**（“当前方案”
  只标记实际购买的套餐，其余付费套餐保留购买按钮）

---

## 开发

```bash
# 改了 Go 服务方法后，重新生成前端绑定（必须）
wails3 generate bindings

# 重新生成图标（可选）
go run .icon/main.go build/icons

# 格式化 Go 代码
go fmt ./...

# 静态检查
go vet ./...

# 单元测试
go test ./internal/store/... ./internal/auth/... ./internal/bunkr/... \
        ./internal/aria2/... ./internal/hub/... ./internal/bunkrtest/... \
        ./internal/config/... ./internal/services/... -count=1

# 集成测试（会拉起真实 aria2c）
go test ./internal/downloads/... -v -timeout 600s -count=1

# 全部测试
go vet ./... && go test ./... -count=1 -timeout 800s

# 整理 go.mod
go mod tidy
```

> 改动了 `internal/services/*.go` 的方法签名后，**必须**重新执行
> `wails3 generate bindings`，否则前端类型会不一致。

---

## 打包

```bash
wails3 package -nocolour
# 产物：build/bin/（Windows NSIS / macOS dmg / Linux AppImage）
```

`build/icons/` 里是源图标；重新生成：

```bash
go run .icon/main.go build/icons
```

---

## 已知限制

- Wails v3 仍处于 beta（`v3.0.0-beta.26`），API 可能在小版本间调整。
- Linux 无头环境构建需要 GTK/WebKit 开发包；纯 `go build` 只能验证编译。
- 支付为模拟网关，不接真实支付渠道。
- 下载目录修改仅桌面版可见（由服务端 `features.desktop` 标记驱动），
  Web/gin 版没有此入口。

---

## 许可证

MIT，见 [LICENSE](LICENSE)。
