<script setup lang="ts">
import { computed, useId } from 'vue'

const props = withDefaults(
  defineProps<{
    modelValue: string
    label?: string
    placeholder?: string
    hint?: string
    error?: string
    rows?: number
    disabled?: boolean
    required?: boolean
    monospace?: boolean
    /** Rendered under the field, e.g. per-line validation markers. */
    invalid?: boolean
  }>(),
  { rows: 5 },
)

const emit = defineEmits<{ 'update:modelValue': [value: string] }>()

const uid = useId()
const inputId = computed(() => (props.label ? `bd-ta-${uid}` : undefined))
const msgId = computed(() => (props.hint || props.error ? `bd-ta-msg-${uid}` : undefined))
const bad = computed(() => Boolean(props.error) || Boolean(props.invalid))
</script>

<template>
  <div class="bd-field">
    <label v-if="label" :for="inputId" class="bd-field__label">
      {{ label }}
      <span v-if="required" class="bd-field__req" aria-hidden="true">*</span>
    </label>
    <textarea
      :id="inputId"
      class="bd-ta"
      :class="{ 'is-bad': bad, 'is-mono': monospace }"
      :rows="rows"
      :value="modelValue"
      :placeholder="placeholder"
      :disabled="disabled"
      :required="required"
      :aria-invalid="bad || undefined"
      :aria-describedby="msgId"
      spellcheck="false"
      @input="emit('update:modelValue', ($event.target as HTMLTextAreaElement).value)"
    />
    <p v-if="error || hint" :id="msgId" class="bd-field__msg" :class="{ 'is-error': bad }">
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
.bd-ta {
  width: 100%;
  resize: vertical;
  padding: 9px 11px;
  border-radius: 8px;
  background-color: var(--app-surface);
  border: 1px solid var(--app-border);
  color: var(--app-text);
  font-size: 13px;
  line-height: 1.6;
  outline: none;
  transition:
    border-color 150ms var(--ease-ui),
    box-shadow 150ms var(--ease-ui);
}
.bd-ta::placeholder {
  color: var(--app-text-dim);
}
.bd-ta:hover:not(:disabled) {
  border-color: var(--app-border-strong);
}
.bd-ta:focus-visible,
.bd-ta:focus {
  border-color: var(--app-accent);
  box-shadow: 0 0 0 3px var(--app-accent-soft);
}
.bd-ta.is-bad {
  border-color: color-mix(in srgb, var(--app-danger) 55%, transparent);
}
.bd-ta.is-mono {
  font-family: var(--font-mono);
  font-size: 12.5px;
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
