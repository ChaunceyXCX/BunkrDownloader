import { Events } from '@wailsio/runtime'
import { getToken, systemApi } from './client'
import type { WsFrame, WsStatsData } from './types'

/**
 * Live task stream for the desktop build.
 *
 * `main.go` bridges the download hub onto Wails events: every frame is emitted
 * as `bunkr:<frame.Type>` with the payload `{ type, ts, data, user_id, task_id }`.
 * This client subscribes to those event names and re-publishes them to the app
 * exactly like the previous `GET /api/ws` socket did, so `stores/tasks.ts` and
 * the components are unchanged.
 *
 * The transport is in-process: there is no handshake, no heartbeat and no
 * reconnect logic — `connect()` subscribes, `close()` unsubscribes.
 */
export type WsStatus = 'closed' | 'connecting' | 'open' | 'reconnecting'

type FrameHandler = (frame: WsFrame) => void
type StatusHandler = (status: WsStatus) => void

/** Every frame type the web build handled over the socket. */
const FRAME_TYPES = [
  'hello',
  'task_created',
  'task_updated',
  'task_progress',
  'task_completed',
  'file_created',
  'file_progress',
  'file_updated',
  'log',
  'quota',
  'stats',
  'auth',
  'pong',
  'error',
] as const

/**
 * Frames that only belong to the task currently open in the file/log panes —
 * the server-side WebSocket delivered them for subscribed tasks only, so the
 * same filter is applied here.
 */
const TASK_SCOPED = new Set<string>(['file_created', 'file_progress', 'file_updated', 'log'])

/** Shape published by `main.go` for every `bunkr:*` event. */
interface WailsFrame {
  type?: string
  ts?: number
  data?: unknown
  user_id?: number
  task_id?: number
}

function nowSec(): number {
  return Math.floor(Date.now() / 1000)
}

export class TaskSocket {
  private disposers: (() => void)[] = []
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

  connect(): void {
    if (this.disposers.length > 0) return
    if (!getToken()) {
      // Nothing to listen to before sign-in; main.ts connects again once the
      // session is restored.
      this.setStatus('closed')
      return
    }
    this.setStatus('open')
    for (const type of FRAME_TYPES) {
      this.disposers.push(
        Events.On(`bunkr:${type}`, (ev) => this.dispatch(type, (ev.data ?? {}) as WailsFrame)),
      )
    }
    void this.seedStats()
  }

  /**
   * The web build pushed a `hello` frame right after the handshake so the
   * dashboard had counters before the first poll. The download hub only
   * broadcasts `stats` while something is running, so the initial snapshot is
   * fetched once here and published as the very first `stats` frame. User and
   * quota already come from `authApi.me()` during boot.
   */
  private async seedStats(): Promise<void> {
    try {
      const stats = await systemApi.stats()
      this.dispatch('stats', {
        type: 'stats',
        ts: nowSec(),
        data: { stats } satisfies WsStatsData,
      })
    } catch {
      /* best effort — a 401 already routes through the session handlers */
    }
  }

  private dispatch(eventType: string, raw: WailsFrame): void {
    if (eventType === 'pong') return
    const type = typeof raw.type === 'string' ? raw.type : eventType
    const taskId = Number(raw.task_id ?? 0)
    if (TASK_SCOPED.has(type) && taskId !== 0 && !this.subscriptions.has(taskId)) return
    const frame: WsFrame = {
      type,
      ts: typeof raw.ts === 'number' ? raw.ts : nowSec(),
      data: raw.data,
    }
    this.handlers.forEach((fn) => fn(frame))
  }

  subscribe(taskId: number): void {
    this.subscriptions.add(taskId)
  }

  unsubscribe(taskId: number): void {
    this.subscriptions.delete(taskId)
  }

  /** Used by the "retry" button in the topbar — re-binds every event handler. */
  reconnect(): void {
    const active = new Set(this.subscriptions)
    this.close()
    this.subscriptions.clear()
    active.forEach((id) => this.subscriptions.add(id))
    this.connect()
  }

  close(): void {
    this.disposers.forEach((dispose) => dispose())
    this.disposers = []
    this.subscriptions.clear()
    this.setStatus('closed')
  }
}

/** App-wide singleton — one subscription set, many subscribers. */
export const socket = new TaskSocket()
