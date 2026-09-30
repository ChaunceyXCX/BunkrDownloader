<script setup lang="ts">
import { computed, ref } from 'vue'
import Icon from './Icon.vue'
import type { IconName } from './Icon.vue'

export interface TabItem {
  value: string
  label: string
  icon?: IconName
  count?: number | null
}

const props = withDefaults(
  defineProps<{
    modelValue: string
    items: TabItem[]
    size?: 'sm' | 'md'
    /** `pill` = segmented control, `line` = underlined tabs. */
    variant?: 'pill' | 'line'
  }>(),
  { size: 'md', variant: 'pill' },
)

const emit = defineEmits<{ 'update:modelValue': [value: string] }>()

const root = ref<HTMLElement | null>(null)

function onKeydown(e: KeyboardEvent): void {
  const idx = props.items.findIndex((i) => i.value === props.modelValue)
  if (idx < 0) return
  let next = -1
  if (e.key === 'ArrowRight') next = (idx + 1) % props.items.length
  else if (e.key === 'ArrowLeft') next = (idx - 1 + props.items.length) % props.items.length
  else if (e.key === 'Home') next = 0
  else if (e.key === 'End') next = props.items.length - 1
  if (next < 0) return
  e.preventDefault()
  emit('update:modelValue', props.items[next].value)
  const btns = root.value?.querySelectorAll<HTMLButtonElement>('[role="tab"]')
  btns?.[next]?.focus()
}

const active = computed(() => props.modelValue)
</script>

<template>
  <div
    ref="root"
    class="bd-tabs"
    :class="[`bd-tabs--${variant}`, `bd-tabs--${size}`]"
    role="tablist"
    @keydown="onKeydown"
  >
    <button
      v-for="item in items"
      :key="item.value"
      type="button"
      role="tab"
      class="bd-tab"
      :class="{ 'is-active': item.value === active }"
      :aria-selected="item.value === active"
      :tabindex="item.value === active ? 0 : -1"
      @click="emit('update:modelValue', item.value)"
    >
      <Icon v-if="item.icon" :name="item.icon" :size="14" />
      <span>{{ item.label }}</span>
      <span v-if="item.count !== undefined && item.count !== null" class="bd-tab__count" data-numeric>
        {{ item.count }}
      </span>
    </button>
  </div>
</template>

<style scoped>
.bd-tabs {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  min-width: 0;
  overflow-x: auto;
  scrollbar-width: none;
}
.bd-tabs::-webkit-scrollbar {
  display: none;
}
.bd-tab {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  border: 1px solid transparent;
  background: transparent;
  color: var(--app-text-muted);
  font-weight: 550;
  white-space: nowrap;
  cursor: pointer;
  transition:
    background-color 150ms var(--ease-ui),
    color 150ms var(--ease-ui),
    border-color 150ms var(--ease-ui);
}
.bd-tab:focus-visible {
  outline: 2px solid var(--app-accent);
  outline-offset: 2px;
}

.bd-tabs--pill {
  padding: 3px;
  border-radius: 10px;
  background-color: var(--app-surface-3);
  border: 1px solid var(--app-border);
}
.bd-tabs--pill .bd-tab {
  border-radius: 8px;
  padding: 0 12px;
  height: 30px;
  font-size: 12.5px;
}
.bd-tabs--pill .bd-tab:hover {
  color: var(--app-text);
}
.bd-tabs--pill .bd-tab.is-active {
  background-color: var(--app-surface);
  border-color: var(--app-border);
  color: var(--app-text);
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.08);
}
.bd-tabs--pill.bd-tabs--sm .bd-tab {
  height: 26px;
  font-size: 12px;
  padding: 0 10px;
}

.bd-tabs--line {
  gap: 4px;
  border-bottom: 1px solid var(--app-border);
}
.bd-tabs--line .bd-tab {
  position: relative;
  padding: 8px 12px 10px;
  font-size: 13px;
  border-radius: 0;
  border-bottom: 2px solid transparent;
  margin-bottom: -1px;
}
.bd-tabs--line .bd-tab:hover {
  color: var(--app-text);
}
.bd-tabs--line .bd-tab.is-active {
  color: var(--app-text);
  border-bottom-color: var(--app-accent);
}

.bd-tab__count {
  min-width: 18px;
  padding: 0 5px;
  border-radius: 999px;
  background-color: var(--app-surface-hover);
  color: var(--app-text-muted);
  font-size: 11px;
  line-height: 16px;
  text-align: center;
}
.bd-tab.is-active .bd-tab__count {
  background-color: var(--app-accent-soft);
  color: var(--app-accent);
}
</style>
