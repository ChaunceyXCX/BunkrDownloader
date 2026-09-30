import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import {
  ApiError,
  authApi,
  getToken,
  setToken,
} from '@/api/client'
import type { ChangePasswordReq, Quota, RegisterReq, LoginReq, User } from '@/api/types'
import { navigate } from '@/router/bridge'
import { useI18nStore } from './i18n'
import { useToastStore } from './toast'

export const useAuthStore = defineStore('auth', () => {
  const user = ref<User | null>(null)
  const quota = ref<Quota | null>(null)
  const token = ref<string | null>(getToken())
  /** True until the first session check finishes (avoids a login flash). */
  const bootstrapped = ref(false)

  const isAuthenticated = computed(() => Boolean(token.value))
  const isMember = computed(() => Boolean(quota.value?.is_member ?? user.value?.is_member))
  const plan = computed(() => quota.value?.plan ?? user.value?.plan ?? 'free')

  const linksUsed = computed(() => quota.value?.links_used ?? 0)
  const linksLimit = computed(() => quota.value?.links_limit ?? 0)
  const linksUnlimited = computed(() => Boolean(quota.value?.links_unlimited) || linksLimit.value < 0)
  const filesUsed = computed(() => quota.value?.files_used ?? 0)
  const filesLimit = computed(() => quota.value?.files_limit ?? 0)
  const filesUnlimited = computed(() => Boolean(quota.value?.files_unlimited) || filesLimit.value < 0)
  const concurrentLimit = computed(() => quota.value?.concurrent_limit ?? 0)
  const concurrentRunning = computed(() => quota.value?.concurrent_running ?? 0)

  /** 0–100, or 0 when unlimited/unknown. */
  const quotaPercent = computed<number>(() =>
    percent(linksUsed.value, linksLimit.value, linksUnlimited.value),
  )
  const filesPercent = computed<number>(() =>
    percent(filesUsed.value, filesLimit.value, filesUnlimited.value),
  )

  function percent(used: number, limit: number, unlimited: boolean): number {
    if (unlimited || limit <= 0) return 0
    return Math.max(0, Math.min(100, (used / limit) * 100))
  }

  function applySession(nextUser: User, nextQuota: Quota, nextToken?: string): void {
    user.value = nextUser
    quota.value = nextQuota
    if (nextToken) {
      setToken(nextToken)
      token.value = nextToken
    }
  }

  /** Optimistic merge used by the `quota` / `hello` WebSocket frames. */
  function setQuota(next: Quota): void {
    quota.value = next
    if (user.value) user.value = { ...user.value, is_member: next.is_member, plan: next.plan }
  }

  function setUser(next: User): void {
    user.value = next
  }

  function clearSession(): void {
    setToken(null)
    token.value = null
    user.value = null
    quota.value = null
  }

  async function register(body: RegisterReq): Promise<User> {
    const res = await authApi.register(body)
    applySession(res.user, res.quota, res.token)
    return res.user
  }

  async function login(body: LoginReq): Promise<User> {
    const res = await authApi.login(body)
    applySession(res.user, res.quota, res.token)
    return res.user
  }

  async function logout(): Promise<void> {
    try {
      await authApi.logout()
    } catch (e) {
      // The endpoint is best-effort; the client always drops the token.
      if (!(e instanceof ApiError)) throw e
    } finally {
      clearSession()
      navigate('/login')
    }
  }

  /** `GET /auth/me` — also the boot-time session restore. */
  async function fetchMe(): Promise<boolean> {
    if (!getToken()) {
      clearSession()
      bootstrapped.value = true
      return false
    }
    try {
      const res = await authApi.me()
      applySession(res.user, res.quota, res.token)
      token.value = getToken()
      return true
    } catch (e) {
      if (e instanceof ApiError && e.isUnauthorized) clearSession()
      return false
    } finally {
      bootstrapped.value = true
    }
  }

  async function changePassword(body: ChangePasswordReq): Promise<void> {
    await authApi.changePassword(body)
    useToastStore().push('success', useI18nStore().t('account.passwordChanged'))
  }

  return {
    user,
    quota,
    token,
    bootstrapped,
    isAuthenticated,
    isMember,
    plan,
    linksUsed,
    linksLimit,
    linksUnlimited,
    filesUsed,
    filesLimit,
    filesUnlimited,
    concurrentLimit,
    concurrentRunning,
    quotaPercent,
    filesPercent,
    register,
    login,
    logout,
    fetchMe,
    changePassword,
    setQuota,
    setUser,
    clearSession,
  }
})
