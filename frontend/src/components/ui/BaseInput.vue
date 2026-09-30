<script setup lang="ts">
import { computed, useId } from 'vue'
import Icon from './Icon.vue'
import type { IconName } from './Icon.vue'

const props = withDefaults(
  defineProps<{
    modelValue: string | number
    type?: string
    label?: string
    placeholder?: string
    hint?: string
    error?: string
    icon?: IconName
    disabled?: boolean
    required?: boolean
    min?: number
    max?: number
    step?: number
    autocomplete?: string
    inputmode?: 'text' | 'numeric' | 'decimal' | 'email' | 'url' | 'tel' | 'search'
  }>(),
  { type: 'text' },
)

const emit = defineEmits<{
  'update:modelValue': [value: string | number]
  enter: []
}>()

const uid = useId()
const inputId = computed(() => props.label ? `bd-in-${uid}` : undefined)
const hintId = computed(() => (props.hint || props.error) ? `bd-in-hint-${uid}` : undefined)
const invalid = computed(() => Boolean(props.error))
</script>

<template>
  <div class="bd-field">
    <label v-if="label" :for="inputId" class="bd-field__label">
      {{ label }}
      <span v-if="required" class="bd-field__req" aria-hidden="true">*</span>
    </label>

    <div
      class="bd-input"
      :class="{ 'bd-input--invalid': invalid, 'bd-input--disabled': disabled, 'bd-input--with-icon': icon }"
    >
      <Icon v-if="icon" :name="icon" :size="16" class="bd-input__icon" />
      <input
        :id="inputId"
        class="bd-input__el"
        :type="type"
        :value="modelValue"
        :placeholder="placeholder"
        :disabled="disabled"
        :required="required"
        :min="min"
        :max="max"
        :step="step"
        :autocomplete="autocomplete"
        :inputmode="inputmode"
        :aria-invalid="invalid || undefined"
        :aria-describedby="hintId"
        @input="emit('update:modelValue', ($event.target as HTMLInputElement).value)"
        @keydown.enter="emit('enter')"
      />
      <div class="bd-input__affix"><slot name="suffix" /></div>
    </div>

    <p v-if="error || hint" :id="hintId" class="bd-field__msg" :class="{ 'is-error': invalid }">
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
.bd-field__req {
  color: var(--app-danger);
  margin-left: 2px;
}
.bd-input {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 36px;
  padding: 0 10px;
  border-radius: 8px;
  background-color: var(--app-surface);
  border: 1px solid var(--app-border);
  transition:
    border-color 150ms var(--ease-ui),
    box-shadow 150ms var(--ease-ui),
    background-color 150ms var(--ease-ui);
}
.bd-input:hover:not(.bd-input--disabled) {
  border-color: var(--app-border-strong);
}
.bd-input:focus-within {
  border-color: var(--app-accent);
  box-shadow: 0 0 0 3px var(--app-accent-soft);
}
.bd-input--invalid {
  border-color: color-mix(in srgb, var(--app-danger) 60%, transparent);
}
.bd-input--invalid:focus-within {
  border-color: var(--app-danger);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--app-danger) 16%, transparent);
}
.bd-input--disabled {
  opacity: 0.6;
  cursor: not-allowed;
}
.bd-input__el {
  flex: 1 1 auto;
  min-width: 0;
  height: 100%;
  background: transparent;
  border: 0;
  outline: none;
  font-size: 13.5px;
}
.bd-input__el::placeholder {
  color: var(--app-text-dim);
}
.bd-input__icon {
  color: var(--app-text-dim);
}
.bd-input__affix:empty {
  display: none;
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
