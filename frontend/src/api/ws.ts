import { API_BASE, getToken } from './client'
import type { WsFrame } from './types'

/**
 * Native WebSocket client for `GET /api/ws?token=<jwt>` (docs/API.md §6).
 *
 * - exponential backoff reconnect 1s → 2s → 4s → … → 15s max, reset on open
 * - 25s heartbeat: the client sends `{action:"ping"}`; the server answers `pong`
 *   (the server may also initiate a `ping` frame, which is answered the same way)
 * - subscribe/unsubscribe a single task for its detailed event stream
 */
export type WsStatus = 'closed' | 'connecting' | 'open' | 'reconnecting'

type FrameHandler = (frame: WsFrame) => void
type StatusHandler = (status: WsStatus) => void

const HEARTBEAT_MS = 25_000
const BACKOFF_MIN_MS = 1_000
const BACKOFF_MAX_MS = 15_000
/** A pong (or any inbound frame) proves the link is alive — reset the watchdog. */
const STALL_TIMEOUT_MS = 60_000

export class TaskSocket {
  private ws: WebSocket | null = null
  private url: string | null = null
  private backoff = BACKOFF_MIN_MS
  private reconnectTimer: number | null = null
  private heartbeatTimer: number | null = null
  private stallTimer: number | null = null
  private attempts = 0
  private closedByUser = false

  private readonly handlers = new Set<FrameHandler>()
  private readonly statusHandlers = new Set<StatusHandler>()
  private readonly subscriptions = new Set<number>()
  private _status: WsStatus = 'closed'

  get status(): WsStatus {
    return this._status
  }

  onFrame(fn: FrameHandler): () => void {
    this.handlers.add(fn)
    return () => this.handlers.delete(fn)
  }

  onStatus(fn: StatusHandler): () => void {
    this.statusHandlers.add(fn)
    fn(this._status)
    return () => this.statusHandlers.delete(fn)
  }

  private setStatus(s: WsStatus): void {
    if (this._status === s) return
    this._status = s
    this.statusHandlers.forEach((fn) => fn(s))
  }

  private buildUrl(): string | null {
    const token = getToken()
    if (!token) return null
    // Same-origin http(s) → ws(s); dev server proxies /api including WS.
    if (typeof location === 'undefined') return null
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
    return `${proto}//${location.host}${API_BASE}/ws?token=${encodeURIComponent(token)}`
  }

  connect(): void {
    this.closedByUser = false
    const url = this.buildUrl()
    if (!url) {
      this.setStatus('closed')
      return
    }
    if (this.ws && (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)) {
      return
    }
    this.url = url
    this.setStatus(this.attempts > 0 ? 'reconnecting' : 'connecting')

    let ws: WebSocket
    try {
      ws = new WebSocket(url)
    } catch {
      this.scheduleReconnect()
      return
    }
    this.ws = ws

    ws.onopen = () => {
      this.attempts = 0
      this.backoff = BACKOFF_MIN_MS
      this.setStatus('open')
      // Re-assert subscriptions that survived the disconnect.
      this.subscriptions.forEach((id) => this.send({ action: 'subscribe', task_id: id }))
      this.startHeartbeat()
    }

    ws.onmessage = (ev: MessageEvent<string>) => {
      this.armStallWatchdog()
      let frame: WsFrame
      try {
        frame = JSON.parse(ev.data) as WsFrame
      } catch {
        return
      }
      if (!frame || typeof frame.type !== 'string') return
      // Server-initiated keepalive: reply so its own watchdog stays happy.
      if (frame.type === 'ping') {
        this.send({ action: 'ping' })
        return
      }
      if (frame.type === 'pong') return
      this.handlers.forEach((fn) => fn(frame))
    }

    ws.onerror = () => {
      // `onclose` always follows; reconnect is handled there.
    }

    ws.onclose = () => {
      this.stopTimers()
      this.ws = null
      if (this.closedByUser) {
        this.setStatus('closed')
        return
      }
      this.scheduleReconnect()
    }
  }

  private scheduleReconnect(): void {
    if (this.reconnectTimer !== null) return
    this.setStatus('reconnecting')
    this.attempts += 1
    const delay = Math.min(this.backoff, BACKOFF_MAX_MS)
    this.backoff = Math.min(this.backoff * 2, BACKOFF_MAX_MS)
    this.reconnectTimer = window.setTimeout(() => {
      this.reconnectTimer = null
      this.connect()
    }, delay)
  }

  private startHeartbeat(): void {
    this.stopTimers()
    this.send({ action: 'ping' })
    this.heartbeatTimer = window.setInterval(() => this.send({ action: 'ping' }), HEARTBEAT_MS)
    this.armStallWatchdog()
  }

  private armStallWatchdog(): void {
    if (this.stallTimer !== null) window.clearTimeout(this.stallTimer)
    this.stallTimer = window.setTimeout(() => {
      // No traffic at all for 60s: the socket is half-open, force a reconnect.
      this.ws?.close()
    }, STALL_TIMEOUT_MS)
  }

  private stopTimers(): void {
    if (this.heartbeatTimer !== null) {
      window.clearInterval(this.heartbeatTimer)
      this.heartbeatTimer = null
    }
    if (this.stallTimer !== null) {
      window.clearTimeout(this.stallTimer)
      this.stallTimer = null
    }
  }

  private send(payload: Record<string, unknown>): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(payload))
    }
  }

  subscribe(taskId: number): void {
    this.subscriptions.add(taskId)
    this.send({ action: 'subscribe', task_id: taskId })
  }

  unsubscribe(taskId: number): void {
    this.subscriptions.delete(taskId)
    this.send({ action: 'unsubscribe', task_id: taskId })
  }

  /** Force an immediate reconnect (used by the "retry" button in the topbar). */
  reconnect(): void {
    this.attempts = 0
    this.backoff = BACKOFF_MIN_MS
    if (this.reconnectTimer !== null) {
      window.clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
    this.close()
    this.connect()
  }

  close(): void {
    this.closedByUser = true
    this.subscriptions.clear()
    this.stopTimers()
    if (this.reconnectTimer !== null) {
      window.clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
    this.ws?.close()
    this.ws = null
    this.setStatus('closed')
  }
}

/** App-wide singleton — one connection, many subscribers. */
export const socket = new TaskSocket()
