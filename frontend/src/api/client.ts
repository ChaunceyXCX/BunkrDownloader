import type {
  ApiErrorBody,
  AppSettings,
  AuthRes,
  CancelOrderReq,
  CancelOrderRes,
  ChangePasswordReq,
  CreateOrderReq,
  CreateOrderRes,
  CreateTaskReq,
  CreateTaskRes,
  DeleteTaskRes,
  FileActionRes,
  Health,
  ListEventsQuery,
  ListEventsRes,
  ListFilesQuery,
  ListFilesRes,
  ListOrdersRes,
  ListTasksQuery,
  ListTasksRes,
  LoginReq,
  MembershipStatusRes,
  OkRes,
  PayOrderReq,
  PayOrderRes,
  PlansRes,
  RedeemReq,
  RedeemRes,
  RegisterReq,
  Stats,
  TaskActionReq,
  TaskActionRes,
  TaskDetailRes,
} from './types'

/** Base path, always slash-terminated so `join` is safe. `/api` by default. */
const RAW_BASE = (import.meta.env.VITE_API_BASE as string | undefined) ?? '/api'
export const API_BASE: string = RAW_BASE.endsWith('/') ? RAW_BASE.slice(0, -1) : RAW_BASE

export const TOKEN_KEY = 'bunkr_token'

/** Typed error thrown by every non-2xx response. */
export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly details?: Record<string, unknown>

  constructor(status: number, code: string, message: string, details?: Record<string, unknown>) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.details = details
  }

  /** True when the session is gone / invalid. */
  get isUnauthorized(): boolean {
    return this.status === 401
  }
}

/* ------------------------------------------------------------------ *
 * Token + 401 plumbing
 * ------------------------------------------------------------------ */
type UnauthorizedHandler = () => void
let onUnauthorized: UnauthorizedHandler | null = null
let onGlobalError: ((err: ApiError) => void) | null = null
/** Endpoints whose failure must not blow up the global error toast. */
const silent = new Set<string>(['/auth/me', '/auth/logout'])

export function onUnauthorizedHandler(fn: UnauthorizedHandler): void {
  onUnauthorized = fn
}
export function onGlobalErrorHandler(fn: (err: ApiError) => void): void {
  onGlobalError = fn
}

export function getToken(): string | null {
  try {
    return localStorage.getItem(TOKEN_KEY)
  } catch {
    return null
  }
}

export function setToken(token: string | null): void {
  try {
    if (token) localStorage.setItem(TOKEN_KEY, token)
    else localStorage.removeItem(TOKEN_KEY)
  } catch {
    /* storage unavailable (private mode) — session stays in-memory only */
  }
}

export function apiUrl(path: string): string {
  return `${API_BASE}${path.startsWith('/') ? path : `/${path}`}`
}

/* ------------------------------------------------------------------ *
 * Core fetch wrapper
 * ------------------------------------------------------------------ */
type Method = 'GET' | 'POST' | 'DELETE' | 'PUT' | 'PATCH'

interface RequestOptions {
  method?: Method
  body?: unknown
  /** Suppress the global error toast (the caller handles it). */
  quiet?: boolean
  signal?: AbortSignal
}

async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const { method = 'GET', body, quiet = false, signal } = opts
  const headers: Record<string, string> = { Accept: 'application/json' }
  const token = getToken()
  if (token) headers.Authorization = `Bearer ${token}`
  if (body !== undefined) headers['Content-Type'] = 'application/json'

  let res: Response
  try {
    res = await fetch(apiUrl(path), {
      method,
      headers,
      signal,
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch (e) {
    // Network / CORS / abort — surface as a normal ApiError so callers
    // have a single failure shape.
    const err = new ApiError(0, 'network_error', (e as Error)?.message || 'Network Error')
    if (!quiet && !silent.has(path)) onGlobalError?.(err)
    throw err
  }

  if (res.status === 204) return undefined as T

  let payload: unknown = null
  const text = await res.text()
  if (text) {
    try {
      payload = JSON.parse(text)
    } catch {
      payload = null
    }
  }

  if (!res.ok) {
    const env = payload as ApiErrorBody | null
    const err = new ApiError(
      res.status,
      env?.error?.code ?? `http_${res.status}`,
      env?.error?.message ?? res.statusText ?? 'Request Failed',
      env?.error?.details,
    )
    if (err.isUnauthorized) {
      setToken(null)
      onUnauthorized?.()
    } else if (!quiet && !silent.has(path)) {
      onGlobalError?.(err)
    }
    throw err
  }

  return payload as T
}

function qs(params: Record<string, unknown> | undefined): string {
  if (!params) return ''
  const sp = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === null || v === '' || v === 0) continue
    sp.set(k, String(v))
  }
  const s = sp.toString()
  return s ? `?${s}` : ''
}

