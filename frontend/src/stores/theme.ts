import { defineStore } from 'pinia'
import { computed, ref, watch } from 'vue'

export type ThemeMode = 'light' | 'dark' | 'system'
export type ResolvedTheme = 'light' | 'dark'

const STORAGE_KEY = 'bunkr_theme'

function readStoredMode(): ThemeMode {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw === 'light' || raw === 'dark' || raw === 'system') return raw
  } catch {
    /* storage unavailable */
  }
  return 'system'
}

function systemPrefersDark(): boolean {
  return typeof window !== 'undefined' && typeof window.matchMedia === 'function'
    ? window.matchMedia('(prefers-color-scheme: dark)').matches
    : false
}

export const useThemeStore = defineStore('theme', () => {
  const mode = ref<ThemeMode>(readStoredMode())
  const systemDark = ref(systemPrefersDark())

  const resolved = computed<ResolvedTheme>(() =>
    mode.value === 'system' ? (systemDark.value ? 'dark' : 'light') : mode.value,
  )

  function apply(): void {
    if (typeof document === 'undefined') return
    const root = document.documentElement
    root.classList.toggle('dark', resolved.value === 'dark')
    root.classList.toggle('light', resolved.value === 'light')
    root.style.colorScheme = resolved.value
  }

  function setMode(next: ThemeMode): void {
    mode.value = next
    try {
      localStorage.setItem(STORAGE_KEY, next)
    } catch {
      /* ignore */
    }
    apply()
  }

  function cycle(): void {
    const order: ThemeMode[] = ['light', 'dark', 'system']
    setMode(order[(order.indexOf(mode.value) + 1) % order.length])
  }

  /** Call once at boot: reapplies the stored/system value to <html>. */
  function init(): void {
    if (typeof window !== 'undefined' && typeof window.matchMedia === 'function') {
      const mq = window.matchMedia('(prefers-color-scheme: dark)')
      const onChange = (e: MediaQueryListEvent) => {
        systemDark.value = e.matches
      }
      if (typeof mq.addEventListener === 'function') mq.addEventListener('change', onChange)
    }
    apply()
  }

  function toggleFromIcon(): void {
    setMode(resolved.value === 'dark' ? 'light' : 'dark')
  }

  watch(resolved, apply)

  return { mode, resolved, setMode, cycle, toggleFromIcon, init }
})
