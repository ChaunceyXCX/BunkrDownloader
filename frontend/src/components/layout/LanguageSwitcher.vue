<script setup lang="ts">
import { computed } from 'vue'
import { useI18nStore } from '@/stores/i18n'
import type { Locale } from '@/stores/i18n'
import Icon from '@/components/ui/Icon.vue'
import BaseDropdown from '@/components/ui/BaseDropdown.vue'
import type { DropdownItem } from '@/components/ui/BaseDropdown.vue'

const i18n = useI18nStore()

const items = computed<DropdownItem[]>(() => [
  { key: 'zh-CN', label: '简体中文', icon: 'globe', hint: i18n.locale === 'zh-CN' ? '✓' : undefined },
  { key: 'en', label: 'English', icon: 'globe', hint: i18n.locale === 'en' ? '✓' : undefined },
])

function onSelect(item: DropdownItem): void {
  if (item.key === 'zh-CN' || item.key === 'en') i18n.setLocale(item.key as Locale)
}
</script>

<template>
  <BaseDropdown :items="items" :width="160" align="end" :label="i18n.t('nav.language')" @select="onSelect">
    <template #trigger="{ toggle, open, controls }">
      <button
        type="button"
        class="bd-lang"
        :aria-label="`${i18n.t('nav.language')}: ${i18n.isZh ? '简体中文' : 'English'}`"
        :aria-expanded="open"
        :aria-controls="controls"
        aria-haspopup="menu"
        @click="toggle"
      >
        <Icon name="globe" :size="15" />
        <span class="bd-lang__text">{{ i18n.isZh ? '中文' : 'EN' }}</span>
      </button>
    </template>
  </BaseDropdown>
</template>

<style scoped>
.bd-lang {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 32px;
  padding: 0 10px;
  border-radius: 9px;
  border: 1px solid var(--app-border);
  background-color: var(--app-surface-2);
  color: var(--app-text-muted);
  font-size: 12px;
  font-weight: 550;
  cursor: pointer;
  transition:
    background-color 150ms var(--ease-ui),
    color 150ms var(--ease-ui),
    border-color 150ms var(--ease-ui);
}
.bd-lang:hover {
  color: var(--app-text);
  background-color: var(--app-surface-hover);
  border-color: var(--app-border-strong);
}
@media (max-width: 767px) {
  .bd-lang__text {
    display: none;
  }
}
</style>
