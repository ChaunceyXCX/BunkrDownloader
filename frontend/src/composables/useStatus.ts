import type { FileStatus, OrderStatus, TaskStatus } from '@/api/types'
import { useI18nStore } from '@/stores/i18n'

export type Tone = 'neutral' | 'info' | 'success' | 'warn' | 'danger' | 'accent' | 'muted'

/** Tones reused by BaseBadge, StatCard accents and the connection dot. */
export const TONE_COLOR: Record<Tone, string> = {
  neutral: 'var(--app-text-muted)',
  info: 'var(--app-info)',
  success: 'var(--app-success)',
  warn: 'var(--app-warn)',
  danger: 'var(--app-danger)',
  accent: 'var(--app-accent)',
  muted: 'var(--app-text-dim)',
}

/**
 * Status → tone map covering every enumerated status of docs/API.md §1.
 * `pending`/`canceled` appear in both task and order enums with the same tone.
 */
export const STATUS_TONE: Record<string, Tone> = {
  pending: 'info',
  crawling: 'accent',
  running: 'accent',
  paused: 'warn',
  completed: 'success',
  failed: 'danger',
  canceled: 'muted',
  refunded: 'muted',
  skipped: 'muted',
}

export function statusTone(status: string): Tone {
  return STATUS_TONE[status] ?? 'neutral'
}

/** i18n labels with a graceful fallback to a generic "unknown" string. */
export function useStatusLabel() {
  const i18n = useI18nStore()
  const pick = (prefix: 'task' | 'file' | 'membership', key: string, fallback: string): string => {
    const full = `${prefix}.status.${key}`
    return i18n.t(full) === full ? i18n.t(fallback) : i18n.t(full)
  }
  return {
    taskLabel: (s: TaskStatus | string) => pick('task', s, 'task.statusUnknown'),
    fileLabel: (s: FileStatus | string) => pick('file', s, 'file.statusUnknown'),
    orderLabel: (s: OrderStatus | string) => pick('membership', s, 'common.unknown'),
    tone: statusTone,
  }
}
