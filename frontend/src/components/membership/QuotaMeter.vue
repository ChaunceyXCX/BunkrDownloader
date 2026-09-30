<script setup lang="ts">
import { computed } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { useI18nStore } from '@/stores/i18n'
import { useFormat } from '@/composables/useFormat'
import BaseProgress from '@/components/ui/BaseProgress.vue'
import Icon from '@/components/ui/Icon.vue'

const auth = useAuthStore()
const i18n = useI18nStore()
const { formatCount } = useFormat(() => i18n.locale)

const linksLabel = computed(() =>
  auth.linksUnlimited
    ? `${formatCount(auth.linksUsed)} / ∞`
    : `${auth.linksUsed} / ${auth.linksLimit}`,
)
const filesLabel = computed(() =>
  auth.filesUnlimited
    ? `${formatCount(auth.filesUsed)} / ∞`
    : `${auth.filesUsed} / ${auth.filesLimit}`,
)
const linksTone = computed(() =>
  auth.quotaPercent >= 100 ? 'danger' : auth.quotaPercent >= 80 ? 'warn' : 'accent',
)
const filesTone = computed(() =>
  auth.filesPercent >= 100 ? 'danger' : auth.filesPercent >= 80 ? 'warn' : 'accent',
)
</script>

<template>
  <div class="bd-quotameter card">
    <div class="bd-quotameter__head">
      <span class="bd-quotameter__icon"><Icon name="database" :size="16" /></span>
      <h3 class="bd-quotameter__title">{{ i18n.t('quota.title') }}</h3>
      <span v-if="auth.isMember" class="bd-quotameter__badge">{{ i18n.t('account.planMember') }}</span>
    </div>

    <div class="bd-quotameter__row">
      <div class="bd-quotameter__rowhead">
        <span>{{ i18n.t('quota.links') }}</span>
        <span class="bd-quotameter__num" data-numeric>{{ linksLabel }}</span>
      </div>
      <BaseProgress :value="auth.quotaPercent" :tone="linksTone" :height="6" :label="i18n.t('quota.links')" />
    </div>

    <div class="bd-quotameter__row">
      <div class="bd-quotameter__rowhead">
        <span>{{ i18n.t('quota.files') }}</span>
        <span class="bd-quotameter__num" data-numeric>{{ filesLabel }}</span>
      </div>
      <BaseProgress :value="auth.filesPercent" :tone="filesTone" :height="6" :label="i18n.t('quota.files')" />
    </div>
  </div>
</template>

<style scoped>
.bd-quotameter {
  padding: 15px 16px;
  display: flex;
  flex-direction: column;
  gap: 13px;
}
.bd-quotameter__head {
  display: flex;
  align-items: center;
  gap: 9px;
}
.bd-quotameter__icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: 8px;
  color: var(--app-accent);
  background-color: var(--app-accent-soft);
}
.bd-quotameter__title {
  margin: 0;
  font-size: 13.5px;
  font-weight: 600;
  flex: 1 1 auto;
}
.bd-quotameter__badge {
  padding: 3px 8px;
  border-radius: 999px;
  background-image: linear-gradient(120deg, var(--app-accent), var(--app-accent-3));
  color: #fff;
  font-size: 11px;
  font-weight: 600;
}
.bd-quotameter__row {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.bd-quotameter__rowhead {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 12px;
  color: var(--app-text-muted);
}
.bd-quotameter__num {
  color: var(--app-text);
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}
</style>
