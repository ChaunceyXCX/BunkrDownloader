# BunkrDownloader · HTTP / WebSocket API 契约 (v1)

> 本文件是**前后端唯一契约**。Go (Gin) 后端与 Vue3 前端均以此为准。
> 所有响应均为 `application/json; charset=utf-8`，除 `/api/ws` 升级为 WebSocket。

## 0. 通用约定

### 0.1 错误格式

任何非 2xx 响应：

```json
{ "error": { "code": "quota_exceeded", "message": "免费用户最多添加 5 个链接", "details": { "limit": 5, "used": 5 } } }
```

| 字段 | 说明 |
| --- | --- |
| `error.code` | 机器可读错误码，见下表 |
| `error.message` | 中文可读提示（直接展示给用户） |
| `error.details` | 可选，附加上下文 |

### 0.2 错误码表

| HTTP | code | 含义 |
| --- | --- | --- |
| 400 | `bad_request` | 参数错误 |
| 400 | `invalid_url` | URL 不是 bunkr 链接 |
| 400 | `weak_password` | 密码强度不足 |
| 400 | `email_taken` | 邮箱已注册 |
| 400 | `username_taken` | 用户名已存在 |
| 400 | `invalid_credentials` | 用户名/邮箱或密码错误 |
| 400 | `invalid_state` | 当前状态不允许该操作 |
| 401 | `unauthorized` | 未登录或 token 失效 |
| 403 | `forbidden` | 无权限（非本人资源） |
| 403 | `quota_exceeded` | 超出免费配额 |
| 404 | `not_found` | 资源不存在 |
| 409 | `conflict` | 冲突（如重复提交） |
| 429 | `too_many_requests` | 请求过于频繁 |
| 500 | `internal_error` | 服务端异常 |
| 502 | `upstream_error` | bunkr 站点/接口异常 |
| 503 | `aria2_unavailable` | aria2 未就绪 |

### 0.3 鉴权

除 `GET /api/health`、`GET /api/membership/plans` 外，所有接口需要：

```
Authorization: Bearer <token>
```

- token 为 JWT（HS256），有效期默认 30 天，可在 `.env` / 环境变量 `BUNKR_JWT_TTL` 调整。
- **WebSocket** 无法自定义 header，使用 `GET /api/ws?token=<token>` 查询参数。

### 0.4 时间与数值

- 所有时间字段为 **RFC3339 UTC 字符串**，例：`2024-05-01T12:00:00Z`。
- 所有字节数为 `int64`；`0` 表示未知（前端显示为 `-`）。
- 所有 `speed` 为 `int64` 字节/秒。

---

## 1. 数据结构

### 1.1 User

```json
{
  "id": 1,
  "username": "alice",
  "email": "alice@example.com",
  "role": "user",
  "plan": "free",
  "plan_expires_at": null,
  "is_member": false,
  "created_at": "2024-05-01T00:00:00Z"
}
```

`plan`: `free` | `member`；`role`: `user` | `admin`。
`is_member` = `plan == "member"` 且（`plan_expires_at` 为空或在未来）。

### 1.2 Quota（随用户信息一起返回）

```json
{
  "plan": "free",
  "is_member": false,
  "links_used": 3,
  "links_limit": 5,
  "links_unlimited": false,
  "files_used": 42,
  "files_limit": 50,
  "files_unlimited": false,
  "concurrent_limit": 1,
  "concurrent_running": 1
}
```

- 免费用户：`links_limit = 5`，`files_limit = 50`，`concurrent_limit = 1`。
- 会员：`links_unlimited = true`、`files_unlimited = true`、`concurrent_limit = 5`，`limit` 字段返回 `null` 语义（后端返回 `-1`，前端渲染为 ∞）。

### 1.3 Task

```json
{
  "id": 12,
  "user_id": 1,
  "url": "https://bunkr.si/a/AbCdEfGh",
  "kind": "album",
  "album_id": "AbCdEfGh",
  "album_name": "My Album",
  "status": "running",
  "download_path": "/downloads/My Album (AbCdEfGh)",
  "options": { "max_retries": 5, "connections": 4, "rate_limit_kbps": 0, "ignore": [], "include": [], "no_album_folder": false, "clean_name": false },
  "error_message": null,
  "total_files": 20,
  "completed_files": 8,
  "failed_files": 1,
  "skipped_files": 0,
  "pending_files": 10,
  "downloading_files": 1,
  "total_bytes": 1073741824,
  "downloaded_bytes": 268435456,
  "speed": 524288,
  "progress": 25.0,
  "created_at": "2024-05-01T00:00:00Z",
  "started_at": "2024-05-01T00:00:01Z",
  "finished_at": null,
  "updated_at": "2024-05-01T00:00:20Z"
}
```

`status`: `pending` | `crawling` | `running` | `paused` | `completed` | `failed` | `canceled`

`progress` 百分比 0–100（保留 2 位小数，按字节计算；`total_bytes=0` 时为 0）。

### 1.4 File

