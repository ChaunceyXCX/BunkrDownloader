/* ------------------------------------------------------------------ *
 * TypeScript mirrors of docs/API.md §1 (data structures) and the
 * request/response envelopes of §2–§5. Field names MUST NOT be renamed.
 * ------------------------------------------------------------------ */

/* ---------- §1.1 User ---------- */
export type Plan = 'free' | 'member'
export type Role = 'user' | 'admin'

export interface User {
  id: number
  username: string
  email: string
  role: Role
  plan: Plan
  /** RFC3339 UTC or null (lifetime member) */
  plan_expires_at: string | null
  is_member: boolean
  created_at: string
}

/* ---------- §1.2 Quota ---------- */
export interface Quota {
  plan: Plan
  is_member: boolean
  links_used: number
  /** -1 means unlimited on the wire; render as ∞ */
  links_limit: number
  links_unlimited: boolean
  files_used: number
  /** -1 means unlimited on the wire; render as ∞ */
  files_limit: number
  files_unlimited: boolean
  concurrent_limit: number
  concurrent_running: number
}

/* ---------- §1.3 Task ---------- */
export type TaskStatus =
  | 'pending'
  | 'crawling'
  | 'running'
  | 'paused'
  | 'completed'
  | 'failed'
  | 'canceled'

export type TaskKind = 'album' | 'item' | string

export interface TaskOptions {
  max_retries: number
  connections: number
  rate_limit_kbps: number
  ignore?: string[]
  include?: string[]
  no_album_folder: boolean
  clean_name: boolean
  custom_path?: string
}

export interface Task {
  id: number
  user_id: number
  url: string
  kind: TaskKind
  album_id: string | null
  album_name: string | null
  status: TaskStatus
  download_path: string
  options: TaskOptions
  error_message: string | null
  total_files: number
  completed_files: number
  failed_files: number
  skipped_files: number
  pending_files: number
  downloading_files: number
  total_bytes: number
  downloaded_bytes: number
  speed: number
  /** 0–100, 2 decimals */
  progress: number
  created_at: string
  started_at: string | null
  finished_at: string | null
  updated_at: string
}

/* ---------- §1.4 File ---------- */
export type FileStatus = 'pending' | 'downloading' | 'completed' | 'failed' | 'skipped'

export interface DownloadFile {
  id: number
  task_id: number
  item_url: string
  filename: string
  download_link: string | null
  /** aria2 download group id, null when not enqueued */
  gid: string | null
  /** 0 means unknown → render as "-" */
  file_size: number
  downloaded_bytes: number
  speed: number
  progress: number
  status: FileStatus
  retry_count: number
  error_message: string | null
  item_date: string | null
  created_at: string
  started_at: string | null
  finished_at: string | null
  updated_at: string
}

/* ---------- §1.5 Event ---------- */
export type EventLevel = 'info' | 'warn' | 'error' | 'success'

export interface TaskEvent {
  id: number
  task_id: number
  file_id: number | null
  level: EventLevel
  event: string
  details: string | null
  created_at: string
}

/* ---------- §1.6 Order ---------- */
export type OrderStatus = 'pending' | 'paid' | 'canceled' | 'refunded'
export type PlanId = 'free' | 'member_monthly' | 'member_yearly'

export interface Order {
  id: number
  user_id: number
  plan: PlanId
  amount_cents: number
  currency: string
  status: OrderStatus
  provider: string
  trade_no: string
  created_at: string
  paid_at: string | null
  expires_at: string | null
}

export interface PlanItem {
  id: PlanId
  name: string
  price_cents: number
  currency: string
  period_days: number
  features: string[]
  limits: { links: number; files: number; concurrent: number }
}

/* ---------- §5 system ---------- */
export interface Aria2Info {
  available: boolean
  version: string
}

export interface Health {
  status: string
  version: string
  uptime_seconds: number
  aria2: Aria2Info
}

export interface Aria2Globals {
  download_speed: number
  active: number
  waiting: number
  stopped: number
  num_of_files: number
}

export interface Stats {
  total_tasks: number
  running: number
  pending: number
  paused: number
  completed: number
  failed: number
  canceled: number
  total_files: number
  completed_files: number
  downloaded_bytes: number
  speed: number
  active_files: number
  aria2: Aria2Globals
  quota: Quota
}

