<script setup lang="ts">
import { computed } from 'vue'
import BaseBadge from '@/components/ui/BaseBadge.vue'
import { useStatusLabel } from '@/composables/useStatus'
import type { Tone } from '@/composables/useStatus'
import type { TaskStatus } from '@/api/types'

const props = defineProps<{ status: TaskStatus | string; size?: 'sm' | 'md' }>()

const { taskLabel, tone } = useStatusLabel()
const label = computed(() => taskLabel(props.status))
const toneV = computed<Tone>(() => tone(props.status))
</script>

<template>
  <BaseBadge :tone="toneV" dot :pulse="status === 'crawling' || status === 'running'" :size="size ?? 'sm'">
    {{ label }}
  </BaseBadge>
</template>
