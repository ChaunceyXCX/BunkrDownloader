<script setup lang="ts">
import { computed } from 'vue'
import Icon from './Icon.vue'
import type { IconName } from './Icon.vue'
import { useToastStore } from '@/stores/toast'
import type { ToastType } from '@/stores/toast'
import { useI18nStore } from '@/stores/i18n'

const store = useToastStore()
const i18n = useI18nStore()

const ICON: Record<ToastType, IconName> = {
  success: 'check',
  error: 'alert-triangle',
  warn: 'alert-triangle',
  info: 'inbox',
}
const TITLE_KEY: Record<ToastType, string> = {
  success: 'common.success',
  error: 'common.error',
  warn: 'common.warning',
  info: 'common.info',
}
const items = computed(() => store.toasts)
</script>

<template>
  <Teleport to="body">
    <div class="bd-toasts" role="region" :aria-label="i18n.t('toast.dismiss')">
      <TransitionGroup name="bd-toast">
        <div
          v-for="t in items"
          :key="t.id"
          class="bd-toast"
          :class="`bd-toast--${t.type}`"
          role="status"
          aria-live="polite"
        >
          <span class="bd-toast__icon"><Icon :name="ICON[t.type]" :size="15" :stroke-width="2.2" /></span>
          <div class="bd-toast__body">
            <p class="bd-toast__title">{{ t.title }}</p>
            <p v-if="t.message" class="bd-toast__msg">{{ t.message }}</p>
          </div>
          <button
            type="button"
            class="bd-toast__close"
            :aria-label="i18n.t('toast.dismiss')"
            @click="store.dismiss(t.id)"
          >
            <Icon name="x" :size="14" />
          </button>
          <span class="bd-toast__bar" aria-hidden="true" />
        </div>
      </TransitionGroup>
    </div>
  </Teleport>
</template>

<style scoped>
.bd-toasts {
  position: fixed;
  right: 16px;
  bottom: 16px;
  z-index: 120;
  display: flex;
  flex-direction: column;
  gap: 8px;
  width: min(360px, calc(100vw - 32px));
  pointer-events: none;
}
.bd-toast {
  position: relative;
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 11px 12px;
  border-radius: 10px;
  background-color: var(--app-surface-2);
  border: 1px solid var(--app-border);
  box-shadow: var(--shadow-pop);
  pointer-events: auto;
  overflow: hidden;
}
.bd-toast--success {
  --bd-t: var(--app-success);
}
.bd-toast--error {
  --bd-t: var(--app-danger);
}
.bd-toast--warn {
  --bd-t: var(--app-warn);
}
.bd-toast--info {
  --bd-t: var(--app-info);
}
.bd-toast__icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  flex: 0 0 auto;
  border-radius: 999px;
  color: var(--bd-t);
  background-color: color-mix(in srgb, var(--bd-t) 14%, transparent);
}
.bd-toast__body {
  min-width: 0;
  flex: 1 1 auto;
}
.bd-toast__title {
  margin: 0;
  font-size: 13px;
  font-weight: 600;
  overflow-wrap: anywhere;
}
.bd-toast__msg {
  margin: 2px 0 0;
  font-size: 12px;
  color: var(--app-text-muted);
  overflow-wrap: anywhere;
}
.bd-toast__close {
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--app-text-dim);
  cursor: pointer;
  transition:
    background-color 150ms var(--ease-ui),
    color 150ms var(--ease-ui);
}
.bd-toast__close:hover {
  background-color: var(--app-surface-hover);
  color: var(--app-text);
}
.bd-toast__bar {
  position: absolute;
  left: 0;
  bottom: 0;
  height: 2px;
  width: 100%;
  background-color: var(--bd-t);
  transform-origin: left center;
  animation: bd-toast-bar 4s linear forwards;
}
@keyframes bd-toast-bar {
  from {
    transform: scaleX(1);
  }
  to {
    transform: scaleX(0);
  }
}

.bd-toast-enter-active {
  transition:
    opacity 200ms var(--ease-ui),
    transform 200ms var(--ease-ui);
}
.bd-toast-leave-active {
  transition:
    opacity 160ms var(--ease-ui),
    transform 160ms var(--ease-ui);
  position: absolute;
  right: 0;
  left: 0;
}
.bd-toast-enter-from {
  opacity: 0;
  transform: translateX(16px);
}
.bd-toast-leave-to {
  opacity: 0;
  transform: translateX(24px);
}
.bd-toast-move {
  transition: transform 200ms var(--ease-ui);
}
</style>
