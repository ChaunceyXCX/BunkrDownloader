<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, useId, watch } from 'vue'
import Icon from './Icon.vue'
import type { IconName } from './Icon.vue'

export interface DropdownItem {
  key: string
  type?: 'item' | 'separator'
  label?: string
  icon?: IconName
  tone?: 'default' | 'danger'
  disabled?: boolean
  /** Optional trailing hint, e.g. a plan name. */
  hint?: string
}

const props = withDefaults(
  defineProps<{
    items?: DropdownItem[]
    align?: 'start' | 'end'
    width?: number
    label?: string
    disabled?: boolean
  }>(),
  { align: 'end', width: 190 },
)

const emit = defineEmits<{
  select: [item: DropdownItem]
  open: []
  close: []
  toggle: []
}>()

const menuId = `bd-dd-${useId()}`
const root = ref<HTMLElement | null>(null)
const menu = ref<HTMLElement | null>(null)
const open = ref(false)
const activeIndex = ref(-1)

const items = computed<DropdownItem[]>(() => props.items ?? [])
const selectableIndexes = computed(() =>
  items.value
    .map((it, i) => (it.disabled || it.type === 'separator' ? -1 : i))
    .filter((i) => i >= 0),
)

function close(): void {
  if (!open.value) return
  open.value = false
  activeIndex.value = -1
  emit('close')
}

function toggle(): void {
  if (props.disabled) return
  open.value = !open.value
  if (open.value) emit('open')
  else emit('close')
}

function onDocPointer(e: PointerEvent): void {
  if (!open.value) return
  if (root.value && !root.value.contains(e.target as Node)) close()
}

function onKeydown(e: KeyboardEvent): void {
  if (!open.value) return
  if (e.key === 'Escape') {
    e.stopPropagation()
    close()
    return
  }
  if (e.key === 'ArrowDown' || e.key === 'ArrowUp' || e.key === 'Home' || e.key === 'End') {
    const pool = selectableIndexes.value
    if (pool.length === 0) return
    e.preventDefault()
    let next: number
    if (e.key === 'Home') next = pool[0]
    else if (e.key === 'End') next = pool[pool.length - 1]
    else {
      const pos = pool.indexOf(activeIndex.value)
      const delta = e.key === 'ArrowDown' ? 1 : -1
      next = pos < 0 ? (delta > 0 ? pool[0] : pool[pool.length - 1]) : pool[(pos + delta + pool.length) % pool.length]
    }
    activeIndex.value = next
    void nextTick(() => {
      const nodes = menu.value?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]')
      nodes?.[next]?.focus()
    })
    return
  }
  if ((e.key === 'Enter' || e.key === ' ') && activeIndex.value >= 0) {
    e.preventDefault()
    pick(items.value[activeIndex.value])
  }
}

function pick(item: DropdownItem): void {
  if (item.disabled || item.type === 'separator') return
  close()
  emit('select', item)
}

watch(open, (isOpen) => {
  if (isOpen) document.addEventListener('pointerdown', onDocPointer, true)
  else document.removeEventListener('pointerdown', onDocPointer, true)
})

onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onDocPointer, true)
})
</script>

<template>
  <div ref="root" class="bd-dd" @keydown="onKeydown">
    <!-- When a trigger slot is supplied the consumer owns the (button) element
         so we never nest interactive elements. -->
    <slot
      name="trigger"
      :open="open"
      :toggle="toggle"
      :label="label"
      :controls="menuId"
    >
      <button
        type="button"
        class="bd-dd__trigger"
        :disabled="disabled"
        :aria-label="label"
        :aria-expanded="open"
        :aria-controls="menuId"
        aria-haspopup="menu"
        @click="toggle"
      >
        <span class="sr-only">{{ label }}</span>
      </button>
    </slot>

    <Transition name="bd-dd">
      <div
        v-if="open"
        :id="menuId"
        ref="menu"
        class="bd-dd__panel"
        :class="`bd-dd__panel--${align}`"
        :style="{ width: `${width}px` }"
        role="menu"
        :aria-label="label"
        tabindex="-1"
      >
        <slot :close="close">
          <template v-for="(item, i) in items" :key="item.key">
            <div v-if="item.type === 'separator'" class="bd-dd__divider" role="separator" />
            <button
              v-else
              type="button"
              role="menuitem"
              class="bd-dd__item"
              :class="{ 'is-danger': item.tone === 'danger', 'is-disabled': item.disabled }"
              :disabled="item.disabled"
              :tabindex="i === activeIndex ? 0 : -1"
              @click="pick(item)"
            >
              <Icon v-if="item.icon" :name="item.icon" :size="15" />
              <span class="bd-dd__label">{{ item.label }}</span>
              <span v-if="item.hint" class="bd-dd__hint">{{ item.hint }}</span>
            </button>
          </template>
        </slot>
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.bd-dd {
  position: relative;
  display: inline-flex;
}
.bd-dd__trigger {
  display: inline-flex;
  cursor: pointer;
  border-radius: 8px;
  transition: background-color 150ms var(--ease-ui);
}
.bd-dd__trigger:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}
.bd-dd__panel {
  position: absolute;
  top: calc(100% + 6px);
  z-index: 80;
  padding: 5px;
  border-radius: 10px;
  background-color: var(--app-surface-2);
  border: 1px solid var(--app-border);
  box-shadow: var(--shadow-pop);
  max-height: 60vh;
  overflow-y: auto;
  outline: none;
}
.bd-dd__panel--start {
  left: 0;
}
.bd-dd__panel--end {
  right: 0;
}
.bd-dd__item {
  display: flex;
  align-items: center;
  gap: 9px;
  width: 100%;
  padding: 7px 9px;
  border: 0;
  border-radius: 7px;
  background: transparent;
  color: var(--app-text);
  font-size: 13px;
  text-align: left;
  cursor: pointer;
  transition: background-color 130ms var(--ease-ui);
}
.bd-dd__item:hover:not(:disabled),
.bd-dd__item:focus-visible {
  background-color: var(--app-surface-hover);
  outline: none;
}
.bd-dd__item.is-danger {
  color: var(--app-danger);
}
.bd-dd__item.is-danger:hover:not(:disabled) {
  background-color: color-mix(in srgb, var(--app-danger) 12%, transparent);
}
.bd-dd__item.is-disabled {
  opacity: 0.6;
  cursor: not-allowed;
}
.bd-dd__label {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.bd-dd__hint {
  flex: 0 0 auto;
  font-size: 11px;
  color: var(--app-text-dim);
  max-width: 50%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.bd-dd__divider {
  height: 1px;
  margin: 5px 4px;
  background-color: var(--app-border);
}

.bd-dd-enter-active,
.bd-dd-leave-active {
  transition:
    opacity 150ms var(--ease-ui),
    transform 150ms var(--ease-ui);
}
.bd-dd-enter-from,
.bd-dd-leave-to {
  opacity: 0;
  transform: translateY(-4px) scale(0.985);
}
</style>
