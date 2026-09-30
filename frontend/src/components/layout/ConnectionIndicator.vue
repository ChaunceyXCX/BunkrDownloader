<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { socket } from '@/api/ws'
import type { WsStatus } from '@/api/ws'
import Icon from '@/components/ui/Icon.vue'
import BaseTooltip from '@/components/ui/BaseTooltip.vue'
import { useI18nStore } from '@/stores/i18n'

const i18n = useI18nStore()
const status = ref<WsStatus>(socket.status)
let off: (() => void) | null = null

onMounted(() => {
  off = socket.onStatus((s) => {
    status.value = s
  })
})
onBeforeUnmount(() => off?.())

const meta = computed(() => {
  switch (status.value) {
    case 'open':
      return { label: i18n.t('conn.open'), color: 'var(--app-success)', pulse: false }
    case 'connecting':
    case 'reconnecting':
      return { label: i18n.t('conn.reconnecting'), color: 'var(--app-warn)', pulse: true }
    default:
      return { label: i18n.t('conn.closed'), color: 'var(--app-text-dim)', pulse: false }
  }
})
</script>

<template>
  <BaseTooltip :text="`${i18n.t('conn.title')}: ${meta.label}`" placement="bottom">
    <button
      type="button"
      class="bd-conn"
      :aria-label="`${i18n.t('conn.title')}: ${meta.label}`"
      @click="socket.reconnect()"
    >
      <span class="bd-conn__dot" :class="{ 'bd-anim-pulse-dot': meta.pulse }" :style="{ background: meta.color }" />
      <span class="bd-conn__label">{{ meta.label }}</span>
      <Icon name="wifi" :size="14" class="bd-conn__icon" />
    </button>
  </BaseTooltip>
</template>

<style scoped>
.bd-conn {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  height: 30px;
  padding: 0 10px;
  border-radius: 999px;
  border: 1px solid var(--app-border);
  background-color: var(--app-surface-2);
  color: var(--app-text-muted);
  font-size: 12px;
  cursor: pointer;
  transition:
    border-color 150ms var(--ease-ui),
    color 150ms var(--ease-ui),
    background-color 150ms var(--ease-ui);
}
.bd-conn:hover {
  color: var(--app-text);
  border-color: var(--app-border-strong);
  background-color: var(--app-surface-hover);
}
.bd-conn__dot {
  width: 7px;
  height: 7px;
  border-radius: 999px;
  flex: 0 0 auto;
}
.bd-conn__icon {
  color: var(--app-text-dim);
}
@media (max-width: 1279px) {
  .bd-conn__label {
    display: none;
  }
}
</style>
