<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import Icon from './Icon.vue'
import type { IconName } from './Icon.vue'
import BaseSpinner from './BaseSpinner.vue'

type Variant = 'primary' | 'secondary' | 'ghost' | 'danger'
type Size = 'sm' | 'md' | 'lg'

const props = withDefaults(
  defineProps<{
    variant?: Variant
    size?: Size
    /** Render a `router-link` (`to`) or a raw tag name. */
    as?: string
    to?: string
    type?: 'button' | 'submit' | 'reset'
    loading?: boolean
    disabled?: boolean
    block?: boolean
    icon?: IconName
    iconRight?: IconName
    /** Required on icon-only buttons for screen readers. */
    label?: string
  }>(),
  {
    variant: 'secondary',
    size: 'md',
    as: 'button',
    type: 'button',
    loading: false,
    disabled: false,
    block: false,
  },
)

const tag = computed(() => (props.as === 'router-link' ? RouterLink : props.as))
const isNativeButton = computed(() => tag.value === 'button')
const inactive = computed(() => props.disabled || props.loading)
const iconSize = computed(() => (props.size === 'sm' ? 14 : props.size === 'lg' ? 18 : 16))
</script>

<template>
  <component
    :is="tag"
    :to="as === 'router-link' ? to : undefined"
    :type="isNativeButton ? type : undefined"
    :disabled="isNativeButton ? inactive : undefined"
    :aria-disabled="!isNativeButton && inactive ? 'true' : undefined"
    :aria-busy="loading ? 'true' : undefined"
    :aria-label="label"
    :class="['bd-btn', `bd-btn--${variant}`, `bd-btn--${size}`, { 'bd-btn--block': block }]"
  >
    <BaseSpinner v-if="loading" :size="iconSize - 2" />
    <Icon v-else-if="icon" :name="icon" :size="iconSize" />
    <span v-if="label || $slots.default" class="bd-btn__label"
      ><slot>{{ label }}</slot></span
    >
    <Icon v-if="iconRight" :name="iconRight" :size="iconSize" />
  </component>
</template>

<style scoped>
.bd-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  border-radius: 8px;
  border: 1px solid transparent;
  font-weight: 550;
  white-space: nowrap;
  cursor: pointer;
  user-select: none;
  transition:
    background-color 160ms var(--ease-ui),
    border-color 160ms var(--ease-ui),
    color 160ms var(--ease-ui),
    transform 120ms var(--ease-ui),
    box-shadow 160ms var(--ease-ui);
}
.bd-btn:focus-visible {
  outline: 2px solid var(--app-accent);
  outline-offset: 2px;
}
.bd-btn:active:not(:disabled) {
  transform: translateY(1px);
}
.bd-btn:disabled,
.bd-btn[aria-disabled='true'] {
  opacity: 0.5;
  cursor: not-allowed;
  transform: none;
}

.bd-btn--sm {
  height: 30px;
  padding: 0 10px;
  font-size: 12.5px;
}
.bd-btn--md {
  height: 36px;
  padding: 0 14px;
  font-size: 13.5px;
}
.bd-btn--lg {
  height: 42px;
  padding: 0 20px;
  font-size: 14.5px;
  border-radius: 10px;
}
.bd-btn--block {
  width: 100%;
}

.bd-btn--primary {
  background-image: linear-gradient(
    100deg,
    var(--app-accent) 0%,
    var(--app-accent-2) 100%
  );
  color: var(--app-accent-contrast);
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.18);
}
.bd-btn--primary:hover:not(:disabled) {
  filter: brightness(1.07);
}
.bd-btn--primary:active:not(:disabled) {
  filter: brightness(0.96);
}

.bd-btn--secondary {
  background-color: var(--app-surface-2);
  border-color: var(--app-border);
  color: var(--app-text);
}
.bd-btn--secondary:hover:not(:disabled) {
  background-color: var(--app-surface-hover);
  border-color: var(--app-border-strong);
}

.bd-btn--ghost {
  background-color: transparent;
  color: var(--app-text-muted);
}
.bd-btn--ghost:hover:not(:disabled) {
  background-color: var(--app-surface-hover);
  color: var(--app-text);
}

.bd-btn--danger {
  background-color: transparent;
  border-color: color-mix(in srgb, var(--app-danger) 34%, transparent);
  color: var(--app-danger);
}
.bd-btn--danger:hover:not(:disabled) {
  background-color: color-mix(in srgb, var(--app-danger) 12%, transparent);
  border-color: color-mix(in srgb, var(--app-danger) 55%, transparent);
}

.bd-btn__label {
  display: inline-block;
}
</style>
