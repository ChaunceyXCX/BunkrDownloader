import { defineStore } from 'pinia'
import { computed, reactive, ref } from 'vue'
import { systemApi } from '@/api/client'
import type { Aria2Info, AppSettings } from '@/api/types'

const STORAGE_KEY = 'bunkr_settings'

/** Client-side preferences only — the server keeps no per-user config. */
export interface LocalSettings {
  /** aria2 `split` per download. */
  connections: number
  /** Retries per file before it is marked failed. */
  maxRetries: number
  /** 0 = unlimited, otherwise kbps. */
  rateLimitKbps: number
  /** Extensions never downloaded, e.g. ['.tmp']. */
  ignore: string[]
  /** When non-empty, only these extensions are downloaded. */
  include: string[]
  /** false → `no_album_folder` when creating a task. */
  albumFolder: boolean
  /** `clean_name` when creating a task. */
  cleanName: boolean
  /** Default `auto_start` of NewTaskModal. */
  autoStart: boolean
  /** Reveal the destination folder once a task completes. */
  openFolder: boolean
}

export const DEFAULT_SETTINGS: LocalSettings = {
  connections: 4,
  maxRetries: 5,
  rateLimitKbps: 0,
  ignore: [],
  include: [],
  albumFolder: true,
  cleanName: false,
  autoStart: true,
  openFolder: false,
}

function sanitize(raw: unknown): LocalSettings {
  const out: LocalSettings = { ...DEFAULT_SETTINGS }
  if (!raw || typeof raw !== 'object') return out
  const r = raw as Record<string, unknown>
  const num = (v: unknown, min: number, max: number, fallback: number): number => {
    const n = typeof v === 'number' ? v : Number(v)
    if (!Number.isFinite(n)) return fallback
    return Math.min(max, Math.max(min, Math.trunc(n)))
  }
  const list = (v: unknown): string[] =>
    Array.isArray(v)
      ? v.filter((x): x is string => typeof x === 'string' && x.trim() !== '').map((x) => x.trim())
      : []
  const bool = (v: unknown, fallback: boolean): boolean => (typeof v === 'boolean' ? v : fallback)

  out.connections = num(r.connections, 1, 16, DEFAULT_SETTINGS.connections)
  out.maxRetries = num(r.maxRetries, 0, 20, DEFAULT_SETTINGS.maxRetries)
  out.rateLimitKbps = num(r.rateLimitKbps, 0, 1_000_000, DEFAULT_SETTINGS.rateLimitKbps)
  out.ignore = list(r.ignore)
  out.include = list(r.include)
  out.albumFolder = bool(r.albumFolder, DEFAULT_SETTINGS.albumFolder)
  out.cleanName = bool(r.cleanName, DEFAULT_SETTINGS.cleanName)
  out.autoStart = bool(r.autoStart, DEFAULT_SETTINGS.autoStart)
  out.openFolder = bool(r.openFolder, DEFAULT_SETTINGS.openFolder)
  return out
}

/** `.TMP, part` → `['.tmp', 'part']` — the textarea form of the extension lists. */
export function parseExtensionList(value: string): string[] {
  return value
    .split(/[\s,;]+/)
    .map((s) => s.trim())
    .filter((s) => s !== '')
    .map((s) => (s.startsWith('.') ? s : `.${s}`).toLowerCase())
}

export function formatExtensionList(list: string[]): string {
  return list.join(', ')
}

export const useSettingsStore = defineStore('settings', () => {
  const prefs = reactive<LocalSettings>({ ...DEFAULT_SETTINGS })
  const version = ref('')
  const downloadDir = ref('')
  const aria2 = ref<Aria2Info>({ available: false, version: '' })
  const isDesktop = ref(false)
  const runtimeLoaded = ref(false)
  const runtimeLoading = ref(false)

  function hydrate(): void {
    try {
      const raw = localStorage.getItem(STORAGE_KEY)
      if (raw) Object.assign(prefs, sanitize(JSON.parse(raw)))
      else Object.assign(prefs, { ...DEFAULT_SETTINGS })
    } catch {
      Object.assign(prefs, { ...DEFAULT_SETTINGS })
    }
  }

  function persist(): void {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify({ ...prefs }))
    } catch {
      /* ignore */
    }
  }

  /** Pull the server-owned runtime facts (version, aria2, download dir). */
  async function loadRuntime(): Promise<void> {
    runtimeLoading.value = true
    try {
      const [health, serverSettings] = await Promise.allSettled([
        systemApi.health(),
        systemApi.settings(),
      ])
      if (health.status === 'fulfilled') {
        version.value = health.value.version
        aria2.value = health.value.aria2
      }
      if (serverSettings.status === 'fulfilled') {
        downloadDir.value = serverSettings.value.download_dir
        isDesktop.value = Boolean(serverSettings.value.features?.desktop)
        if (!version.value) version.value = serverSettings.value.version
      }
      runtimeLoaded.value = true
    } finally {
      runtimeLoading.value = false
    }
  }

  function reset(): void {
    Object.assign(prefs, { ...DEFAULT_SETTINGS })
    persist()
  }

  function clearLocal(): void {
    try {
      localStorage.removeItem(STORAGE_KEY)
    } catch {
      /* ignore */
    }
    Object.assign(prefs, { ...DEFAULT_SETTINGS })
  }

  const ignoreText = computed({
    get: () => formatExtensionList(prefs.ignore),
    set: (v: string) => {
      prefs.ignore = parseExtensionList(v)
    },
  })
  const includeText = computed({
    get: () => formatExtensionList(prefs.include),
    set: (v: string) => {
      prefs.include = parseExtensionList(v)
    },
  })

  return {
    prefs,
    version,
    downloadDir,
    aria2,
    isDesktop,
    runtimeLoaded,
    runtimeLoading,
    ignoreText,
    includeText,
    hydrate,
    persist,
    loadRuntime,
    reset,
    clearLocal,
  }
})

export type SettingsStore = ReturnType<typeof useSettingsStore>

/** Local-only, never cached from the server — the settings endpoint has no such field. */
export type ServerSettings = AppSettings
