<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { useTasksStore } from '@/stores/tasks'
import { useSettingsStore, parseExtensionList } from '@/stores/settings'
import { useI18nStore } from '@/stores/i18n'
import { navigate } from '@/router/bridge'
import BaseModal from '@/components/ui/BaseModal.vue'
import BaseTextarea from '@/components/ui/BaseTextarea.vue'
import BaseInput from '@/components/ui/BaseInput.vue'
import BaseCheckbox from '@/components/ui/BaseCheckbox.vue'
import BaseButton from '@/components/ui/BaseButton.vue'
import Icon from '@/components/ui/Icon.vue'
import type { TaskOptions } from '@/api/types'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [] }>()

const auth = useAuthStore()
const tasks = useTasksStore()
const settings = useSettingsStore()
const i18n = useI18nStore()

/* ---------------- validation helpers ---------------- */
function isBunkrUrl(line: string): boolean {
  const trimmed = line.trim()
  if (!trimmed) return false
  try {
    const u = new URL(trimmed)
    return /^https?:$/.test(u.protocol) && /bunkr/i.test(u.hostname)
  } catch {
    return false
  }
}

function nonEmptyLines(): string[] {
  return urls.value.split('\n').map((s) => s.trim()).filter((s) => s !== '')
}

/* ---------------- form state ---------------- */
const urls = ref('')
const advancedOpen = ref(false)
interface FormState {
  connections: number
  maxRetries: number
  rateLimit: number
  ignore: string
  include: string
  noAlbumFolder: boolean
  cleanName: boolean
  autoStart: boolean
}
const opts = reactive<FormState>({
  connections: settings.prefs.connections,
  maxRetries: settings.prefs.maxRetries,
  rateLimit: settings.prefs.rateLimitKbps,
  ignore: settings.prefs.ignore.join(', '),
  include: settings.prefs.include.join(', '),
  noAlbumFolder: !settings.prefs.albumFolder,
  cleanName: settings.prefs.cleanName,
  autoStart: settings.prefs.autoStart,
})
const touched = ref(false)

const invalidLines = computed<number[]>(() => {
  const out: number[] = []
  urls.value.split('\n').forEach((raw, idx) => {
    const line = raw.trim()
    if (line && !isBunkrUrl(line)) out.push(idx)
  })
  return out
})

const urlsCount = computed(() => nonEmptyLines().filter(isBunkrUrl).length)
const hasBlankInvalid = computed(() => nonEmptyLines().some((l) => !isBunkrUrl(l)))

/* ---------------- quota pre-check ---------------- */
const need = computed(() => urlsCount.value)
const linksUnlimited = computed(() => auth.linksUnlimited || auth.linksLimit < 0)
const quotaLeft = computed(() => Math.max(0, auth.linksLimit - auth.linksUsed))
const canSubmitUrl = computed(() => linksUnlimited.value || auth.linksUsed + need.value <= auth.linksLimit)
const quotaWarnVisible = computed(() => !linksUnlimited.value && need.value > 0 && !canSubmitUrl.value)

const submitable = computed(
  () => canSubmitUrl.value && need.value > 0 && invalidLines.value.length === 0 && !tasks.submitting,
)

function buildOptions(): TaskOptions {
  return {
    max_retries: opts.maxRetries,
    connections: opts.connections,
    rate_limit_kbps: opts.rateLimit,
    ignore: parseExtensionList(opts.ignore),
    include: parseExtensionList(opts.include),
    no_album_folder: opts.noAlbumFolder,
    clean_name: opts.cleanName,
  }
}

async function submit(): Promise<void> {
  touched.value = true
  if (!submitable.value) return
  const lines = nonEmptyLines().filter(isBunkrUrl)
  try {
    await tasks.createTask(lines, buildOptions(), opts.autoStart)
    emit('close')
  } catch {
    // The store / global handler surfaces the error; keep the modal open.
  }
}

function onClose(): void {
  if (tasks.submitting) return
  emit('close')
}

watch(
  () => props.open,
  (open) => {
    if (open) {
      urls.value = ''
      touched.value = false
      advancedOpen.value = false
      Object.assign(opts, {
        connections: settings.prefs.connections,
        maxRetries: settings.prefs.maxRetries,
        rateLimit: settings.prefs.rateLimitKbps,
        ignore: settings.prefs.ignore.join(', '),
        include: settings.prefs.include.join(', '),
        noAlbumFolder: !settings.prefs.albumFolder,
        cleanName: settings.prefs.cleanName,
        autoStart: settings.prefs.autoStart,
      })
    }
  },
)
</script>

