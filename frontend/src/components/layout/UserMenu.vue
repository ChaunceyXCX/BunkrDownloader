<script setup lang="ts">
import { computed } from 'vue'
import { navigate } from '@/router/bridge'
import { useAuthStore } from '@/stores/auth'
import { useI18nStore } from '@/stores/i18n'
import Icon from '@/components/ui/Icon.vue'
import BaseDropdown from '@/components/ui/BaseDropdown.vue'
import type { DropdownItem } from '@/components/ui/BaseDropdown.vue'
import { useFormat } from '@/composables/useFormat'

const auth = useAuthStore()
const i18n = useI18nStore()
const { formatCount } = useFormat(() => i18n.locale)

const initial = computed(() => (auth.user?.username ?? '?').trim().charAt(0).toUpperCase() || '?')
const isMember = computed(() => auth.isMember)

const items = computed<DropdownItem[]>(() => [
  { key: 'email', label: auth.user?.email ?? '—', icon: 'user', disabled: true },
  { key: 'sep1', type: 'separator' },
  { key: 'membership', label: i18n.t('nav.membership'), icon: 'crown' },
  { key: 'settings', label: i18n.t('nav.settings'), icon: 'settings' },
  { key: 'sep2', type: 'separator' },
  { key: 'logout', label: i18n.t('nav.logout'), icon: 'log-out', tone: 'danger' },
])

function onSelect(item: DropdownItem): void {
  if (item.key === 'membership') navigate('/app/membership', false)
  else if (item.key === 'settings') navigate('/app/settings', false)
  else if (item.key === 'logout') void auth.logout()
}

const quotaSummary = computed(() => {
  if (!auth.quota) return '—'
  if (auth.linksUnlimited) return `${formatCount(auth.linksUsed)} ${i18n.t('common.unlimited')}`
  return `${auth.linksUsed}/${auth.linksLimit}`
})
</script>

<template>
  <BaseDropdown :items="items" :width="230" :label="auth.user?.username ?? ''" @select="onSelect">
    <template #trigger="{ toggle, open, controls }">
      <button
        type="button"
        class="bd-user"
        :aria-label="auth.user?.username ?? ''"
        :aria-expanded="open"
        :aria-controls="controls"
        aria-haspopup="menu"
        @click="toggle"
      >
        <span class="bd-user__avatar" :class="{ 'is-member': isMember }">{{ initial }}</span>
        <span class="bd-user__meta">
          <span class="bd-user__name">{{ auth.user?.username ?? '—' }}</span>
          <span class="bd-user__plan">
            <Icon v-if="isMember" name="crown" :size="11" :stroke-width="2" />
            {{ isMember ? i18n.t('account.planMember') : i18n.t('account.planFree') }}
            <span class="bd-user__quota" data-numeric>· {{ quotaSummary }}</span>
          </span>
        </span>
        <Icon name="chevron-down" :size="14" class="bd-user__caret" />
      </button>
    </template>
  </BaseDropdown>
</template>

<style scoped>
.bd-user {
  display: inline-flex;
  align-items: center;
  gap: 9px;
  height: 38px;
  padding: 0 8px 0 6px;
  border-radius: 10px;
  border: 1px solid var(--app-border);
  background-color: var(--app-surface-2);
  cursor: pointer;
  transition:
    border-color 150ms var(--ease-ui),
    background-color 150ms var(--ease-ui);
}
.bd-user:hover {
  border-color: var(--app-border-strong);
  background-color: var(--app-surface-hover);
}
.bd-user__avatar {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  flex: 0 0 auto;
  border-radius: 8px;
  background-color: var(--app-surface-3);
  border: 1px solid var(--app-border);
  color: var(--app-text);
  font-size: 12px;
  font-weight: 650;
}
.bd-user__avatar.is-member {
  background-image: linear-gradient(120deg, var(--app-accent), var(--app-accent-3));
  border-color: transparent;
  color: var(--app-accent-contrast);
}
.bd-user__meta {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  line-height: 1.25;
  min-width: 0;
}
.bd-user__name {
  font-size: 12.5px;
  font-weight: 600;
  max-width: 130px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.bd-user__plan {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  font-size: 11px;
  color: var(--app-text-dim);
  white-space: nowrap;
}
.bd-user__quota {
  font-variant-numeric: tabular-nums;
}
.bd-user__caret {
  color: var(--app-text-dim);
}
@media (max-width: 639px) {
  .bd-user__meta,
  .bd-user__caret {
    display: none;
  }
}
</style>
