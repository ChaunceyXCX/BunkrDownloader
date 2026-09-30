<script setup lang="ts">
withDefaults(
  defineProps<{
    /** Any CSS length; height is derived when omitted. */
    width?: string
    height?: string
    rounded?: string
  }>(),
  { width: '100%', height: '14px', rounded: '7px' },
)
</script>

<template>
  <span
    class="bd-skeleton"
    aria-hidden="true"
    :style="{ width, height, borderRadius: rounded }"
  />
</template>

<style scoped>
.bd-skeleton {
  display: block;
  background-color: var(--app-surface-3);
  background-image: linear-gradient(
    90deg,
    transparent,
    color-mix(in srgb, var(--app-text) 5%, transparent),
    transparent
  );
  background-size: 220% 100%;
  animation: bd-skeleton 1.5s var(--ease-ui) infinite;
}
@keyframes bd-skeleton {
  from {
    background-position: 140% 0;
  }
  to {
    background-position: -40% 0;
  }
}
</style>
