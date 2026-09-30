/**
 * Display formatters. Every function is total: unknown/zero input yields `—`
 * so the UI never shows `NaN`, `0 B` or an empty cell.
 */
const EM_DASH = '—'

const BYTE_UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'] as const
const num0 = new Intl.NumberFormat('en-US', { maximumFractionDigits: 0 })
const num1 = new Intl.NumberFormat('en-US', { maximumFractionDigits: 1 })

function isNum(v: unknown): v is number {
  return typeof v === 'number' && Number.isFinite(v)
}

/** 1073741824 → `1 GB` (1024-based, max 1 decimal, trailing `.0` trimmed). */
export function formatBytes(bytes: number | null | undefined, perSecond = false): string {
  if (!isNum(bytes) || bytes <= 0) return EM_DASH
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < BYTE_UNITS.length - 1) {
    value /= 1024
    unit += 1
  }
  const text = value >= 100 || unit === 0 ? num0.format(Math.round(value)) : num1.format(value)
  return `${text} ${BYTE_UNITS[unit]}${perSecond ? '/s' : ''}`
}

/** Alias that reads better at call sites dealing with transfer rates. */
export function formatSpeed(bytesPerSecond: number | null | undefined): string {
  return formatBytes(bytesPerSecond, true)
}

/** 25 → `25%`; out-of-range or non-finite → `—`. */
export function formatPercent(value: number | null | undefined, digits = 0): string {
  if (!isNum(value) || value < 0) return EM_DASH
  const clamped = Math.min(100, value)
  const d = Math.max(0, Math.min(3, digits))
  const text = d === 0 ? num0.format(Math.round(clamped)) : new Intl.NumberFormat('en-US', {
    maximumFractionDigits: d,
    minimumFractionDigits: d,
  }).format(clamped)
  return `${text}%`
}

/** 3723 → `1h 2m 3s`; sub-second → `0s`. */
export function formatDuration(seconds: number | null | undefined): string {
  if (!isNum(seconds) || seconds < 0) return EM_DASH
  const total = Math.round(seconds)
  if (total === 0) return EM_DASH
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  const s = total % 60
  if (h > 0) return `${h}h ${m}m ${s}s`
  if (m > 0) return `${m}m ${s}s`
  return `${s}s`
}

/** Compact relative time; the formatter language follows the active locale. */
export function formatRelativeTime(iso: string | null | undefined, locale = 'zh-CN'): string {
  if (!iso) return EM_DASH
  const then = new Date(iso).getTime()
  if (!Number.isFinite(then)) return EM_DASH
  const diffSec = Math.round((Date.now() - then) / 1000)
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' })
  const abs = Math.abs(diffSec)
  if (abs < 45) return rtf.format(-diffSec, 'second')
  if (abs < 3600) return rtf.format(-diffSec, 'minute')
  if (abs < 86400) return rtf.format(-diffSec, 'hour')
  if (abs < 2592000) return rtf.format(-diffSec, 'day')
  if (abs < 31536000) return rtf.format(-diffSec, 'month')
  return rtf.format(-diffSec, 'year')
}

/** 1200 → `1.2k`. */
export function formatCount(n: number | null | undefined): string {
  if (!isNum(n) || n < 0) return EM_DASH
  if (n < 1000) return num0.format(n)
  if (n < 1_000_000) {
    const v = n / 1000
    return `${v >= 100 ? num0.format(Math.round(v)) : num1.format(v)}k`
  }
  return `${num1.format(n / 1_000_000)}M`
}

/** 990 cents / CNY → `¥9.90`; unknown currency → `9.90 XXX`. */
export function formatPrice(cents: number | null | undefined, currency = 'CNY'): string {
  if (!isNum(cents)) return EM_DASH
  const amount = cents / 100
  const frac = new Intl.NumberFormat('en-US', {
    minimumFractionDigits: amount % 1 === 0 ? 0 : 2,
    maximumFractionDigits: 2,
  }).format(amount)
  try {
    return new Intl.NumberFormat('zh-CN', {
      style: 'currency',
      currency,
      currencyDisplay: 'narrowSymbol',
    }).format(amount)
  } catch {
    return `${frac} ${currency}`
  }
}

function localeTag(locale: string): string {
  return locale === 'en' ? 'en-US' : locale
}

/** `2024-05-01 12:00:00` in the active locale. */
export function formatDateTime(iso: string | null | undefined, locale = 'zh-CN'): string {
  if (!iso) return EM_DASH
  const d = new Date(iso)
  if (!Number.isFinite(d.getTime())) return EM_DASH
  return new Intl.DateTimeFormat(localeTag(locale), {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  }).format(d)
}

/** Estimated seconds left from remaining bytes + speed, or `null`. */
export function etaSeconds(remainingBytes: number, speed: number): number | null {
  if (!isNum(remainingBytes) || !isNum(speed) || speed <= 0 || remainingBytes <= 0) return null
  return remainingBytes / speed
}

export function useFormat(locale?: () => string) {
  return {
    formatBytes: (b: number | null | undefined, perSecond = false) => formatBytes(b, perSecond),
    formatSpeed,
    formatPercent,
    formatDuration,
    formatCount,
    formatPrice,
    formatDateTime: (iso: string | null | undefined) => formatDateTime(iso, locale?.() ?? 'zh-CN'),
    formatRelativeTime: (iso: string | null | undefined) =>
      formatRelativeTime(iso, locale?.() ?? 'zh-CN'),
    etaSeconds,
    EM_DASH,
  }
}
