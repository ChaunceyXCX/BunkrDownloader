<script setup lang="ts">
import { computed } from 'vue'
import Icon from './Icon.vue'
import type { IconName } from './Icon.vue'
import type { Tone } from '@/composables/useStatus'
import { TONE_COLOR } from '@/composables/useStatus'

const props = withDefaults(
  defineProps<{
    label: string
    value: string
    icon?: IconName
    tone?: Tone
    /** Small delta line under the value, e.g. `+3 today`. */
    delta?: string
    deltaTone?: 'up' | 'down' | 'flat'
    active?: boolean
  }>(),
  { tone: 'accent', deltaTone: 'flat', active: false },
)

const styleVars = computed(() => ({ '--bd-tone': TONE_COLOR[props.tone] }))
</script>

<template>
  <div class="bd-stat card" :class="{ 'is-active': active }" :style="styleVars">
    <div class="bd-stat__top">
      <span class="bd-stat__label">{{ label }}</span>
      <span v-if="icon" class="bd-stat__icon" aria-hidden="true">
        <Icon :name="icon" :size="15" :stroke-width="1.7" />
      </span>
    </div>
    <p class="bd-stat__value" data-numeric>{{ value }}</p>
    <p v-if="delta" class="bd-stat__delta" :class="`is-${deltaTone}`" data-numeric>
      <Icon
        v-if="deltaTone === 'up'"
        name="arrow-up"
        :size="12"
        :stroke-width="2.2"
      />
      <Icon
        v-else-if="deltaTone === 'down'"
        name="arrow-down"
        :size="12"
        :stroke-width="2.2"
      />
      <span>{{ delta }}</span>
    </p>
  </div>
</template>

<style scoped>
.bd-stat {
  position: relative;
  padding: 12px 14px 13px;
  min-width: 0;
  overflow: hidden;
}
.bd-stat::before {
  content: '';
  position: absolute;
  inset: 0 auto 0 0;
  width: 2px;
  background-color: var(--bd-tone);
  opacity: 0;
  transition: opacity 180ms var(--ease-ui);
}
.bd-stat.is-active::before,
.bd-stat:hover::before {
  opacity: 0.85;
}
.bd-stat__top {
  display: flex;
  align-items: center;
  gap: 8px;
}
.bd-stat__label {
  font-size: 11.5px;
  font-weight: 600;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: var(--app-text-dim);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.bd-stat__icon {
  margin-left: auto;
  display: inline-flex;
  color: var(--bd-tone);
  opacity: 0.9;
}
.bd-stat__value {
  margin: 4px 0 0;
  font-size: 22px;
  font-weight: 620;
  line-height: 1.15;
  letter-spacing: -0.02em;
  color: var(--app-text);
  font-variant-numeric: tabular-nums;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.bd-stat__delta {
  margin: 3px 0 0;
  display: flex;
  align-items: center;
  gap: 3px;
  font-size: 11.5px;
  color: var(--app-text-dim);
}
.bd-stat__delta.is-up {
  color: var(--app-success);
}
.bd-stat__delta.is-down {
  color: var(--app-danger);
}
</style>
