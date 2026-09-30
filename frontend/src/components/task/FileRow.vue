<script setup lang="ts">
import { computed } from 'vue'
import type { DownloadFile } from '@/api/types'
import { useI18nStore } from '@/stores/i18n'
import { useFormat } from '@/composables/useFormat'
import Icon from '@/components/ui/Icon.vue'
import BaseTooltip from '@/components/ui/BaseTooltip.vue'
import BaseProgress from '@/components/ui/BaseProgress.vue'
import FileStatusBadge from './FileStatusBadge.vue'

const props = defineProps<{ file: DownloadFile }>()
const emit = defineEmits<{ retry: [fileId: number] }>()

const i18n = useI18nStore()
const { formatBytes, formatSpeed, formatPercent, formatRelativeTime } = useFormat(() => i18n.locale)

const sizeText = computed(() => (props.file.file_size > 0 ? formatBytes(props.file.file_size) : '—'))
const downloadedText = computed(() =>
  props.file.downloaded_bytes > 0 ? formatBytes(props.file.downloaded_bytes) : '—',
)
const speedText = computed(() => (props.file.status === 'downloading' ? formatSpeed(props.file.speed) : '—'))
const progress = computed(() => Math.min(100, props.file.progress))
const relative = computed(() => formatRelativeTime(props.file.created_at))
const title = computed(() => props.file.filename)
</script>

<template>
  <BaseTooltip :text="title" placement="top">
    <div class="bd-filerow">
      <span class="bd-filerow__name" :title="title">{{ file.filename }}</span>

      <span class="bd-filerow__cell bd-filerow__size" data-numeric>{{ sizeText }}</span>
      <span class="bd-filerow__cell bd-filerow__dl" data-numeric>
        {{ file.status === 'completed' ? sizeText : downloadedText }}
      </span>
      <span class="bd-filerow__cell bd-filerow__speed" data-numeric>{{ speedText }}</span>
      <span class="bd-filerow__cell bd-filerow__status"><FileStatusBadge :status="file.status" size="sm" /></span>
      <button
        v-if="file.status === 'failed'"
        type="button"
        class="bd-filerow__retry"
        :aria-label="`${i18n.t('file.retry')}: ${file.filename}`"
        :title="i18n.t('file.retry')"
        @click="emit('retry', file.id)"
      >
        <Icon name="refresh" :size="14" />
      </button>
      <span v-else class="bd-filerow__retryslot" />
    </div>
    <div v-if="file.status === 'downloading'" class="bd-filerow__bar">
      <BaseProgress :value="progress" active :height="4" :label="`${file.filename}: ${formatPercent(progress)}`" />
    </div>
  </BaseTooltip>
</template>

<style scoped>
.bd-filerow {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 88px 84px 74px 92px 28px;
  align-items: center;
  gap: 10px;
  min-height: 42px;
  padding: 0 12px;
  border-bottom: 1px solid var(--app-border);
  font-size: 12.5px;
}
.bd-filerow:hover {
  background-color: var(--app-surface-hover);
}
.bd-filerow__name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-weight: 500;
  color: var(--app-text);
}
.bd-filerow__cell {
  color: var(--app-text-muted);
  text-align: right;
  white-space: nowrap;
  font-size: 12px;
}
.bd-filerow__status {
  text-align: right;
}
.bd-filerow__retry {
  justify-self: end;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  border-radius: 7px;
  border: 1px solid var(--app-border);
  background-color: var(--app-surface-2);
  color: var(--app-text-muted);
  cursor: pointer;
  transition:
    color 150ms var(--ease-ui),
    border-color 150ms var(--ease-ui),
    background-color 150ms var(--ease-ui);
}
.bd-filerow__retry:hover {
  color: var(--app-accent);
  border-color: color-mix(in srgb, var(--app-accent) 40%, transparent);
  background-color: var(--app-accent-soft);
}
.bd-filerow__retryslot {
  width: 26px;
}
.bd-filerow__bar {
  padding: 0 12px 6px;
}

@media (max-width: 767px) {
  .bd-filerow {
    grid-template-columns: 44px minmax(0, 1fr) auto;
    grid-template-areas:
      'st name name'
      'st size speed'
      'st dl  dl';
    gap: 4px 10px;
    padding: 9px 12px;
  }
  .bd-filerow__name {
    grid-area: name;
  }
  .bd-filerow__size {
    grid-area: size;
    text-align: left;
  }
  .bd-filerow__dl {
    grid-area: dl;
    text-align: left;
  }
  .bd-filerow__speed {
    grid-area: speed;
    text-align: right;
  }
  .bd-filerow__status {
    grid-area: st;
    text-align: left;
  }
}
</style>
