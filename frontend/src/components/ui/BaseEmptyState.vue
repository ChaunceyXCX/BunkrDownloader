<script setup lang="ts">
import { computed } from 'vue'
import Icon from './Icon.vue'
import type { IconName } from './Icon.vue'
import type { Tone } from '@/composables/useStatus'
import { TONE_COLOR } from '@/composables/useStatus'

const props = withDefaults(
  defineProps<{
    icon?: IconName
    title: string
    hint?: string
    tone?: Tone
    compact?: boolean
  }>(),
  { tone: 'neutral', compact: false },
)

const styleVars = computed(() => ({ '--bd-tone': TONE_COLOR[props.tone] }))
</script>

<template>
  <div class="bd-empty" :class="{ 'bd-empty--compact': compact }">
    <span class="bd-empty__mark" aria-hidden="true">
      <Icon :name="icon ?? 'inbox'" :size="compact ? 18 : 22" :stroke-width="1.6" />
    </span>
    <p class="bd-empty__title">{{ title }}</p>
    <p v-if="hint" class="bd-empty__hint">{{ hint }}</p>
    <div v-if="$slots.default" class="bd-empty__cta"><slot /></div>
  </div>
</template>

<style scoped>
.bd-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  text-align: center;
  padding: 44px 24px;
  gap: 6px;
}
.bd-empty--compact {
  padding: 26px 18px;
}
.bd-empty__mark {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 46px;
  height: 46px;
  margin-bottom: 4px;
  border-radius: 12px;
  color: var(--bd-tone);
  background-color: color-mix(in srgb, var(--bd-tone) 10%, transparent);
  border: 1px solid color-mix(in srgb, var(--bd-tone) 20%, transparent);
}
.bd-empty--compact .bd-empty__mark {
  width: 36px;
  height: 36px;
  border-radius: 10px;
}
.bd-empty__title {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  color: var(--app-text);
}
.bd-empty__hint {
  margin: 0;
  max-width: 42ch;
  font-size: 12.5px;
  color: var(--app-text-muted);
}
.bd-empty__cta {
  margin-top: 10px;
  display: flex;
  gap: 8px;
}
</style>
