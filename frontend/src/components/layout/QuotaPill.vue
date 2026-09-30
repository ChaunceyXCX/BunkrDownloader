<script setup lang="ts">
import { computed } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { useI18nStore } from '@/stores/i18n'
import { navigate } from '@/router/bridge'
import BaseDropdown from '@/components/ui/BaseDropdown.vue'
import type { DropdownItem } from '@/components/ui/BaseDropdown.vue'
import BaseProgress from '@/components/ui/BaseProgress.vue'
import Icon from '@/components/ui/Icon.vue'
import { useFormat } from '@/composables/useFormat'

const auth = useAuthStore()
const i18n = useI18nStore()
const { formatCount } = useFormat(() => i18n.locale)

const pct = computed(() => auth.quotaPercent)
const tone = computed(() => (pct.value >= 100 ? 'danger' : pct.value >= 80 ? 'warn' : 'accent'))
const label = computed(() =>
  auth.linksUnlimited
    ? `${formatCount(auth.linksUsed)} ${i18n.t('common.unlimited')}`
    : `${auth.linksUsed}/${auth.linksLimit}`,
)
const fileLabel = computed(() =>
  auth.filesUnlimited
    ? `${formatCount(auth.filesUsed)} ${i18n.t('common.unlimited')}`
    : `${auth.filesUsed}/${auth.filesLimit}`,
)
// Concurrency is unlimited (config default 0) — only link/file counts gate an
// account now, so hide the running/slot row instead of showing "0 / 0".
const concurrencyUnlimited = computed(() => auth.concurrentLimit <= 0)

const items = computed<DropdownItem[]>(() => [{ key: 'membership', label: i18n.t('membership.title'), icon: 'crown' }])
function onSelect(): void {
  navigate('/app/membership', false)
}
</script>

<template>
  <BaseDropdown :items="items" :width="230" :label="i18n.t('quota.popover')" align="end" @select="onSelect">
    <template #trigger="{ toggle, open, controls }">
      <button
        type="button"
        class="bd-quota"
        :aria-label="`${i18n.t('quota.popover')}: ${label}`"
        :aria-expanded="open"
        :aria-controls="controls"
        aria-haspopup="menu"
        @click="toggle"
      >
        <span class="bd-quota__label">{{ i18n.t('quota.links') }}</span>
        <span class="bd-quota__value" data-numeric>{{ label }}</span>
        <span class="bd-quota__bar">
          <BaseProgress
            :value="pct"
            :tone="tone"
            :height="4"
            :label="i18n.t('quota.links')"
          />
        </span>
      </button>
    </template>

    <template #default>
      <div class="bd-quota__panel">
        <p class="bd-quota__head">{{ i18n.t('quota.popover') }}</p>

        <div class="bd-quota__row">
          <div class="bd-quota__rowhead">
            <span>{{ i18n.t('quota.links') }}</span>
            <span data-numeric class="bd-quota__num">{{ label }}</span>
          </div>
          <BaseProgress
            :value="pct"
            :tone="tone"
            :height="5"
            :label="i18n.t('quota.links')"
          />
        </div>

        <div class="bd-quota__row">
          <div class="bd-quota__rowhead">
            <span>{{ i18n.t('quota.files') }}</span>
            <span data-numeric class="bd-quota__num">{{ fileLabel }}</span>
          </div>
          <BaseProgress
            :value="auth.filesPercent"
            :tone="auth.filesPercent >= 100 ? 'danger' : auth.filesPercent >= 80 ? 'warn' : 'accent'"
            :height="5"
            :label="i18n.t('quota.files')"
          />
        </div>

        <div v-if="!concurrencyUnlimited" class="bd-quota__rowhead bd-quota__conc">
          <span>{{ i18n.t('quota.concurrent') }}</span>
          <span data-numeric class="bd-quota__num">
            {{ auth.concurrentRunning }} / {{ auth.concurrentLimit }}
          </span>
        </div>

        <p v-if="!auth.isMember" class="bd-quota__upsell">
          <Icon name="crown" :size="13" />
          {{ i18n.t('membership.currentFree') }}
        </p>
      </div>
    </template>
  </BaseDropdown>
</template>

<style scoped>
.bd-quota {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  height: 30px;
  padding: 0 10px;
  border-radius: 999px;
  border: 1px solid var(--app-border);
  background-color: var(--app-surface-2);
  cursor: pointer;
  transition:
    border-color 150ms var(--ease-ui),
    background-color 150ms var(--ease-ui);
}
.bd-quota:hover {
  border-color: var(--app-border-strong);
  background-color: var(--app-surface-hover);
}
.bd-quota__label {
  font-size: 11.5px;
  color: var(--app-text-dim);
}
.bd-quota__value {
  font-size: 11.5px;
  font-weight: 600;
  color: var(--app-text);
}
.bd-quota__bar {
  width: 44px;
  display: inline-flex;
}
.bd-quota__panel {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 4px 4px 2px;
}
.bd-quota__head {
  margin: 0 0 2px;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.07em;
  text-transform: uppercase;
  color: var(--app-text-dim);
}
.bd-quota__row {
  display: flex;
  flex-direction: column;
  gap: 5px;
}
.bd-quota__rowhead {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 12px;
  color: var(--app-text-muted);
}
.bd-quota__conc {
  padding-top: 2px;
  border-top: 1px solid var(--app-border);
}
.bd-quota__num {
  color: var(--app-text);
  font-weight: 600;
}
.bd-quota__upsell {
  margin: 0;
  display: flex;
  align-items: flex-start;
  gap: 6px;
  padding: 7px 8px;
  border-radius: 8px;
  background-color: var(--app-accent-soft);
  color: var(--app-accent);
  font-size: 11.5px;
  line-height: 1.45;
}
</style>
