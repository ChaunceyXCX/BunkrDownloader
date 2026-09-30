<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AuthLayout from '@/layouts/AuthLayout.vue'
import { useAuthStore } from '@/stores/auth'
import { useI18nStore } from '@/stores/i18n'
import { ApiError } from '@/api/client'
import BaseInput from '@/components/ui/BaseInput.vue'
import BaseButton from '@/components/ui/BaseButton.vue'
import BaseCheckbox from '@/components/ui/BaseCheckbox.vue'
import BaseTabs from '@/components/ui/BaseTabs.vue'
import Icon from '@/components/ui/Icon.vue'
import type { IconName } from '@/components/ui/Icon.vue'

const auth = useAuthStore()
const i18n = useI18nStore()
const route = useRoute()
const router = useRouter()

const mode = ref<'login' | 'register'>('login')
const busy = ref(false)
const showPw = ref(false)
const remember = ref(true)
const submitError = ref('')

const login = reactive({ account: '', password: '' })
const reg = reactive({ username: '', email: '', password: '', confirm: '' })

/* ---------------- validation ---------------- */
const pwHint = computed(() => {
  const p = reg.password
  if (p.length === 0) return ''
  if (p.length < 6) return { text: i18n.t('auth.err.passwordShort'), tone: 'warn' }
  const score = (p.match(/[A-Z]/) ? 1 : 0) + (p.match(/[0-9]/) ? 1 : 0) + (p.match(/[^A-Za-z0-9]/) ? 1 : 0)
  if (p.length >= 10 && score >= 2) return { text: i18n.t('auth.strengthStrong'), tone: 'success' }
  if (p.length >= 7 && score >= 1) return { text: i18n.t('auth.strengthMedium'), tone: 'accent' }
  return { text: i18n.t('auth.strengthWeak'), tone: 'warn' }
})

const USERNAME_RE = /^[A-Za-z0-9_]+$/
const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/

const loginValid = computed(() => login.account.trim() !== '' && login.password.length > 0)
const regValid = computed(() => {
  const u = reg.username.trim()
  const e = reg.email.trim()
  return (
    u.length >= 3 &&
    u.length <= 24 &&
    USERNAME_RE.test(u) &&
    EMAIL_RE.test(e) &&
    reg.password.length >= 6 &&
    reg.confirm === reg.password
  )
})

function switchMode(next: 'login' | 'register'): void {
  mode.value = next
  submitError.value = ''
}

async function submit(): Promise<void> {
  if (mode.value === 'login') await doLogin()
  else await doRegister()
}

async function doLogin(): Promise<void> {
  if (!loginValid.value || busy.value) return
  busy.value = true
  submitError.value = ''
  try {
    await auth.login({ account: login.account.trim(), password: login.password })
    if (!remember.value) localStorage.removeItem('bunkr_token')
    const redirect = (route.query.redirect as string | undefined) ?? '/app'
    await router.replace(redirect.startsWith('/') ? redirect : '/app')
  } catch (e) {
    submitError.value = e instanceof ApiError ? e.message : i18n.t('error.generic')
  } finally {
    busy.value = false
  }
}

async function doRegister(): Promise<void> {
  if (!regValid.value || busy.value) return
  busy.value = true
  submitError.value = ''
  try {
    await auth.register({
      username: reg.username.trim(),
      email: reg.email.trim(),
      password: reg.password,
    })
    await router.replace('/app')
  } catch (e) {
    submitError.value = e instanceof ApiError ? e.message : i18n.t('error.generic')
  } finally {
    busy.value = false
  }
}

/* ---------------- remember-me keeps token only in-memory when unchecked ---- */

const passwordIcon = computed<IconName>(() => (showPw.value ? 'eye-off' : 'eye'))
</script>