```json
{
  "id": 101,
  "task_id": 12,
  "item_url": "https://bunkr.si/v/XyZ12345",
  "filename": "video.mp4",
  "download_link": "https://cdn.example/video.mp4?token=...",
  "gid": "2089b05ecca3d829",
  "file_size": 52428800,
  "downloaded_bytes": 10485760,
  "speed": 131072,
  "progress": 20.0,
  "status": "downloading",
  "retry_count": 0,
  "error_message": null,
  "item_date": "2024-04-20T10:00:00Z",
  "created_at": "...",
  "started_at": "...",
  "finished_at": "...",
  "updated_at": "..."
}
```

`status`: `pending` | `downloading` | `completed` | `failed` | `skipped`

`gid` 为 aria2 下载组 ID；未入队时为 `null`。

### 1.5 Event（任务日志）

```json
{ "id": 900, "task_id": 12, "file_id": null, "level": "info", "event": "Album crawled", "details": "Found 20 item(s)", "created_at": "..." }
```

`level`: `info` | `warn` | `error` | `success`

### 1.6 Order（会员订单）

```json
{
  "id": 7,
  "user_id": 1,
  "plan": "member_monthly",
  "amount_cents": 990,
  "currency": "CNY",
  "status": "pending",
  "provider": "mock",
  "trade_no": "MOCK-20240501-0007",
  "created_at": "...",
  "paid_at": null,
  "expires_at": "..."
}
```

`status`: `pending` | `paid` | `canceled` | `refunded`

---

## 2. 认证接口

### POST /api/auth/register

```json
// Req
{ "username": "alice", "email": "alice@example.com", "password": "s3cret-pass" }
// 201 Res
{ "user": { ...User }, "token": "<jwt>", "quota": { ...Quota } }
```

校验：用户名 3–24 字符（字母数字下划线）；邮箱合法；密码 ≥ 6 字符。

### POST /api/auth/login

```json
// Req
{ "account": "alice@example.com", "password": "s3cret-pass" }
// 200 Res
{ "user": { ...User }, "token": "<jwt>", "quota": { ...Quota } }
```

`account` 可为邮箱或用户名。

### POST /api/auth/logout

`200 { "ok": true }`（客户端丢弃 token 即可，服务端无状态）。

### GET /api/auth/me

`200 { "user": {...User}, "quota": {...Quota} }`

### POST /api/auth/change-password

```json
{ "old_password": "...", "new_password": "..." } → { "ok": true }
```

---

## 3. 会员接口

### GET /api/membership/plans （公开）

```json
{
  "plans": [
    { "id": "free", "name": "免费版", "price_cents": 0, "currency": "CNY", "period_days": 0,
      "features": ["最多 5 个链接", "最多 50 个文件", "同时 1 个任务"],
      "limits": { "links": 5, "files": 50, "concurrent": 1 } },
    { "id": "member_monthly", "name": "会员 · 月付", "price_cents": 990, "currency": "CNY", "period_days": 30,
      "features": ["无限链接", "无限文件", "同时 5 个任务", "优先队列"],
      "limits": { "links": -1, "files": -1, "concurrent": 5 } },
    { "id": "member_yearly", "name": "会员 · 年付", "price_cents": 9900, "currency": "CNY", "period_days": 365,
      "features": ["无限链接", "无限文件", "同时 5 个任务", "优先队列", "立省 17%"],
      "limits": { "links": -1, "files": -1, "concurrent": 5 } }
  ]
}
```

### POST /api/membership/orders

```json
{ "plan": "member_monthly" } → 201 { "order": {...Order} }
```

### GET /api/membership/orders

`200 { "orders": [ {...Order} ] }`（当前用户，按时间倒序）

### POST /api/membership/orders/:id/pay

模拟支付网关回调（演示用）。环境变量 `BUNKR_PAYMENT_AUTO=1`（默认开启）时立即置为 `paid` 并升级会员。

```json
{ "pay_method": "alipay" } → 200 { "order": {...Order}, "user": {...User}, "quota": {...Quota} }
```

### POST /api/membership/cancel-order

```json
{ "order_id": 7 } → 200 { "order": {...Order} }   // 仅 pending 可取消
```

### POST /api/membership/redeem

使用兑换码直接开通（离线/内网场景）。

```json
{ "code": "BUNKR-MEMBER-2024" } → 200 { "user": {...User}, "quota": {...Quota} }
```

### GET /api/membership/status

`200 { "plan": "...", "is_member": true, "expires_at": "...", "quota": {...Quota} }`

---

## 4. 任务接口

### GET /api/tasks

Query：`status`（可空）、`q`（模糊匹配 url/album_name）、`limit`（默认 50，最大 200）、`offset`、`sort`（`created_at` | `updated_at`，默认 `created_at`，`asc|desc`）

`200 { "tasks": [ {...Task} ], "total": 120 }`

### POST /api/tasks

```json
{
  "url": "https://bunkr.si/a/xxx\nhttps://bunkr.si/v/yyy",   // 字符串按行分隔，或传数组
  "options": {
    "max_retries": 5, "connections": 4, "rate_limit_kbps": 0,
    "ignore": [".tmp"], "include": [],
    "no_album_folder": false, "clean_name": false,
    "custom_path": ""
  },
  "auto_start": true
}
→ 201 { "task_ids": [12, 13], "count": 2, "quota": {...Quota} }
```