export interface AppSettings {
  download_dir: string
  version: string
  features: { aria2: boolean; payment: string; desktop?: boolean }
}

/* ---------- error envelope §0.1 ---------- */
export type ErrorCode =
  | 'bad_request'
  | 'invalid_url'
  | 'weak_password'
  | 'email_taken'
  | 'username_taken'
  | 'invalid_credentials'
  | 'invalid_state'
  | 'unauthorized'
  | 'forbidden'
  | 'quota_exceeded'
  | 'not_found'
  | 'conflict'
  | 'too_many_requests'
  | 'internal_error'
  | 'upstream_error'
  | 'aria2_unavailable'
  | (string & {})

export interface ApiErrorBody {
  error: {
    code: ErrorCode
    message: string
    details?: Record<string, unknown>
  }
}

/* ---------- request bodies ---------- */
export interface RegisterReq {
  username: string
  email: string
  password: string
}
export interface LoginReq {
  account: string
  password: string
}
export interface ChangePasswordReq {
  old_password: string
  new_password: string
}
export interface CreateTaskReq {
  url: string
  options: TaskOptions
  auto_start: boolean
}
export interface TaskActionReq {
  files?: number[]
}
export interface CreateOrderReq {
  plan: PlanId
}
export interface PayOrderReq {
  pay_method: string
}
export interface CancelOrderReq {
  order_id: number
}
export interface RedeemReq {
  code: string
}

/* ---------- response envelopes ---------- */
export interface AuthRes {
  user: User
  token?: string
  quota: Quota
}
export interface MeRes {
  user: User
  quota: Quota
}
export interface OkRes {
  ok: true
}
export interface ListTasksRes {
  tasks: Task[]
  total: number
}
export interface TaskDetailRes {
  task: Task
  stats: Task
  files_summary: Partial<Record<FileStatus, number>>
}
export interface ListFilesRes {
  files: DownloadFile[]
  total: number
  limit: number
  offset: number
}
export interface ListEventsRes {
  events: TaskEvent[]
}
export interface FileActionRes {
  ok: true
  file: DownloadFile
}
export interface TaskActionRes {
  ok: true
  task: Task
}
export interface DeleteTaskRes {
  ok: true
  deleted: number
}
export interface CreateTaskRes {
  task_ids: number[]
  count: number
  quota: Quota
}
export interface PlansRes {
  plans: PlanItem[]
}
export interface ListOrdersRes {
  orders: Order[]
}
export interface CreateOrderRes {
  order: Order
}
export interface PayOrderRes {
  order: Order
  user: User
  quota: Quota
}
export interface CancelOrderRes {
  order: Order
}
export interface RedeemRes {
  user: User
  quota: Quota
}
export interface MembershipStatusRes {
  plan: PlanId
  is_member: boolean
  expires_at: string | null
  quota: Quota
}

export interface ListTasksQuery {
  status?: TaskStatus | ''
  q?: string
  limit?: number
  offset?: number
  sort?: 'created_at' | 'updated_at'
  order?: 'asc' | 'desc'
}

export interface ListFilesQuery {
  status?: FileStatus | ''
  q?: string
  limit?: number
  offset?: number
  sort?: 'filename' | 'size' | 'status' | 'created_at'
  order?: 'asc' | 'desc'
}

export interface ListEventsQuery {
  before_id?: number
  limit?: number
}

/* ---------- §6 WebSocket frames ---------- */
export interface WsFrame<T = unknown> {
  type: string
  ts?: number
  data: T
}

export interface WsHelloData {
  user: User
  quota: Quota
  stats: Stats
  tasks: Task[]
  aria2: Aria2Globals
}
export interface WsTaskData {
  task: Task
}
export interface WsFileData {
  task_id: number
  file: DownloadFile
}
export interface WsLogData {
  task_id: number
  event: TaskEvent
}
export interface WsQuotaData {
  quota: Quota
}
export interface WsStatsData {
  stats: Stats
}
export interface WsErrorData {
  code: string
  message: string
}
export interface WsPongData {
  ts: number
}
