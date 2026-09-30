<script setup lang="ts">
import { computed } from 'vue'
import BaseBadge from '@/components/ui/BaseBadge.vue'
import { useStatusLabel } from '@/composables/useStatus'
import type { FileStatus } from '@/api/types'

const props = defineProps<{ status: FileStatus | string; size?: 'sm' | 'md' }>()

const { fileLabel, tone } = useStatusLabel()
const label = computed(() => fileLabel(props.status))
</script>

<template>
  <BaseBadge :tone="tone(status)" dot :pulse="status === 'downloading'" :size="size ?? 'sm'">
    {{ label }}
  </BaseBadge>
</template>
