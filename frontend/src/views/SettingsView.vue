<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useThemeStore } from '@/stores/theme'
import type { ThemeMode } from '@/stores/theme'
import { useI18nStore } from '@/stores/i18n'
import type { Locale } from '@/stores/i18n'
import { useSettingsStore, formatExtensionList, parseExtensionList } from '@/stores/settings'
import { useAuthStore } from '@/stores/auth'
import { socket } from '@/api/ws'
import type { WsStatus } from '@/api/ws'
import { useFormat } from '@/composables/useFormat'
import { useToast } from '@/components/ui/useToast'
import Icon from '@/components/ui/Icon.vue'
import BaseSelect from '@/components/ui/BaseSelect.vue'
import BaseInput from '@/components/ui/BaseInput.vue'
import BaseCheckbox from '@/components/ui/BaseCheckbox.vue'
import BaseButton from '@/components/ui/BaseButton.vue'
import BaseModal from '@/components/ui/BaseModal.vue'
import ConnectionIndicator from '@/components/layout/ConnectionIndicator.vue'
import { authApi, ApiError, systemApi } from '@/api/client'

const theme = useThemeStore()
const i18n = useI18nStore()
const settings = useSettingsStore()
const auth = useAuthStore()
const router = useRouter()
const toast = useToast()
const { formatDuration } = useFormat(() => i18n.locale)

/* ---------------- runtime --------------- */
const wsStatus = ref<WsStatus>(socket.status)
const uptimeSeconds = ref<number | null>(null)

const wsMeta = computed(() => {
  switch (wsStatus.value) {
    case 'open':
      return { label: i18n.t('conn.open'), color: 'var(--app-success)' }
    case 'connecting':
    case 'reconnecting':
      return { label: i18n.t('conn.reconnecting'), color: 'var(--app-warn)' }
    default:
      return { label: i18n.t('conn.closed'), color: 'var(--app-text-dim)' }
  }
})

onMounted(async () => {
  socket.onStatus((s) => (wsStatus.value = s))
  await settings.loadRuntime()
  try {
    const h = await systemApi.health()
    uptimeSeconds.value = h.uptime_seconds
  } catch {
    uptimeSeconds.value = null
  }
})

/* ---------------- local prefs --------------- */
const form = reactive({
  connections: settings.prefs.connections,
  maxRetries: settings.prefs.maxRetries,
  rateLimit: settings.prefs.rateLimitKbps,
  ignore: formatExtensionList(settings.prefs.ignore),
  include: formatExtensionList(settings.prefs.include),
  albumFolder: settings.prefs.albumFolder,
  cleanName: settings.prefs.cleanName,
  autoStart: settings.prefs.autoStart,
  openFolder: settings.prefs.openFolder,
})

watch(
  form,
  () => {
    settings.prefs.connections = form.connections || 1
    settings.prefs.maxRetries = form.maxRetries ?? 0
    settings.prefs.rateLimitKbps = form.rateLimit ?? 0
    settings.prefs.ignore = parseExtensionList(form.ignore)
    settings.prefs.include = parseExtensionList(form.include)
    settings.prefs.albumFolder = form.albumFolder
    settings.prefs.cleanName = form.cleanName
    settings.prefs.autoStart = form.autoStart
    settings.prefs.openFolder = form.openFolder
    settings.persist()
  },
  { deep: true },
)

function resetDefaults(): void {
  settings.reset()
  Object.assign(form, {
    connections: settings.prefs.connections,
    maxRetries: settings.prefs.maxRetries,
    rateLimit: settings.prefs.rateLimitKbps,
    ignore: formatExtensionList(settings.prefs.ignore),
    include: formatExtensionList(settings.prefs.include),
    albumFolder: settings.prefs.albumFolder,
    cleanName: settings.prefs.cleanName,
    autoStart: settings.prefs.autoStart,
    openFolder: settings.prefs.openFolder,
  })
  toast.success(i18n.t('settings.saved'))
}

/* ---------------- change password --------------- */
const pwOpen = ref(false)
const pw = reactive({ old: '', next: '', confirm: '' })
const pwBusy = ref(false)
const pwError = ref('')

async function changePassword(): Promise<void> {
  pwError.value = ''
  if (pw.next.length < 6) {
    pwError.value = i18n.t('auth.err.passwordShort')
    return
  }
  if (pw.next !== pw.confirm) {
    pwError.value = i18n.t('auth.err.confirmMismatch')
    return
  }
  pwBusy.value = true
  try {
    await authApi.changePassword({ old_password: pw.old, new_password: pw.next })
    toast.success(i18n.t('account.passwordChanged'))
    pwOpen.value = false
    pw.old = pw.next = pw.confirm = ''
  } catch (e) {
    pwError.value = e instanceof ApiError ? e.message : i18n.t('error.generic')
  } finally {
    pwBusy.value = false
  }
}

