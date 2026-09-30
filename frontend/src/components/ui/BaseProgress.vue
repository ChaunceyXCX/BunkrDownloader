<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    /** Fraction 0–1. */
    value: number
    /** Adds the moving stripe overlay while the job is live. */
    active?: boolean
    /** `indeterminate` renders a sweeping bar for unknown totals. */
    indeterminate?: boolean
    height?: number
    tone?: 'accent' | 'success' | 'warn' | 'danger'
    label?: string
    showValue?: boolean
    valueText?: string
  }>(),
  { height: 6, tone: 'accent', active: false, indeterminate: false, showValue: false },
)

const pct = computed(() => {
  if (props.indeterminate) return 0
  const v = Number.isFinite(props.value) ? props.value : 0
  return Math.max(0, Math.min(100, v))
})
const toneClass = computed(() => `bd-progress--${props.tone}`)
</script>

<template>
  <div class="bd-progress-wrap">
    <div
      class="bd-progress"
      :class="[toneClass, { 'bd-progress-live': active, 'bd-progress--indeterminate': indeterminate }]"
      :style="{ height: `${height}px` }"
      role="progressbar"
      :aria-valuenow="indeterminate ? undefined : Math.round(pct)"
      :aria-valuemin="0"
      :aria-valuemax="100"
      :aria-label="label"
    >
      <div
        class="bd-progress-fill"
        :style="indeterminate ? undefined : { width: `${pct}%` }"
      />
    </div>
    <span v-if="showValue" class="bd-progress__text" data-numeric>{{ valueText ?? `${Math.round(pct)}%` }}</span>
  </div>
</template>

<style scoped>
.bd-progress-wrap {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.bd-progress {
  position: relative;
  flex: 1 1 auto;
  min-width: 0;
  border-radius: 999px;
  background-color: var(--app-surface-3);
  overflow: hidden;
  box-shadow: inset 0 0 0 1px var(--app-border);
}
.bd-progress-fill {
  height: 100%;
  border-radius: 999px;
  background-image: linear-gradient(90deg, var(--app-accent), var(--app-accent-2));
  transition: width 220ms var(--ease-ui);
}
.bd-progress--success .bd-progress-fill {
  background-image: linear-gradient(90deg, var(--app-success), color-mix(in srgb, var(--app-success) 65%, var(--app-accent-3)));
}
.bd-progress--warn .bd-progress-fill {
  background-image: linear-gradient(90deg, var(--app-warn), color-mix(in srgb, var(--app-warn) 60%, var(--app-danger)));
}
.bd-progress--danger .bd-progress-fill {
  background-image: linear-gradient(90deg, var(--app-danger), color-mix(in srgb, var(--app-danger) 60%, #b91c1c));
}
.bd-progress--indeterminate .bd-progress-fill {
  width: 38%;
  animation: bd-indeterminate 1.25s var(--ease-ui) infinite;
}
.bd-progress__text {
  flex: 0 0 auto;
  font-size: 11.5px;
  color: var(--app-text-muted);
  min-width: 34px;
  text-align: right;
}
</style>