/* ------------------------------------------------------------------ *
 * §2 Auth
 * ------------------------------------------------------------------ */
export const authApi = {
  register: (body: RegisterReq) => request<AuthRes>('/auth/register', { method: 'POST', body }),
  login: (body: LoginReq) => request<AuthRes>('/auth/login', { method: 'POST', body }),
  logout: () => request<OkRes>('/auth/logout', { method: 'POST', quiet: true }),
  me: () => request<AuthRes>('/auth/me'),
  changePassword: (body: ChangePasswordReq) =>
    request<OkRes>('/auth/change-password', { method: 'POST', body }),
}

/* ------------------------------------------------------------------ *
 * §3 Membership
 * ------------------------------------------------------------------ */
export const membershipApi = {
  plans: () => request<PlansRes>('/membership/plans'),
  status: () => request<MembershipStatusRes>('/membership/status'),
  createOrder: (body: CreateOrderReq) =>
    request<CreateOrderRes>('/membership/orders', { method: 'POST', body }),
  orders: () => request<ListOrdersRes>('/membership/orders'),
  pay: (id: number, body: PayOrderReq) =>
    request<PayOrderRes>(`/membership/orders/${id}/pay`, { method: 'POST', body }),
  cancelOrder: (body: CancelOrderReq) =>
    request<CancelOrderRes>('/membership/cancel-order', { method: 'POST', body }),
  redeem: (body: RedeemReq) => request<RedeemRes>('/membership/redeem', { method: 'POST', body }),
}

/* ------------------------------------------------------------------ *
 * §4 Tasks
 * ------------------------------------------------------------------ */
export const tasksApi = {
  list: (q?: ListTasksQuery) => request<ListTasksRes>(`/tasks${qs(q as Record<string, unknown>)}`),
  create: (body: CreateTaskReq) => request<CreateTaskRes>('/tasks', { method: 'POST', body }),
  detail: (id: number) => request<TaskDetailRes>(`/tasks/${id}`),
  remove: (id: number) => request<DeleteTaskRes>(`/tasks/${id}`, { method: 'DELETE' }),
  action: (
    id: number,
    act: 'start' | 'pause' | 'resume' | 'cancel' | 'retry',
    body?: TaskActionReq,
  ) => request<TaskActionRes>(`/tasks/${id}/${act}`, { method: 'POST', body }),
  files: (id: number, q?: ListFilesQuery) =>
    request<ListFilesRes>(`/tasks/${id}/files${qs(q as Record<string, unknown>)}`),
  retryFile: (id: number, fileId: number) =>
    request<FileActionRes>(`/tasks/${id}/files/${fileId}/retry`, { method: 'POST' }),
  events: (id: number, q?: ListEventsQuery) =>
    request<ListEventsRes>(`/tasks/${id}/events${qs(q as Record<string, unknown>)}`),
  recentEvents: (limit = 50) => request<ListEventsRes>(`/events${qs({ limit })}`),
}

/* ------------------------------------------------------------------ *
 * §5 System
 * ------------------------------------------------------------------ */
export const systemApi = {
  health: () => request<Health>('/health'),
  stats: () => request<Stats>('/stats'),
  settings: () => request<AppSettings>('/settings'),
}
