<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import Icon from './Icon.vue'

const props = withDefaults(
  defineProps<{
    open: boolean
    title?: string
    description?: string
    size?: 'sm' | 'md' | 'lg' | 'xl'
    /** Click on the backdrop dismisses the dialog. */
    dismissable?: boolean
  }>(),
  { size: 'md', dismissable: true },
)

const emit = defineEmits<{ close: [] }>()

const panel = ref<HTMLElement | null>(null)
const FOCUSABLE =
  'a[href],button:not([disabled]),textarea:not([disabled]),input:not([disabled]),select:not([disabled]),[tabindex]:not([tabindex="-1"])'

function onKeydown(e: KeyboardEvent): void {
  if (!props.open) return
  if (e.key === 'Escape') {
    e.stopPropagation()
    emit('close')
    return
  }
  if (e.key !== 'Tab' || !panel.value) return
  const nodes = Array.from(panel.value.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(
    (n) => n.offsetParent !== null,
  )
  if (nodes.length === 0) return
  const first = nodes[0]
  const last = nodes[nodes.length - 1]
  const active = document.activeElement as HTMLElement | null
  if (e.shiftKey && (active === first || !panel.value.contains(active))) {
    e.preventDefault()
    last.focus()
  } else if (!e.shiftKey && active === last) {
    e.preventDefault()
    first.focus()
  }
}

let scrollLockCount = 0
let savedOverflow = ''

function lockScroll(): void {
  scrollLockCount += 1
  if (scrollLockCount === 1) {
    savedOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
  }
}

function unlockScroll(): void {
  scrollLockCount = Math.max(0, scrollLockCount - 1)
  if (scrollLockCount === 0) document.body.style.overflow = savedOverflow
}

watch(
  () => props.open,
  async (open) => {
    if (open) {
      lockScroll()
      document.addEventListener('keydown', onKeydown, true)
      await nextTick()
      const target = panel.value?.querySelector<HTMLElement>(FOCUSABLE)
      target?.focus()
    } else {
      unlockScroll()
      document.removeEventListener('keydown', onKeydown, true)
    }
  },
)

onBeforeUnmount(() => {
  document.removeEventListener('keydown', onKeydown, true)
  if (props.open) unlockScroll()
})
</script>

<template>
  <Teleport to="body">
    <Transition name="bd-modal">
      <div v-if="open" class="bd-modal" :class="`bd-modal--${size}`">
        <div class="bd-modal__backdrop" @click="dismissable && emit('close')" />
        <div
          ref="panel"
          class="bd-modal__panel"
          role="dialog"
          aria-modal="true"
          :aria-label="title ?? undefined"
          @click.stop
        >
          <header v-if="title || $slots.header" class="bd-modal__head">
            <slot name="header">
              <div class="min-w-0">
                <h2 class="bd-modal__title">{{ title }}</h2>
                <p v-if="description" class="bd-modal__desc">{{ description }}</p>
              </div>
            </slot>
            <button
              type="button"
              class="bd-modal__close"
              :aria-label="$slots.closeLabel ? undefined : 'close'"
              @click="emit('close')"
            >
              <slot name="closeLabel"><span class="sr-only">close</span></slot>
              <Icon name="x" :size="16" />
            </button>
          </header>
          <div class="bd-modal__body">
            <slot />
          </div>
          <footer v-if="$slots.footer" class="bd-modal__foot">
            <slot name="footer" />
          </footer>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.bd-modal {
  position: fixed;
  inset: 0;
  z-index: 90;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
}
.bd-modal__backdrop {
  position: absolute;
  inset: 0;
  background-color: color-mix(in srgb, var(--app-text) 42%, transparent);
  backdrop-filter: blur(6px);
  -webkit-backdrop-filter: blur(6px);
}
.bd-modal__panel {
  position: relative;
  width: 100%;
  max-height: calc(100vh - 40px);
  display: flex;
  flex-direction: column;
  border-radius: 14px;
  background-color: var(--app-surface-2);
  border: 1px solid var(--app-border);
  box-shadow: var(--shadow-pop);
  overflow: hidden;
}
.bd-modal--sm {
  max-width: 380px;
}
.bd-modal--md {
  max-width: 520px;
}
.bd-modal--lg {
  max-width: 720px;
}
.bd-modal--xl {
  max-width: 960px;
}
.bd-modal__head {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 16px 18px 12px;
  border-bottom: 1px solid var(--app-border);
}
.bd-modal__title {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
}
.bd-modal__desc {
  margin: 4px 0 0;
  font-size: 12.5px;
  color: var(--app-text-muted);
}
.bd-modal__close {
  margin-left: auto;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: 8px;
  border: 1px solid transparent;
  background: transparent;
  color: var(--app-text-dim);
  cursor: pointer;
  transition:
    background-color 150ms var(--ease-ui),
    color 150ms var(--ease-ui);
}
.bd-modal__close:hover {
  background-color: var(--app-surface-hover);
  color: var(--app-text);
}
.bd-modal__body {
  padding: 18px;
  overflow-y: auto;
  flex: 1 1 auto;
}
.bd-modal__foot {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 18px;
  border-top: 1px solid var(--app-border);
  background-color: var(--app-surface-3);
}

.bd-modal-enter-active .bd-modal__panel,
.bd-modal-leave-active .bd-modal__panel {
  transition:
    opacity 180ms var(--ease-ui),
    transform 180ms var(--ease-ui);
}
.bd-modal-enter-active .bd-modal__backdrop,
.bd-modal-leave-active .bd-modal__backdrop {
  transition: opacity 180ms var(--ease-ui);
}
.bd-modal-enter-from .bd-modal__panel,
.bd-modal-leave-to .bd-modal__panel {
  opacity: 0;
  transform: translateY(10px) scale(0.975);
}
.bd-modal-enter-from .bd-modal__backdrop,
.bd-modal-leave-to .bd-modal__backdrop {
  opacity: 0;
}
</style>
