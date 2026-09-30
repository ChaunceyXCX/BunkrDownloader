<script setup lang="ts">
import { ref } from 'vue'

/**
 * CSS-only tooltip: the trigger keeps focus/keyboard reachability and the
 * bubble fades in on hover *and* `:focus-visible`.
 */
withDefaults(defineProps<{ text: string; placement?: 'top' | 'bottom' | 'left' | 'right' }>(), {
  placement: 'top',
})
const open = ref(false)
</script>

<template>
  <span
    class="bd-tip"
    :class="[`bd-tip--${placement}`, { 'is-open': open }]"
    @mouseenter="open = true"
    @mouseleave="open = false"
    @focusin="open = true"
    @focusout="open = false"
  >
    <span class="bd-tip__trigger"><slot /></span>
    <span class="bd-tip__bubble" role="tooltip">{{ text }}</span>
  </span>
</template>

<style scoped>
.bd-tip {
  position: relative;
  display: inline-flex;
  max-width: 100%;
}
.bd-tip__trigger {
  display: inline-flex;
  min-width: 0;
}
.bd-tip__bubble {
  position: absolute;
  z-index: 70;
  padding: 5px 8px;
  border-radius: 7px;
  background-color: color-mix(in srgb, var(--app-text) 94%, transparent);
  color: var(--app-bg);
  font-size: 11.5px;
  font-weight: 500;
  line-height: 1.35;
  white-space: nowrap;
  max-width: 42ch;
  overflow: hidden;
  text-overflow: ellipsis;
  pointer-events: none;
  opacity: 0;
  transition:
    opacity 140ms var(--ease-ui),
    transform 140ms var(--ease-ui);
  box-shadow: var(--shadow-pop);
}
.bd-tip--top .bd-tip__bubble {
  bottom: calc(100% + 6px);
  left: 50%;
  transform: translate(-50%, 3px);
}
.bd-tip--bottom .bd-tip__bubble {
  top: calc(100% + 6px);
  left: 50%;
  transform: translate(-50%, -3px);
}
.bd-tip--left .bd-tip__bubble {
  right: calc(100% + 6px);
  top: 50%;
  transform: translate(3px, -50%);
}
.bd-tip--right .bd-tip__bubble {
  left: calc(100% + 6px);
  top: 50%;
  transform: translate(-3px, -50%);
}
.is-open .bd-tip__bubble {
  opacity: 1;
  transform: translate(-50%, 0);
}
.bd-tip--left.is-open .bd-tip__bubble,
.bd-tip--right.is-open .bd-tip__bubble {
  transform: translate(0, -50%);
}
@media (max-width: 640px) {
  .bd-tip__bubble {
    display: none;
  }
}
</style>
