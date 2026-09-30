<script setup lang="ts">
import { computed } from 'vue'
import type { Task } from '@/api/types'
import { useI18nStore } from '@/stores/i18n'
import { useFormat } from '@/composables/useFormat'
import Icon from '@/components/ui/Icon.vue'
import BaseProgress from '@/components/ui/BaseProgress.vue'
import BaseButton from '@/components/ui/BaseButton.vue'
import TaskStatusBadge from './TaskStatusBadge.vue'
import { etaSeconds } from '@/composables/useFormat'

const props = defineProps<{ task: Task }>()
const emit = defineEmits<{
  action: [action: 'start' | 'pause' | 'resume' | 'retry' | 'cancel' | 'delete']
}>()

const i18n = useI18nStore()
const { formatBytes, formatSpeed, formatPercent, formatDuration, formatCount } = useFormat(
  () => i18n.locale,
)

const running = computed(() => props.task.status === 'running')
const progress = computed(() => Math.min(100, props.task.progress))

const speed = computed(() => formatSpeed(props.task.speed))
const downloaded = computed(() => formatBytes(props.task.downloaded_bytes))
const totalBytes = computed(() => (props.task.total_bytes > 0 ? formatBytes(props.task.total_bytes) : '—'))
const remainingBytes = computed(() => Math.max(0, props.task.total_bytes - props.task.downloaded_bytes))
const eta = computed(() => {
  const s = etaSeconds(remainingBytes.value, props.task.speed)
  return s === null ? '—' : formatDuration(s)
})

const filesCount = computed(() => formatCount(props.task.total_files))

function emitAction(a: 'start' | 'pause' | 'resume' | 'retry' | 'cancel' | 'delete'): void {
  emit('action', a)
}
</script>

<template>
  <section class="bd-dhead card">
    <div class="bd-dhead__top">
      <div class="bd-dhead__main">
        <h2 class="bd-dhead__name" :title="task.url">
          {{ task.album_name || task.url }}
        </h2>
        <p class="bd-dhead__url">{{ task.url }}</p>
      </div>
      <TaskStatusBadge :status="task.status" size="md" />
    </div>

    <div class="bd-dhead__bar">
      <BaseProgress
        :value="progress"
        :active="running"
        :height="8"
        :label="`${i18n.t('task.overall')}: ${formatPercent(progress)}`"
        :show-value="true"
        :value-text="formatPercent(progress)"
      />
    </div>

    <div class="bd-dhead__stats">
      <div class="bd-dhead__stat" data-numeric>
        <span class="bd-dhead__key">{{ i18n.t('task.overall') }}</span>
        <span class="bd-dhead__val">{{ downloaded }} / {{ totalBytes }}</span>
      </div>
      <div class="bd-dhead__stat" data-numeric>
        <span class="bd-dhead__key">{{ i18n.t('task.speed') }}</span>
        <span class="bd-dhead__val is-accent">{{ speed }}</span>
      </div>
      <div class="bd-dhead__stat" data-numeric>
        <span class="bd-dhead__key">{{ i18n.t('task.eta') }}</span>
        <span class="bd-dhead__val">{{ eta }}</span>
      </div>
      <div class="bd-dhead__stat" data-numeric>
        <span class="bd-dhead__key">{{ i18n.t('task.counts') }}</span>
        <span class="bd-dhead__val">{{ task.completed_files }} / {{ filesCount }}</span>
      </div>
      <div v-if="task.error_message" class="bd-dhead__stat" data-numeric>
        <span class="bd-dhead__key">{{ i18n.t('task.errorMessage') }}</span>
        <span class="bd-dhead__val is-danger">{{ task.error_message }}</span>
      </div>
    </div>

    <div class="bd-dhead__actions">
      <BaseButton
        v-if="task.status === 'pending' || task.status === 'failed' || task.status === 'canceled'"
        variant="primary"
        size="sm"
        :icon="'play'"
        :label="i18n.t('task.start')"
        @click="emitAction('start')"
      >
        {{ i18n.t('task.start') }}
      </BaseButton>
      <BaseButton
        v-if="running"
        variant="secondary"
        size="sm"
        :icon="'pause'"
        :label="i18n.t('task.pause')"
        @click="emitAction('pause')"
      >
        {{ i18n.t('task.pause') }}
      </BaseButton>
      <BaseButton
        v-if="task.status === 'paused'"
        variant="secondary"
        size="sm"
        :icon="'play'"
        :label="i18n.t('task.resume')"
        @click="emitAction('resume')"
      >
        {{ i18n.t('task.resume') }}
      </BaseButton>
      <BaseButton
        v-if="task.status === 'paused' || task.status === 'failed' || task.status === 'completed'"
        variant="secondary"
        size="sm"
        :icon="'refresh'"
        :label="i18n.t('task.retry')"
        @click="emitAction('retry')"
      >
        {{ i18n.t('task.retry') }}
      </BaseButton>
      <div class="bd-dhead__spacer" />
      <BaseButton variant="ghost" size="sm" :icon="'x'" :label="i18n.t('task.cancel')" @click="emitAction('cancel')">
        {{ i18n.t('task.cancel') }}
      </BaseButton>
      <BaseButton variant="danger" size="sm" :icon="'trash'" :label="i18n.t('task.delete')" @click="emitAction('delete')">
        {{ i18n.t('task.delete') }}
      </BaseButton>
    </div>
  </section>
</template>

<style scoped>
.bd-dhead {
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.bd-dhead__top {
  display: flex;
  align-items: flex-start;
  gap: 12px;
}
.bd-dhead__main {
  min-width: 0;
  flex: 1 1 auto;
}
.bd-dhead__name {
  margin: 0;
  font-size: 16px;
  font-weight: 650;
  letter-spacing: -0.015em;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.bd-dhead__url {
  margin: 3px 0 0;
  font-size: 12px;
  color: var(--app-text-dim);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.bd-dhead__bar {
  min-width: 0;
}
.bd-dhead__stats {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
  gap: 10px;
}
.bd-dhead__stat {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 8px 10px;
  border-radius: 10px;
  background-color: var(--app-surface-3);
  min-width: 0;
}
.bd-dhead__key {
  font-size: 11px;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: var(--app-text-dim);
}
.bd-dhead__val {
  font-size: 13.5px;
  font-weight: 600;
  color: var(--app-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.bd-dhead__val.is-accent {
  color: var(--app-accent);
}
.bd-dhead__val.is-danger {
  color: var(--app-danger);
}
.bd-dhead__actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.bd-dhead__spacer {
  flex: 1 1 auto;
}
</style>
