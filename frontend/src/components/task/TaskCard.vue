<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import type { Task, TaskStatus } from '@/api/types'
import { useI18nStore } from '@/stores/i18n'
import { useFormat } from '@/composables/useFormat'
import Icon from '@/components/ui/Icon.vue'
import BaseProgress from '@/components/ui/BaseProgress.vue'
import BaseDropdown from '@/components/ui/BaseDropdown.vue'
import type { DropdownItem } from '@/components/ui/BaseDropdown.vue'
import TaskStatusBadge from './TaskStatusBadge.vue'

export type TaskAction = 'start' | 'pause' | 'resume' | 'retry' | 'cancel' | 'delete'

const props = defineProps<{
  task: Task
  compact?: boolean
}>()

const emit = defineEmits<{
  action: [action: TaskAction]
}>()

const i18n = useI18nStore()
const { formatBytes, formatSpeed, formatPercent, formatRelativeTime, formatCount } = useFormat(
  () => i18n.locale,
)

const running = computed(() => props.task.status === 'running' || props.task.status === 'crawling')
const name = computed(() => props.task.album_name || props.task.url)
const active = computed(
  () => props.task.status === 'running' || props.task.status === 'crawling' || props.task.status === 'paused',
)

const progress = computed(() => Math.min(100, props.task.progress))
const speed = computed(() => formatSpeed(props.task.speed))
const sizeText = computed(() =>
  props.task.total_bytes > 0 ? formatBytes(props.task.downloaded_bytes) : '—',
)
const sizeTotal = computed(() => (props.task.total_bytes > 0 ? formatBytes(props.task.total_bytes) : '—'))
const filesDone = computed(() => formatCount(props.task.completed_files))
const filesTotal = computed(() => formatCount(props.task.total_files))
const createdText = computed(() =>
  formatRelativeTime(props.task.created_at) === '—'
    ? '—'
    : i18n.t('task.createdAt', { time: formatRelativeTime(props.task.created_at) }),
)

const actions = computed<DropdownItem[]>(() => {
  const s = props.task.status
  const list: DropdownItem[] = []
  if (s === 'pending' || s === 'failed' || s === 'canceled') {
    list.push({ key: 'start', label: i18n.t('task.start'), icon: 'play' })
  }
  if (s === 'running' || s === 'crawling') {
    list.push({ key: 'pause', label: i18n.t('task.pause'), icon: 'pause' })
  }
  if (s === 'paused') {
    list.push({ key: 'resume', label: i18n.t('task.resume'), icon: 'play' })
  }
  if (s === 'failed' || s === 'completed' || s === 'paused') {
    list.push({ key: 'retry', label: i18n.t('task.retry'), icon: 'refresh' })
  }
  list.push(
    { key: 'sep1', type: 'separator' },
    { key: 'cancel', label: i18n.t('task.cancel'), icon: 'x' },
    { key: 'delete', label: i18n.t('task.delete'), icon: 'trash', tone: 'danger' },
  )
  return list
})

function onAction(item: DropdownItem): void {
  const k = item.key as TaskAction
  emit('action', k)
}
</script>

<template>
  <div class="bd-task card card-hover" :class="{ 'is-active': active }">
    <div class="bd-task__row">
      <RouterLink
        :to="`/app/tasks/${task.id}`"
        class="bd-task__main"
        :title="task.url"
      >
        <span class="bd-task__icon" aria-hidden="true">
          <Icon :name="task.kind === 'item' ? 'file' : 'folder'" :size="16" :stroke-width="1.7" />
        </span>
        <span class="bd-task__info">
          <span class="bd-task__name">{{ name }}</span>
          <span class="bd-task__url">{{ task.url }}</span>
        </span>
      </RouterLink>

      <span class="bd-task__status"><TaskStatusBadge :status="task.status" /></span>
      <BaseDropdown :items="actions" :width="170" :label="i18n.t('task.action')" @select="onAction">
        <template #trigger="{ toggle, open, controls }">
          <button
            type="button"
            class="bd-task__menu"
            :aria-label="`${i18n.t('task.action')}: ${name}`"
            :aria-expanded="open"
            :aria-controls="controls"
            aria-haspopup="menu"
            @click="toggle"
          >
            <Icon name="chevron-down" :size="16" />
          </button>
        </template>
      </BaseDropdown>
    </div>

    <div class="bd-task__meta">
      <span class="bd-task__files" data-numeric>
        <Icon name="database" :size="13" />
        {{ filesDone }}<template v-if="task.total_files > 0"> / {{ filesTotal }}</template>
      </span>
      <span class="bd-task__size" data-numeric>
        {{ sizeText }}<template v-if="task.total_bytes > 0"> / {{ sizeTotal }}</template>
      </span>
      <span v-if="running" class="bd-task__speed" data-numeric>
        <Icon name="zap" :size="13" class="is-accent" />
        {{ speed }}
      </span>
      <span class="bd-task__time">{{ createdText }}</span>
    </div>

    <div class="bd-task__bar">
      <BaseProgress
        :value="progress"
        :active="task.status === 'running'"
        :height="5"
        :label="`${task.album_name || task.url}: ${formatPercent(progress)}`"
      />
    </div>
  </div>
</template>

<style scoped>
.bd-task {
  padding: 13px 14px 12px;
  display: flex;
  flex-direction: column;
  gap: 9px;
}
.is-active {
  border-left: 2px solid var(--app-accent);
}
.bd-task__row {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
}
.bd-task__main {
  display: flex;
  align-items: center;
  gap: 11px;
  min-width: 0;
  flex: 1 1 auto;
  color: inherit;
  text-decoration: none;
}
.bd-task__icon {
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border-radius: 9px;
  color: var(--app-accent);
  background-color: var(--app-accent-soft);
}
.bd-task__info {
  display: flex;
  flex-direction: column;
  min-width: 0;
  gap: 1px;
}
.bd-task__name {
  font-size: 13.5px;
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.bd-task__url {
  font-size: 11.5px;
  color: var(--app-text-dim);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.bd-task__status {
  flex: 0 0 auto;
}
.bd-task__menu {
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: 8px;
  border: 0;
  background: transparent;
  color: var(--app-text-dim);
  cursor: pointer;
  transition:
    background-color 150ms var(--ease-ui),
    color 150ms var(--ease-ui);
}
.bd-task__menu:hover {
  background-color: var(--app-surface-hover);
  color: var(--app-text);
}
.bd-task__meta {
  display: flex;
  align-items: center;
  gap: 14px;
  font-size: 11.5px;
  color: var(--app-text-muted);
  flex-wrap: nowrap;
  overflow: hidden;
  white-space: nowrap;
}
.bd-task__meta > span {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  flex: 0 0 auto;
}
.bd-task__speed .is-accent {
  color: var(--app-accent);
}
.bd-task__time {
  margin-left: auto;
  color: var(--app-text-dim);
  overflow: hidden;
  text-overflow: ellipsis;
}
.bd-task__bar {
  min-width: 0;
}
</style>
