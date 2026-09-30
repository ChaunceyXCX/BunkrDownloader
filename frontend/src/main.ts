import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { watch } from 'vue'
import App from './App.vue'
import router from './router'
import { getToken, onGlobalErrorHandler, onUnauthorizedHandler } from './api/client'
import { socket } from './api/ws'
import { useThemeStore } from './stores/theme'
import { useI18nStore } from './stores/i18n'
import { useToastStore } from './stores/toast'
import { useAuthStore } from './stores/auth'
import { useTasksStore } from './stores/tasks'
import { useSettingsStore } from './stores/settings'
import './style.css'

const pinia = createPinia()
const app = createApp(App)
app.use(pinia)
app.use(router)

/* ---- synchronous, pre-paint: theme, locale and persisted prefs ---- */
useThemeStore(pinia).init()
useI18nStore(pinia).init()
useSettingsStore(pinia).hydrate()

app.mount('#app')

/* ---- session restore ---- */
async function restore(): Promise<void> {
  const auth = useAuthStore(pinia)
  const i18n = useI18nStore(pinia)
  const toast = useToastStore(pinia)
  const tasks = useTasksStore(pinia)

  // Wire global error handlers once.
  onUnauthorizedHandler(() => {
    auth.clearSession()
    if (router.currentRoute.value.meta.public) return
    void router.replace({
      name: 'login',
      query: { redirect: router.currentRoute.value.fullPath },
    })
  })

  onGlobalErrorHandler((err) => {
    const fallback = err.isUnauthorized ? 'error.unauthorized' : 'error.generic'
    const key = `error.${err.code}`
    const title = i18n.t(key) === key ? i18n.t(fallback) : i18n.t(key)
    toast.push('error', title, err.status ? err.message : i18n.t('error.network'))
  })

  const hasToken = Boolean(getToken())
  if (hasToken) {
    await auth.fetchMe()
    void tasks.fetchTasks().catch(() => {
      /* boot seed is best-effort; WS fills the gap */
    })
    tasks.ensureSocketSubscription()
    if (auth.isAuthenticated) socket.connect()
  } else {
    auth.bootstrapped = true
  }

  // Connect / tear down the socket whenever the session changes at runtime
  // (login, logout, 401 invalidation).
  watch(
    () => auth.token,
    (t) => {
      tasks.ensureSocketSubscription()
      if (t) socket.connect()
      else socket.close()
    },
  )
}

// The router guard only needs a token to decide the first screen; restore()
// fills user/quota/global-stats in the background afterwards.
void restore()
