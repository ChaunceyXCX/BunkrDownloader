<script setup lang="ts">
import { computed, useId } from 'vue'
import Icon from './Icon.vue'

export interface SelectOption {
  value: string | number
  label: string
  disabled?: boolean
}

const props = withDefaults(
  defineProps<{
    modelValue: string | number
    options: SelectOption[]
    label?: string
    hint?: string
    error?: string
    disabled?: boolean
    size?: 'sm' | 'md'
  }>(),
  { size: 'md' },
)

const emit = defineEmits<{ 'update:modelValue': [value: string] }>()

const uid = useId()
const selectId = computed(() => (props.label ? `bd-sel-${uid}` : undefined))
const msgId = computed(() => (props.hint || props.error ? `bd-sel-msg-${uid}` : undefined))
</script>

<template>
  <div class="bd-field">
    <label v-if="label" :for="selectId" class="bd-field__label">{{ label }}</label>
    <div class="bd-select" :class="[`bd-select--${size}`, { 'is-bad': Boolean(error) }]">
      <select
        :id="selectId"
        class="bd-select__el"
        :value="modelValue"
        :disabled="disabled"
        :aria-invalid="Boolean(error) || undefined"
        :aria-describedby="msgId"
        @change="emit('update:modelValue', ($event.target as HTMLSelectElement).value)"
      >
        <option
          v-for="opt in options"
          :key="opt.value"
          :value="opt.value"
          :disabled="opt.disabled"
        >
          {{ opt.label }}
        </option>
      </select>
      <Icon name="chevron-down" :size="15" class="bd-select__caret" />
    </div>
    <p v-if="error || hint" :id="msgId" class="bd-field__msg" :class="{ 'is-error': Boolean(error) }">
      {{ error || hint }}
    </p>
  </div>
</template>

<style scoped>
.bd-field {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}
.bd-field__label {
  font-size: 12.5px;
  font-weight: 550;
  color: var(--app-text-muted);
}
.bd-select {
  position: relative;
  display: flex;
  align-items: center;
  border-radius: 8px;
  background-color: var(--app-surface);
  border: 1px solid var(--app-border);
  transition:
    border-color 150ms var(--ease-ui),
    box-shadow 150ms var(--ease-ui);
}
.bd-select--sm {
  height: 30px;
}
.bd-select--md {
  height: 36px;
}
.bd-select:hover:not([data-disabled]) {
  border-color: var(--app-border-strong);
}
.bd-select:focus-within {
  border-color: var(--app-accent);
  box-shadow: 0 0 0 3px var(--app-accent-soft);
}
.bd-select.is-bad {
  border-color: color-mix(in srgb, var(--app-danger) 55%, transparent);
}
.bd-select__el {
  appearance: none;
  width: 100%;
  height: 100%;
  padding: 0 30px 0 10px;
  background: transparent;
  border: 0;
  outline: none;
  font-size: 13px;
  cursor: pointer;
  border-radius: inherit;
}
.bd-select__el:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}
.bd-select__el option {
  background-color: var(--app-surface);
  color: var(--app-text);
}
.bd-select__caret {
  position: absolute;
  right: 9px;
  color: var(--app-text-dim);
  pointer-events: none;
}
.bd-field__msg {
  margin: 0;
  font-size: 11.5px;
  color: var(--app-text-dim);
}
.bd-field__msg.is-error {
  color: var(--app-danger);
}
</style>