<template>
  <AuthLayout>
    <div class="bd-auth">
      <BaseTabs
        :model-value="mode"
        :items="[
          { value: 'login', label: i18n.t('auth.login') },
          { value: 'register', label: i18n.t('auth.register') },
        ]"
        @update:model-value="(v) => switchMode(v as 'login' | 'register')"
      />

      <p class="bd-auth__subtitle">
        {{ mode === 'login' ? i18n.t('auth.loginSubtitle') : i18n.t('auth.registerSubtitle') }}
      </p>

      <form class="bd-auth__form" @submit.prevent="submit">
        <!-- register fields -->
        <template v-if="mode === 'register'">
          <BaseInput
            v-model="reg.username"
            :label="i18n.t('auth.username')"
            :placeholder="i18n.t('auth.usernamePlaceholder')"
            autocomplete="username"
            :error="reg.username && (reg.username.length < 3 || reg.username.length > 24 || !USERNAME_RE.test(reg.username)) ? i18n.t(
              reg.username.length < 3 ? 'auth.err.usernameShort' : !USERNAME_RE.test(reg.username) ? 'auth.err.usernamePattern' : 'auth.err.usernameLong'
            ) : undefined"
          />
          <BaseInput
            v-model="reg.email"
            type="email"
            :label="i18n.t('auth.email')"
            :placeholder="i18n.t('auth.emailPlaceholder')"
            autocomplete="email"
            :error="reg.email && !EMAIL_RE.test(reg.email) ? i18n.t('auth.err.emailInvalid') : undefined"
          />
          <BaseInput
            v-model="reg.password"
            :type="showPw ? 'text' : 'password'"
            :label="i18n.t('auth.password')"
            :hint="pwHint ? pwHint.text : undefined"
            autocomplete="new-password"
          >
            <template #suffix>
              <button
                type="button"
                class="bd-auth__eye"
                :aria-label="showPw ? i18n.t('auth.hidePassword') : i18n.t('auth.showPassword')"
                @click="showPw = !showPw"
              >
                <Icon :name="passwordIcon" :size="16" />
              </button>
            </template>
          </BaseInput>
          <BaseInput
            v-model="reg.confirm"
            :type="showPw ? 'text' : 'password'"
            :label="i18n.t('auth.confirmPassword')"
            :error="reg.confirm && reg.confirm !== reg.password ? i18n.t('auth.err.confirmMismatch') : undefined"
          />
        </template>

        <!-- login fields -->
        <template v-else>
          <BaseInput
            v-model="login.account"
            type="text"
            :label="i18n.t('auth.account')"
            :placeholder="i18n.t('auth.accountPlaceholder')"
            autocomplete="username"
            icon="user"
          />
          <BaseInput
            v-model="login.password"
            :type="showPw ? 'text' : 'password'"
            :label="i18n.t('auth.password')"
            autocomplete="current-password"
            @enter="submit"
          >
            <template #suffix>
              <button
                type="button"
                class="bd-auth__eye"
                :aria-label="showPw ? i18n.t('auth.hidePassword') : i18n.t('auth.showPassword')"
                @click="showPw = !showPw"
              >
                <Icon :name="passwordIcon" :size="16" />
              </button>
            </template>
          </BaseInput>
        </template>

        <div v-if="mode === 'login'" class="bd-auth__row">
          <BaseCheckbox v-model="remember" :label="i18n.t('auth.rememberMe')" />
        </div>

        <p v-if="submitError" class="bd-auth__error" role="alert">
          {{ submitError }}
        </p>

        <BaseButton
          variant="primary"
          type="submit"
          block
          size="lg"
          :loading="busy"
          :disabled="mode === 'login' ? !loginValid : !regValid"
          :label="busy ? (mode === 'login' ? i18n.t('auth.loggingIn') : i18n.t('auth.registering')) : (mode === 'login' ? i18n.t('auth.login') : i18n.t('auth.register'))"
        >
          {{ busy ? (mode === 'login' ? i18n.t('auth.loggingIn') : i18n.t('auth.registering')) : (mode === 'login' ? i18n.t('auth.login') : i18n.t('auth.register')) }}
        </BaseButton>
      </form>

      <div class="bd-auth__switch">
        <span>{{ mode === 'login' ? i18n.t('auth.haveAccount') : i18n.t('auth.noAccount') }}</span>
        <button type="button" class="bd-auth__link" @click="switchMode(mode === 'login' ? 'register' : 'login')">
          {{ mode === 'login' ? i18n.t('auth.toRegister') : i18n.t('auth.toLogin') }}
        </button>
      </div>

      <button type="button" class="bd-auth__plans" @click="switchMode('register')">
        <Icon name="crown" :size="13" />
        {{ i18n.t('auth.viewPlans') }}
      </button>
    </div>
  </AuthLayout>
</template>

<style scoped>
.bd-auth {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.bd-auth__subtitle {
  margin: -6px 0 0;
  font-size: 12.5px;
  color: var(--app-text-muted);
}
.bd-auth__form {
  display: flex;
  flex-direction: column;
  gap: 13px;
}
.bd-auth__row {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.bd-auth__eye {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  border: 0;
  border-radius: 7px;
  background: transparent;
  color: var(--app-text-dim);
  cursor: pointer;
}
.bd-auth__eye:hover {
  color: var(--app-text);
  background-color: var(--app-surface-hover);
}
.bd-auth__error {
  margin: 0;
  padding: 8px 11px;
  border-radius: 8px;
  font-size: 12px;
  color: var(--app-danger);
  background-color: color-mix(in srgb, var(--app-danger) 9%, transparent);
}
.bd-auth__switch {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  font-size: 12.5px;
  color: var(--app-text-muted);
}
.bd-auth__link {
  border: 0;
  background: none;
  padding: 0;
  color: var(--app-accent);
  font-size: 12.5px;
  font-weight: 600;
  cursor: pointer;
}
.bd-auth__link:hover {
  text-decoration: underline;
}
.bd-auth__plans {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  align-self: center;
  border: 0;
  background: none;
  color: var(--app-text-dim);
  font-size: 12px;
  cursor: pointer;
}
.bd-auth__plans:hover {
  color: var(--app-accent);
}
</style>