<template>
  <BaseModal :open="open" :title="i18n.t('newTask.title')" size="lg" @close="onClose">
    <form class="bd-new" @submit.prevent="submit">
      <BaseTextarea
        v-model="urls"
        :label="i18n.t('newTask.urls')"
        :hint="i18n.t('newTask.urlsHint')"
        :invalid="touched && invalidLines.length > 0"
        :rows="5"
        monospace
        :placeholder="i18n.t('newTask.urlsPlaceholder')"
      />

      <p v-if="touched && invalidLines.length > 0" class="bd-new__invalid" role="alert">
        <Icon name="alert-triangle" :size="14" />
        {{ i18n.t('newTask.invalidLine', { n: invalidLines[0] + 1 }) }}
      </p>
      <p v-else-if="need > 0" class="bd-new__count" data-numeric>
        {{ i18n.t('newTask.urlsCount', { n: need }) }}
      </p>

      <!-- quota gate -->
      <div v-if="quotaWarnVisible" class="bd-new__quota" role="alert">
        <Icon name="alert-triangle" :size="16" />
        <div class="bd-new__quota-body">
          <p class="bd-new__quota-title">{{ i18n.t('newTask.quotaWarnTitle') }}</p>
          <p class="bd-new__quota-text">
            {{
              auth.linksUsed >= auth.linksLimit
                ? i18n.t('newTask.quotaWarn', { limit: auth.linksLimit, used: auth.linksUsed, need })
                : i18n.t('newTask.quotaWarnLeft', { left: quotaLeft, need })
            }}
          </p>
        </div>
        <BaseButton
          variant="primary"
          size="sm"
          :label="i18n.t('newTask.upgrade')"
          @click="navigate('/app/membership', false)"
        >
          {{ i18n.t('newTask.upgrade') }}
        </BaseButton>
      </div>

      <button
        type="button"
        class="bd-new__toggle"
        :aria-expanded="advancedOpen"
        @click="advancedOpen = !advancedOpen"
      >
        <Icon :name="advancedOpen ? 'chevron-up' : 'chevron-down'" :size="15" />
        {{ i18n.t('newTask.advanced') }}
      </button>

      <div v-if="advancedOpen" class="bd-new__advanced">
        <div class="bd-new__grid">
          <BaseInput
            :model-value="opts.connections"
            :type="'number'"
            :label="i18n.t('newTask.connections')"
            :min="1"
            :max="16"
            @update:model-value="(v) => (opts.connections = Number(v) || 1)"
          />
          <BaseInput
            :model-value="opts.maxRetries"
            :type="'number'"
            :label="i18n.t('newTask.maxRetries')"
            :min="0"
            :max="20"
            @update:model-value="(v) => (opts.maxRetries = Number(v) || 0)"
          />
        </div>
        <BaseInput
          :model-value="opts.rateLimit"
          :type="'number'"
          :label="i18n.t('newTask.rateLimit')"
          :hint="i18n.t('newTask.rateLimitHint')"
          :min="0"
          @update:model-value="(v) => (opts.rateLimit = Number(v) || 0)"
        />
        <div class="bd-new__grid">
          <BaseInput
            v-model="opts.ignore"
            :label="i18n.t('newTask.ignore')"
            :placeholder="i18n.t('newTask.ignorePlaceholder')"
          />
          <BaseInput
            v-model="opts.include"
            :label="i18n.t('newTask.include')"
            :placeholder="i18n.t('newTask.includePlaceholder')"
          />
        </div>
        <div class="bd-new__checks">
          <BaseCheckbox v-model="opts.noAlbumFolder" :label="i18n.t('newTask.noAlbumFolder')" />
          <BaseCheckbox v-model="opts.cleanName" :label="i18n.t('newTask.cleanName')" />
          <BaseCheckbox v-model="opts.autoStart" :label="i18n.t('newTask.autoStart')" />
        </div>
      </div>
    </form>

    <template #footer>
      <BaseButton variant="ghost" :label="i18n.t('common.cancel')" :disabled="tasks.submitting" @click="onClose">
        {{ i18n.t('common.cancel') }}
      </BaseButton>
      <BaseButton
        variant="primary"
        :label="tasks.submitting ? i18n.t('newTask.submitting') : i18n.t('newTask.submit')"
        :loading="tasks.submitting"
        :disabled="!submitable"
        @click="submit"
      >
        {{ tasks.submitting ? i18n.t('newTask.submitting') : i18n.t('newTask.submit') }}
      </BaseButton>
    </template>
  </BaseModal>
</template>

<style scoped>
.bd-new {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.bd-new__invalid,
.bd-new__count {
  margin: 0;
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  padding: 8px 11px;
  border-radius: 8px;
}
.bd-new__invalid {
  color: var(--app-danger);
  background-color: color-mix(in srgb, var(--app-danger) 10%, transparent);
}
.bd-new__count {
  color: var(--app-text-muted);
  background-color: var(--app-surface-3);
}
.bd-new__quota {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border-radius: 10px;
  color: var(--app-warn);
  background-color: color-mix(in srgb, var(--app-warn) 9%, transparent);
  border: 1px solid color-mix(in srgb, var(--app-warn) 28%, transparent);
}
.bd-new__quota-body {
  flex: 1 1 auto;
  min-width: 0;
}
.bd-new__quota-title {
  margin: 0;
  font-size: 12.5px;
  font-weight: 600;
  color: color-mix(in srgb, var(--app-warn) 88%, var(--app-text));
}
.bd-new__quota-text {
  margin: 2px 0 0;
  font-size: 12px;
  color: var(--app-text-muted);
}
.bd-new__toggle {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  align-self: flex-start;
  padding: 5px 8px;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: var(--app-text-muted);
  font-size: 12.5px;
  font-weight: 550;
  cursor: pointer;
  transition:
    background-color 150ms var(--ease-ui),
    color 150ms var(--ease-ui);
}
.bd-new__toggle:hover {
  background-color: var(--app-surface-hover);
  color: var(--app-text);
}
.bd-new__advanced {
  display: flex;
  flex-direction: column;
  gap: 11px;
  padding: 13px;
  border-radius: 10px;
  background-color: var(--app-surface-3);
  border: 1px solid var(--app-border);
}
.bd-new__grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 11px;
}
.bd-new__checks {
  display: flex;
  flex-direction: column;
  gap: 9px;
  padding-top: 2px;
}
@media (max-width: 480px) {
  .bd-new__grid {
    grid-template-columns: 1fr;
  }
}
</style>
