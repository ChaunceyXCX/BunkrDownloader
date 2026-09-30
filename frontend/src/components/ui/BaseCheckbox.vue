<script setup lang="ts">
import { computed, useId } from 'vue'
import Icon from './Icon.vue'

const props = defineProps<{
  modelValue: boolean
  label?: string
  hint?: string
  disabled?: boolean
}>()

const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()

const uid = useId()
const inputId = computed(() => `bd-cb-${uid}`)
const hintId = computed(() => (props.hint ? `bd-cb-hint-${uid}` : undefined))
</script>

<template>
  <div class="bd-check">
    <input
      :id="inputId"
      class="bd-check__native"
      type="checkbox"
      :checked="modelValue"
      :disabled="disabled"
      :aria-describedby="hintId"
      @change="emit('update:modelValue', ($event.target as HTMLInputElement).checked)"
    />
    <label :for="inputId" class="bd-check__label">
      <span class="bd-check__box" aria-hidden="true">
        <Icon v-if="modelValue" name="check" :size="12" :stroke-width="2.8" />
      </span>
      <span class="bd-check__text">
        <slot>{{ label }}</slot>
      </span>
    </label>
    <p v-if="hint" :id="hintId" class="bd-check__hint">{{ hint }}</p>
  </div>
</template>

<style scoped>
.bd-check {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
/* The native input stays in the accessibility tree but is visually replaced. */
.bd-check__native {
  position: absolute;
  width: 1px;
  height: 1px;
  opacity: 0;
  margin: 0;
  pointer-events: none;
}
.bd-check__label {
  display: inline-flex;
  align-items: flex-start;
  gap: 9px;
  cursor: pointer;
  font-size: 13px;
  line-height: 1.5;
  color: var(--app-text);
  user-select: none;
}
.bd-check__box {
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 17px;
  height: 17px;
  margin-top: 1px;
  border-radius: 5px;
  border: 1px solid var(--app-border-strong);
  background-color: var(--app-surface);
  color: var(--app-accent-contrast);
  transition:
    background-color 150ms var(--ease-ui),
    border-color 150ms var(--ease-ui);
}
.bd-check__native:checked + .bd-check__label .bd-check__box {
  background-image: linear-gradient(120deg, var(--app-accent), var(--app-accent-2));
  border-color: transparent;
}
.bd-check__native:focus-visible + .bd-check__label .bd-check__box {
  outline: 2px solid var(--app-accent);
  outline-offset: 2px;
}
.bd-check__native:disabled + .bd-check__label {
  opacity: 0.55;
  cursor: not-allowed;
}
.bd-check__hint {
  margin: 0 0 0 26px;
  font-size: 11.5px;
  color: var(--app-text-dim);
}
</style>