/* ---------------- desktop conveniences --------------- */
const dirBusy = ref(false)
const ariaBusy = ref(false)

/** Reveals the download folder in the OS file manager. */
async function openDownloadDir(): Promise<void> {
  if (dirBusy.value) return
  dirBusy.value = true
  try {
    await systemApi.openDownloadDir()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : i18n.t('error.generic'))
  } finally {
    dirBusy.value = false
  }
}

/** Restarts aria2c and refreshes the runtime row above. */
async function restartAria2(): Promise<void> {
  if (ariaBusy.value) return
  ariaBusy.value = true
  try {
    await systemApi.aria2Restart()
    toast.success(i18n.t('settings.aria2Restarted'))
    await settings.loadRuntime()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : i18n.t('error.generic'))
  } finally {
    ariaBusy.value = false
  }
}

/* ---------------- download directory --------------- */
const dlDir = ref(settings.downloadDir)
const dirSaveBusy = ref(false)
watch(
  () => settings.downloadDir,
  (v) => {
    if (dlDir.value.trim() !== v) dlDir.value = v
  },
)

async function saveDownloadDir(): Promise<void> {
  const path = dlDir.value.trim()
  if (!path) {
    toast.error(i18n.t('settings.downloadDirEmpty'))
    return
  }
  if (path === settings.downloadDir) return
  dirSaveBusy.value = true
  try {
    const updated = await systemApi.setDownloadDir(path)
    settings.downloadDir = updated.download_dir
    toast.success(i18n.t('settings.downloadDirSaved'))
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : i18n.t('error.generic'))
  } finally {
    dirSaveBusy.value = false
  }
}

/* ---------------- clear local data --------------- */
const clearOpen = ref(false)
function clearLocal(): void {
  auth.clearSession()
  settings.clearLocal()
  try {
    localStorage.removeItem('bunkr_theme')
    localStorage.removeItem('bunkr_locale')
  } catch {
    /* ignore */
  }
  socket.close()
  void router.replace('/login')
}

/* ---------------- theme / language selects --------------- */
const themeOptions = [
  { value: 'light', label: i18n.t('settings.theme.light') },
  { value: 'dark', label: i18n.t('settings.theme.dark') },
  { value: 'system', label: i18n.t('settings.theme.system') },
]
const langOptions = [
  { value: 'zh-CN', label: '简体中文' },
  { value: 'en', label: 'English' },
]
</script>

