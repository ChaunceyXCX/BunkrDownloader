<script setup lang="ts">
import { computed } from 'vue'
import { useThemeStore } from '@/stores/theme'
import type { ThemeMode } from '@/stores/theme'
import { useI18nStore } from '@/stores/i18n'
import Icon from '@/components/ui/Icon.vue'
import BaseDropdown from '@/components/ui/BaseDropdown.vue'
import type { DropdownItem } from '@/components/ui/BaseDropdown.vue'

const theme = useThemeStore()
const i18n = useI18nStore()

const MODES: { value: ThemeMode; key: string; icon: 'sun' | 'moon' | 'layers' }[] = [
  { value: 'light', key: 'settings.theme.light', icon: 'sun' },
  { value: 'dark', key: 'settings.theme.dark', icon: 'moon' },
  { value: 'system', key: 'settings.theme.system', icon: 'layers' },
]

const items = computed<DropdownItem[]>(() =>
  MODES.map((m) => ({
    key: m.value,
    label: i18n.t(m.key),
    icon: m.icon,
    hint: theme.mode === m.value ? '✓' : undefined,
  })),
)
const activeIcon = computed(() => MODES.find((m) => m.value === theme.resolved)?.icon ?? 'moon')
const activeLabel = computed(() => MODES.find((m) => m.value === theme.mode)?.key ?? 'settings.theme.system')

function onSelect(item: DropdownItem): void {
  if (item.key === 'light' || item.key === 'dark' || item.key === 'system') theme.setMode(item.key)
}
</script>

<template>
  <BaseDropdown :items="items" :width="150" align="end" :label="i18n.t('nav.theme')" @select="onSelect">
    <template #trigger="{ toggle, open, controls }">
      <button
        type="button"
        class="bd-iconbtn"
        :aria-label="`${i18n.t('nav.theme')}: ${i18n.t(activeLabel)}`"
        :aria-expanded="open"
        :aria-controls="controls"
        aria-haspopup="menu"
        @click="toggle"
      >
        <Icon :name="activeIcon" :size="16" />
      </button>
    </template>
  </BaseDropdown>
</template>

<style scoped>
.bd-iconbtn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border-radius: 9px;
  border: 1px solid var(--app-border);
  background-color: var(--app-surface-2);
  color: var(--app-text-muted);
  cursor: pointer;
  transition:
    background-color 150ms var(--ease-ui),
    color 150ms var(--ease-ui),
    border-color 150ms var(--ease-ui);
}
.bd-iconbtn:hover {
  color: var(--app-text);
  background-color: var(--app-surface-hover);
  border-color: var(--app-border-strong);
}
</style>
