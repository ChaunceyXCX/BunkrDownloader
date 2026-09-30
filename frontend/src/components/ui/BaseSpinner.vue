<script setup lang="ts">
withDefaults(defineProps<{ size?: number | string; label?: string }>(), { size: 18 })
</script>

<template>
  <span
    class="bd-spinner"
    :style="{ width: `${size}px`, height: `${size}px` }"
    role="status"
    :aria-label="label ?? undefined"
  >
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round">
      <circle cx="12" cy="12" r="9" opacity="0.22" />
      <path d="M21 12a9 9 0 0 0-9-9" />
    </svg>
  </span>
</template>

<style scoped>
.bd-spinner {
  display: inline-flex;
  color: currentColor;
}
.bd-spinner svg {
  width: 100%;
  height: 100%;
  animation: bd-spin 720ms linear infinite;
}
</style>