<template>
  <div class="bd-set">
    <div class="bd-set__grid">
      <!-- appearance -->
      <section class="bd-set__card card">
        <header class="bd-set__head">
          <span class="bd-set__icon"><Icon name="sun" :size="16" /></span>
          <h2 class="bd-set__title">{{ i18n.t('settings.appearance') }}</h2>
        </header>
        <div class="bd-set__body">
          <BaseSelect
            :model-value="theme.mode"
            :options="themeOptions"
            :label="i18n.t('settings.theme')"
            @update:model-value="(v) => theme.setMode(v as ThemeMode)"
          />
          <BaseSelect
            :model-value="i18n.locale"
            :options="langOptions"
            :label="i18n.t('settings.language')"
            @update:model-value="(v) => i18n.setLocale(v as Locale)"
          />
        </div>
      </section>

      <!-- download defaults -->
      <section class="bd-set__card card">
        <header class="bd-set__head">
          <span class="bd-set__icon"><Icon name="settings" :size="16" /></span>
          <h2 class="bd-set__title">{{ i18n.t('settings.downloads') }}</h2>
          <BaseButton variant="ghost" size="sm" :label="i18n.t('settings.reset')" @click="resetDefaults">
            {{ i18n.t('settings.reset') }}
          </BaseButton>
        </header>
        <p class="bd-set__hint">{{ i18n.t('settings.downloadsHint') }}</p>
        <div class="bd-set__body">
          <div class="bd-set__grid2">
            <BaseInput
              :model-value="form.connections"
              :type="'number'"
              :label="i18n.t('settings.connections')"
              :min="1"
              :max="16"
              @update:model-value="(v) => (form.connections = Number(v) || 1)"
            />
            <BaseInput
              :model-value="form.maxRetries"
              :type="'number'"
              :label="i18n.t('settings.maxRetries')"
              :min="0"
              :max="20"
              @update:model-value="(v) => (form.maxRetries = Number(v) || 0)"
            />
          </div>
          <BaseInput
            :model-value="form.rateLimit"
            :type="'number'"
            :label="i18n.t('settings.rateLimit')"
            :hint="i18n.t('settings.rateLimitHint')"
            :min="0"
            @update:model-value="(v) => (form.rateLimit = Number(v) || 0)"
          />
          <BaseInput v-model="form.ignore" :label="i18n.t('settings.ignore')" :placeholder="i18n.t('settings.ignorePlaceholder')" />
          <BaseInput v-model="form.include" :label="i18n.t('settings.include')" :placeholder="i18n.t('settings.includePlaceholder')" />
          <div class="bd-set__checks">
            <BaseCheckbox v-model="form.albumFolder" :label="i18n.t('settings.albumFolder')" />
            <BaseCheckbox v-model="form.cleanName" :label="i18n.t('settings.cleanName')" />
            <BaseCheckbox v-model="form.autoStart" :label="i18n.t('settings.autoStart')" />
            <BaseCheckbox v-model="form.openFolder" :label="i18n.t('settings.openFolder')" />
          </div>
          <p class="bd-set__autosaved">{{ i18n.t('settings.saved') }} ✓</p>
        </div>
      </section>

      <!-- runtime -->
      <section class="bd-set__card card">
        <header class="bd-set__head">
          <span class="bd-set__icon"><Icon name="database" :size="16" /></span>
          <h2 class="bd-set__title">{{ i18n.t('settings.runtime') }}</h2>
        </header>
        <div class="bd-set__body">
          <div class="bd-set__kv">
            <span class="bd-set__k">{{ i18n.t('settings.version') }}</span>
            <span class="bd-set__v" data-numeric>{{ settings.version || '—' }}</span>
          </div>
          <div class="bd-set__kv">
            <span class="bd-set__k">{{ i18n.t('settings.aria2') }}</span>
            <span class="bd-set__v">
              <span class="bd-set__dot" :style="{ background: settings.aria2?.available ? 'var(--app-success)' : 'var(--app-danger)' }" />
              {{ settings.aria2?.available ? i18n.t('settings.aria2Ready', { version: settings.aria2.version || '?' }) : i18n.t('settings.aria2Missing') }}
            </span>
          </div>
          <div class="bd-set__kv">
            <span class="bd-set__k">{{ i18n.t('settings.uptime') }}</span>
            <span class="bd-set__v" data-numeric>{{ uptimeSeconds === null ? '—' : formatDuration(uptimeSeconds) }}</span>
          </div>
          <template v-if="settings.isDesktop">
            <div class="bd-set__dir">
              <BaseInput
                v-model="dlDir"
                :label="i18n.t('settings.downloadDir')"
                :hint="i18n.t('settings.downloadDirHint')"
                :placeholder="i18n.t('settings.downloadDirUnknown')"
              />
              <BaseButton
                variant="primary"
                size="sm"
                icon="download"
                :loading="dirSaveBusy"
                :label="i18n.t('common.save')"
                @click="saveDownloadDir"
              >
                {{ i18n.t('common.save') }}
              </BaseButton>
            </div>
          </template>
          <div v-else class="bd-set__kv">
            <span class="bd-set__k">{{ i18n.t('settings.downloadDir') }}</span>
            <span class="bd-set__v">{{ settings.downloadDir || i18n.t('settings.downloadDirUnknown') }}</span>
          </div>
          <div class="bd-set__kv">
            <span class="bd-set__k">{{ i18n.t('settings.openDownloadDir') }}</span>
            <span class="bd-set__v">
              <BaseButton
                variant="ghost"
                size="sm"
                :icon="'folder'"
                :loading="dirBusy"
                :label="i18n.t('settings.openDownloadDir')"
                @click="openDownloadDir"
              >
                {{ i18n.t('settings.openDownloadDir') }}
              </BaseButton>
            </span>
          </div>
          <div class="bd-set__kv">
            <span class="bd-set__k">{{ i18n.t('settings.restartAria2') }}</span>
            <span class="bd-set__v">
              <BaseButton
                variant="ghost"
                size="sm"
                :icon="'refresh'"
                :loading="ariaBusy"
                :label="i18n.t('settings.restartAria2')"
                @click="restartAria2"
              >
                {{ i18n.t('settings.restartAria2') }}
              </BaseButton>
            </span>
          </div>
          <div class="bd-set__kv">
            <span class="bd-set__k">{{ i18n.t('settings.connection') }}</span>
            <span class="bd-set__v"><ConnectionIndicator /></span>
          </div>
        </div>
      </section>

      <!-- account -->
      <section class="bd-set__card card">
        <header class="bd-set__head">
          <span class="bd-set__icon"><Icon name="user" :size="16" /></span>
          <h2 class="bd-set__title">{{ i18n.t('settings.security') }}</h2>
        </header>
        <div class="bd-set__body">
          <div class="bd-set__kv">
            <span class="bd-set__k">{{ i18n.t('account.user') }}</span>
            <span class="bd-set__v">{{ auth.user?.username ?? '—' }} · {{ auth.user?.email ?? '—' }}</span>
          </div>
        </div>
      </section>

      <!-- danger -->
      <section class="bd-set__card card is-danger">
        <header class="bd-set__head">
          <span class="bd-set__icon"><Icon name="alert-triangle" :size="16" /></span>
          <h2 class="bd-set__title">{{ i18n.t('settings.danger') }}</h2>
        </header>
        <p class="bd-set__hint">{{ i18n.t('settings.clearLocalHint') }}</p>
        <BaseButton variant="danger" :label="i18n.t('settings.clearLocal')" @click="clearOpen = true">
          <Icon name="trash" :size="14" />
          {{ i18n.t('settings.clearLocal') }}
        </BaseButton>
      </section>
    </div>

    <!-- change password modal -->
    <BaseModal :open="pwOpen" :title="i18n.t('account.changePassword')" size="sm" @close="pwOpen = false">
      <p class="bd-set__hint">{{ i18n.t('account.changePasswordHint') }}</p>
      <div class="bd-set__pw">
        <BaseInput v-model="pw.old" :type="'password'" :label="i18n.t('account.oldPassword')" />
        <BaseInput v-model="pw.next" :type="'password'" :label="i18n.t('account.newPassword')" />
        <BaseInput v-model="pw.confirm" :type="'password'" :label="i18n.t('account.confirmNewPassword')" />
      </div>
      <p v-if="pwError" class="bd-set__error" role="alert">{{ pwError }}</p>
      <template #footer>
        <BaseButton variant="ghost" :label="i18n.t('common.cancel')" @click="pwOpen = false">
          {{ i18n.t('common.cancel') }}
        </BaseButton>
        <BaseButton variant="primary" :loading="pwBusy" :label="i18n.t('common.save')" @click="changePassword">
          {{ i18n.t('common.save') }}
        </BaseButton>
      </template>
    </BaseModal>

    <!-- clear confirm -->
    <BaseModal :open="clearOpen" :title="i18n.t('settings.confirmClearTitle')" size="sm" @close="clearOpen = false">
      <p class="bd-set__error">{{ i18n.t('settings.confirmClear') }}</p>
      <template #footer>
        <BaseButton variant="ghost" :label="i18n.t('common.cancel')" @click="clearOpen = false">
          {{ i18n.t('common.cancel') }}
        </BaseButton>
        <BaseButton variant="danger" :label="i18n.t('settings.clearLocal')" @click="clearLocal">
          {{ i18n.t('settings.clearLocal') }}
        </BaseButton>
      </template>
    </BaseModal>
  </div>
