# BunkrDownloader

一个用 **Go + Gin** 重写的 Bunkr 相册批量下载器：**aria2** 负责实际传输，
**Vue 3 + TypeScript + Tailwind** 提供控制台界面，内置**账号体系**与**会员额度**。

> 本分支（`gin`）为 Web 服务形态。同样的后端能力另有 `wails` 分支的桌面应用形态。

---

## 功能一览

| 能力 | 说明 |
| --- | --- |
| 批量下载 | 相册（`/a/`）与单文件（`/v/`）链接，支持一次提交多条、文本换行分隔 |
| 下载引擎 | 全程由 **aria2c** 承载：多线程分片、断点续传、暂停/继续、失败重试（指数退避） |
| 直链签名 | 复刻原 Python 逻辑：`jsCDN` 解析 → 签名 API 换 token，档案类走 download API 兜底 |
| 任务队列 | 按链接/文件额度限制；并发不限；进程重启后自动恢复到可续传状态 |
| 实时进度 | WebSocket 推送任务/文件进度、事件日志、全局统计，断线指数退避重连 |
| 账号体系 | 注册 / 登录 / JWT 会话 / 改密，scrypt 密码哈希 |
| 会员额度 | 免费 **5 个链接 / 50 个文件**、并发不限；会员无限下载 |
| 会员购买 | 套餐页 + 模拟支付网关 + 订单记录 + 兑换码激活 |
| 界面 | 深/浅色主题、中英文切换、骨架屏、乐观更新、空态、响应式布局 |
| 部署 | 多阶段 Dockerfile（内置 aria2c、非 root、tini、healthcheck）、docker compose |

---

## 快速开始

### 方式一：Docker（推荐）

```bash
git clone https://github.com/chaunceyxie1/BunkrDownloader.git
cd BunkrDownloader
git checkout gin

cp .env.example .env          # 可选：修改端口 / JWT 密钥 / 免费额度
docker compose up -d --build
```

打开 <http://localhost:8765>。镜像已内置 `aria2c`，首启动无需联网下载。

### 方式二：本地构建

```bash
git checkout gin
make setup                    # 安装前端与 Go 依赖
make build                    # 构建前端 + 编译内嵌 SPA 的二进制
./bin/windows-amd64/bunkr-web # 或 ./bin/linux-amd64/bunkr-web
```

> 前端开发模式（热更新）：
> ```bash
> cd frontend && npm run dev   # 终端 1，Vite 监听 :5173
> make build && ./bin/*/bunkr-web  # 终端 2，API 在 :8765
> ```

### aria2c 说明

| 平台 | 获取方式 |
| --- | --- |
| Linux | 发行版包管理器（镜像内已装：`apk add aria2`） |
| Windows / macOS | 首次启动自动下载官方二进制到 `$BUNKR_DATA_DIR/aria2/` |

也可用 `BUNKR_ARIA2_BIN` 指向已有的 `aria2c`，或用 `BUNKR_ARIA2_AUTO_FETCH=false` 关闭自动下载。

---

## 配置

所有配置走环境变量，完整清单见 [`.env.example`](.env.example)。常用项：

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `BUNKR_PORT` | `8765` | 监听端口 |
| `BUNKR_DATA_DIR` | `~/.bunkr_downloader` | 数据库 + aria2 会话 |
| `BUNKR_DOWNLOAD_DIR` | `$BUNKR_DATA_DIR/downloads` | 下载目录 |
| `BUNKR_JWT_SECRET` | 自动生成并持久化 | 生产环境请显式设置 |
| `BUNKR_FREE_LINKS_LIMIT` | `5` | 免费用户链接上限 |
| `BUNKR_FREE_FILES_LIMIT` | `50` | 免费用户文件上限 |
| `BUNKR_FREE_CONCURRENCY` | `0` | 并发任务数，0=不限 |
| `BUNKR_MEMBER_CONCURRENCY` | `0` | 并发任务数，0=不限 |
| `BUNKR_ARIA2_BIN` | 自动探测 | aria2c 路径 |
| `BUNKR_PAYMENT_AUTO` | `true` | 模拟支付是否直接置为已支付 |

---

## 额度与会员

| 套餐 | 链接 | 文件 | 并发 | 价格 |
| --- | --- | --- | --- | --- |
| 免费版 | 5 | 50 | 不限 | ¥0 |
| 会员 · 月付 | ∞ | ∞ | 不限 | ¥9.90 |
| 会员 · 年付 | ∞ | ∞ | 不限 | ¥99.00 |

- 提交任务时**一次性校验**链接额度，超限返回 `403 quota_exceeded`（整批拒绝，不做部分创建）。
- 相册爬取完成后按剩余文件额度截断：超出的文件标记为 `skipped` 并写明原因，不消耗额度。
- 兑换码（默认种子）：`BUNKR-MEMBER-2024`（30 天）、`BUNKR-MEMBER-2025`（365 天）。
- 支付为**模拟网关**（`BUNKR_PAYMENT_AUTO=1` 时点击即开通），便于离线演示。

---

## 项目结构

```
cmd/bunkr-web/           服务入口
internal/
  api/                   Gin 路由、处理器、WebSocket 升级
  aria2/                 aria2 JSON-RPC 客户端 + 进程守护 + 二进制获取
  auth/                  JWT 签发校验 + scrypt 密码哈希
  bunkr/                 Bunkr 爬虫（URL 解析 / 相册分页 / 文件名 / 直链签名）
  bunkrtest/             本地 mock Bunkr 服务（测试用，支持 Range 与限速）
  config/                环境变量配置
  downloads/             下载编排器（发现 → 签名 → 入队 → 轮询 → 落库 → 广播）
  hub/                   WebSocket 广播中心
  store/                 SQLite 持久化（users/tasks/files/events/orders/redeem_codes）
  web/                   内嵌 SPA
frontend/                Vue 3 + TS + Tailwind 前端
docs/API.md              前后端 API 契约
scripts/e2e.sh           HTTP 全流程冒烟测试
```

---

## 测试

```bash
make test                 # go vet + 全部单元/集成测试
make test-unit            # 仅单元测试
make test-integration     # 集成测试（会拉起真实 aria2c）
sh scripts/e2e.sh         # 对运行中的服务跑 61 项 HTTP 冒烟
```

集成测试通过 `internal/bunkrtest` 的本地 mock 站点跑通**完整链路**：
抓取相册 → 解析直链 → aria2 传输（含 Range 分片、暂停/续传）→ 校验磁盘字节与 SHA-256。

覆盖场景：相册/单文件/档案兜底、暂停续传、取消、免费额度截断、并发排队、
进程重启后恢复、include/ignore 过滤。

---

## 常见问题

**页面提示「aria2 不可用」** — 检查 `GET /api/health` 的 `aria2.available`。Linux 下请
`apk add aria2` / `apt install aria2`，或设置 `BUNKR_ARIA2_BIN`。

**登录后刷新掉线** — 容器重启且数据卷被清空会丢失自动生成的 JWT 密钥。
请在 `.env` 中固定 `BUNKR_JWT_SECRET`。

**下载速度为 0 但任务完成** — 小文件在轮询间隔内完成属于正常；
进度由 1 秒一次的轮询驱动。

---

## 许可证

MIT，见 [LICENSE](LICENSE)。
