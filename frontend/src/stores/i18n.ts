import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import en from '@/locales/en'
import zhCN from '@/locales/zh-CN'

export type Locale = 'zh-CN' | 'en'

const STORAGE_KEY = 'bunkr_locale'
export const DEFAULT_LOCALE: Locale = 'zh-CN'

const TABLES: Record<Locale, Record<string, string>> = {
  'zh-CN': zhCN,
  en,
}

function readStoredLocale(): Locale {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw === 'zh-CN' || raw === 'en') return raw
  } catch {
    /* storage unavailable */
  }
  return DEFAULT_LOCALE
}

/** `{token}` → value; unknown tokens are left untouched so gaps stay visible. */
function interpolate(template: string, params?: Record<string, string | number>): string {
  if (!params) return template
  return template.replace(/\{(\w+)\}/g, (match, key: string) =>
    key in params ? String(params[key]) : match,
  )
}

export const useI18nStore = defineStore('i18n', () => {
  const locale = ref<Locale>(readStoredLocale())

  const table = computed<Record<string, string>>(() => TABLES[locale.value])
  const isZh = computed(() => locale.value === 'zh-CN')

  function t(key: string, params?: Record<string, string | number>): string {
    const template = table.value[key]
    if (template === undefined) {
      if (import.meta.env.DEV) console.warn(`[i18n] missing key: ${key}`)
      return interpolate(key, params)
    }
    return interpolate(template, params)
  }

  function setLocale(next: Locale): void {
    locale.value = next
    try {
      localStorage.setItem(STORAGE_KEY, next)
    } catch {
      /* ignore */
    }
    if (typeof document !== 'undefined') document.documentElement.lang = next
  }

  function toggle(): void {
    setLocale(locale.value === 'zh-CN' ? 'en' : 'zh-CN')
  }

  /** Call once at boot so <html lang> matches the stored preference. */
  function init(): void {
    if (typeof document !== 'undefined') document.documentElement.lang = locale.value
  }

  return { locale, isZh, t, setLocale, toggle, init }
})