</template>

<style scoped>
.bd-set {
  max-width: 1080px;
  margin: 0 auto;
}
.bd-set__grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
  gap: 16px;
  align-items: start;
}
.bd-set__card {
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.bd-set__card.is-danger {
  border-color: color-mix(in srgb, var(--app-danger) 30%, transparent);
}
.bd-set__head {
  display: flex;
  align-items: center;
  gap: 10px;
}
.bd-set__icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  border-radius: 9px;
  color: var(--app-accent);
  background-color: var(--app-accent-soft);
}
.bd-set__title {
  margin: 0;
  font-size: 14px;
  font-weight: 620;
  flex: 1 1 auto;
}
.bd-set__hint {
  margin: -4px 0 0;
  font-size: 12px;
  color: var(--app-text-dim);
}
.bd-set__body {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.bd-set__grid2 {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}
.bd-set__dir {
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 10px;
  align-items: end;
}
.bd-set__checks {
  display: flex;
  flex-direction: column;
  gap: 9px;
  padding-top: 2px;
}
.bd-set__autosaved {
  margin: 0;
  font-size: 11.5px;
  color: var(--app-text-dim);
}
.bd-set__kv {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  font-size: 12.5px;
}
.bd-set__k {
  color: var(--app-text-muted);
}
.bd-set__v {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--app-text);
  font-weight: 550;
  min-width: 0;
  text-align: right;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.bd-set__dot {
  width: 7px;
  height: 7px;
  border-radius: 999px;
}
.bd-set__pw {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.bd-set__error {
  margin: 0;
  padding: 8px 11px;
  border-radius: 8px;
  font-size: 12px;
  color: var(--app-danger);
  background-color: color-mix(in srgb, var(--app-danger) 9%, transparent);
}
@media (max-width: 480px) {
  .bd-set__grid2 {
    grid-template-columns: 1fr;
  }
}
</style>