**配额校验**：创建前一次性校验 `links_used + count <= links_limit`（会员跳过）。
不足时 `403 quota_exceeded`，`details: { "limit": 5, "used": 5, "requested": 2 }`。

### GET /api/tasks/:id

`200 { "task": {...Task}, "stats": { ...与 Task 统计字段相同 }, "files_summary": { "pending": 1, ... } }`

### DELETE /api/tasks/:id

取消任务并删除记录（级联删除 files/events）。`200 { "ok": true, "deleted": 12 }`

### POST /api/tasks/:id/start · pause · resume · cancel · retry

```json
// Req (可选): { "files": [101, 102] }   仅重试指定文件
→ 200 { "ok": true, "task": {...Task} }
```

- `start`：pending/failed → running（受并发上限约束，超出则排队为 `pending`）。
- `pause`：立即 `aria2.pause`，状态 `paused`。
- `resume`：恢复 `paused` 任务及其全部 paused 的 aria2 gid。
- `cancel`：终止所有 gid，状态 `canceled`。
- `retry`：把所有 `failed` 文件重置为 `pending` 并重新入队，然后 `start`。

### GET /api/tasks/:id/files

Query：`status`、`q`（按 filename 过滤）、`limit`（默认 50，最大 200）、`offset`、`sort`（`filename|size|status|created_at`，默认 `filename`，`asc|desc`）

`200 { "files": [ {...File} ], "total": 20, "limit": 50, "offset": 0 }`

### POST /api/tasks/:id/files/:fileId/retry

`200 { "ok": true, "file": {...File} }`

### GET /api/tasks/:id/events

Query：`before_id`、`limit`（默认 200）。`200 { "events": [ {...Event} ] }`（按 id 倒序）

### GET /api/events

全局最近事件（当前用户）。`200 { "events": [ {...Event} ] }`

---

## 5. 系统接口

### GET /api/health （公开）

```json
{ "status": "ok", "version": "1.0.0", "uptime_seconds": 1234, "aria2": { "available": true, "version": "1.37.0" } }
```

### GET /api/stats

```json
{
  "total_tasks": 12, "running": 1, "pending": 0, "paused": 0,
  "completed": 9, "failed": 2, "canceled": 0,
  "total_files": 120, "completed_files": 80, "downloaded_bytes": 10737418240,
  "speed": 1048576, "active_files": 2,
  "aria2": { "download_speed": 1048576, "active": 2, "waiting": 0, "stopped": 118, "num_of_files": 120 },
  "quota": { ...Quota }
}
```

### GET /api/settings

```json
{ "download_dir": "/downloads", "version": "1.0.0", "features": { "aria2": true, "payment": "mock" } }
```

---

## 6. WebSocket `/api/ws?token=<jwt>`

连接成功后服务端**立即**发送 `hello` 快照帧，然后持续推送增量帧。

### 6.1 客户端 → 服务端

```json
{ "action": "ping" }              // 服务端回 { "type": "pong", "ts": 1234567890 }
{ "action": "subscribe", "task_id": 12 }   // 订阅单任务详细事件
{ "action": "unsubscribe", "task_id": 12 }
```

### 6.2 服务端 → 客户端

| `type` | 触发 | `data` 结构 |
| --- | --- | --- |
| `hello` | 连接建立 | `{ user, quota, stats, tasks: [Task], aria2 }` |
| `pong` | 心跳响应 | `{ ts }` |
| `task_created` | 新建任务 | `{ task: Task }` |
| `task_updated` | 任务字段变化 | `{ task: Task }` |
| `task_progress` | 进度刷新（≤1 次/秒） | `{ task: Task }` |
| `task_completed` | 任务结束 | `{ task: Task }` |
| `file_progress` | 单文件进度（≤1 次/秒/文件） | `{ task_id, file: File }` |
| `file_created` | 文件入队 | `{ task_id, file: File }` |
| `file_updated` | 文件状态变化 | `{ task_id, file: File }` |
| `log` | 任务日志 | `{ task_id, event: Event }` |
| `quota` | 配额变化 | `{ quota: Quota }` |
| `stats` | 全局统计（2 秒一次） | `{ stats: {...} }` |
| `error` | 服务端错误 | `{ code, message }` |

所有帧统一结构：

```json
{ "type": "task_progress", "ts": 1714558800, "data": { "...": "..." } }
```

断线策略：客户端指数退避重连（1s→2s→4s→最大 15s），连接成功后重置。

---

## 7. 前端路由（gin 分支 SPA）

| 路由 | 说明 |
| --- | --- |
| `/login` | 登录/注册（同一页面切换） |
| `/app` | 仪表盘（任务列表） |
| `/app/tasks/:id` | 任务详情（文件 + 实时日志） |
| `/app/membership` | 会员中心（套餐、订单、兑换码） |
| `/app/settings` | 设置（默认下载目录、主题、连接数、限速） |
| `/*` | SPA fallback → `index.html` |

WebSocket 心跳间隔 25s（服务端每 20s 发 `ping` 帧，客户端回 `pong`）。
