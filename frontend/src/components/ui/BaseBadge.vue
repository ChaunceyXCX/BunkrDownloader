<script setup lang="ts">
import { computed } from 'vue'
import type { Tone } from '@/composables/useStatus'
import { TONE_COLOR } from '@/composables/useStatus'

const props = withDefaults(
  defineProps<{
    tone?: Tone
    /** Renders a status dot before the label. */
    dot?: boolean
    pulse?: boolean
    size?: 'sm' | 'md'
  }>(),
  { tone: 'neutral', dot: false, pulse: false, size: 'md' },
)

const styleVars = computed(() => ({ '--bd-tone': TONE_COLOR[props.tone] }))
</script>

<template>
  <span :class="['bd-badge', `bd-badge--${size}`]" :style="styleVars">
    <span v-if="dot" class="bd-badge__dot" :class="{ 'bd-anim-pulse-dot': pulse }" />
    <slot />
  </span>
</template>

<style scoped>
.bd-badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  border-radius: 999px;
  background-color: color-mix(in srgb, var(--bd-tone) 12%, transparent);
  border: 1px solid color-mix(in srgb, var(--bd-tone) 26%, transparent);
  color: var(--bd-tone);
  font-weight: 550;
  white-space: nowrap;
  line-height: 1;
}
.bd-badge--sm {
  padding: 3px 8px;
  font-size: 11px;
}
.bd-badge--md {
  padding: 5px 10px;
  font-size: 12px;
}
.bd-badge__dot {
  width: 6px;
  height: 6px;
  border-radius: 999px;
  background-color: currentColor;
}
</style>
