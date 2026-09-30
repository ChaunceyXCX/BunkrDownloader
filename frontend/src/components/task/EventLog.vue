<script setup lang="ts">
import { nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useTasksStore } from '@/stores/tasks'
import { useI18nStore } from '@/stores/i18n'
import { useFormat } from '@/composables/useFormat'
import Icon from '@/components/ui/Icon.vue'
import BaseEmptyState from '@/components/ui/BaseEmptyState.vue'
import type { EventLevel } from '@/api/types'

const store = useTasksStore()
const i18n = useI18nStore()
const { formatDateTime } = useFormat(() => i18n.locale)

const body = ref<HTMLElement | null>(null)
const follow = ref(true)
const scrolledAway = ref(false)

function onScroll(): void {
  if (!body.value) return
  const el = body.value
  const dist = el.scrollHeight - el.scrollTop - el.clientHeight
  const nearBottom = dist < 48
  scrolledAway.value = !nearBottom
  if (nearBottom) follow.value = true
}

function jumpToLatest(): void {
  follow.value = true
  scrolledAway.value = false
  void nextTick(scrollBottom)
}

function scrollBottom(): void {
  if (body.value) body.value.scrollTop = body.value.scrollHeight
}

watch(
  () => store.events.length,
  () => {
    if (follow.value) void nextTick(scrollBottom)
  },
)

onMounted(() => {
  scrollBottom()
})

type Meta = { color: string; icon: 'check' | 'clock' | 'info' | 'alert-triangle' }
const LEVELS: Record<EventLevel, Meta> = {
  success: { color: 'var(--app-success)', icon: 'check' },
  info: { color: 'var(--app-info)', icon: 'clock' },
  warn: { color: 'var(--app-warn)', icon: 'alert-triangle' },
  error: { color: 'var(--app-danger)', icon: 'alert-triangle' },
}

function levelMeta(l: EventLevel): Meta {
  return LEVELS[l] ?? LEVELS.info
}
</script>

<template>
  <div class="bd-log card" :class="{ 'has-away': scrolledAway }">
    <header class="bd-log__head">
      <div class="bd-log__title">
        <Icon name="terminal" :size="15" />
        {{ i18n.t('event.title') }}
        <span class="bd-log__count" data-numeric>{{ store.events.length }}</span>
      </div>
      <button
        v-if="scrolledAway"
        type="button"
        class="bd-log__jump"
        @click="jumpToLatest"
      >
        <Icon name="arrow-down" :size="13" />
        {{ i18n.t('event.jumpToLatest') }}
      </button>
    </header>

    <div ref="body" class="bd-log__body" @scroll.passive="onScroll">
      <BaseEmptyState
        v-if="store.events.length === 0"
        :title="i18n.t('event.empty')"
        :hint="i18n.t('event.emptyHint')"
        icon="terminal"
        compact
      />
      <ul v-else class="bd-log__list">
        <li v-for="ev in store.events" :key="ev.id" class="bd-log__row">
          <span class="bd-log__dot" :style="{ background: levelMeta(ev.level).color }" aria-hidden="true" />
          <span class="bd-log__time" data-numeric>{{ formatDateTime(ev.created_at) }}</span>
          <span class="bd-log__ev">
            <span class="bd-log__tag" :style="{ color: levelMeta(ev.level).color }">
              {{ ev.event }}
            </span>
            <span v-if="ev.details" class="bd-log__det">{{ ev.details }}</span>
          </span>
          <span v-if="ev.file_id !== null" class="bd-log__file" data-numeric>
            {{ i18n.t('event.fileTag', { id: ev.file_id }) }}
          </span>
        </li>
      </ul>
    </div>
  </div>
</template>

<style scoped>
.bd-log {
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}
.bd-log__head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 9px 12px;
  border-bottom: 1px solid var(--app-border);
}
.bd-log__title {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  font-size: 12.5px;
  font-weight: 600;
  color: var(--app-text-muted);
}
.bd-log__count {
  padding: 0 6px;
  border-radius: 999px;
  background-color: var(--app-surface-hover);
  font-size: 11px;
  color: var(--app-text-dim);
}
.bd-log__jump {
  margin-left: auto;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 26px;
  padding: 0 10px;
  border-radius: 999px;
  border: 1px solid var(--app-accent);
  color: var(--app-accent);
  background-color: var(--app-accent-soft);
  font-size: 11.5px;
  font-weight: 600;
  cursor: pointer;
}
.bd-log__body {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  font-family: var(--font-mono);
  font-size: 11.5px;
}
.bd-log__list {
  list-style: none;
  margin: 0;
  padding: 4px 0;
}
.bd-log__row {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 5px 12px;
  line-height: 1.45;
}
.bd-log__row:hover {
  background-color: var(--app-surface-hover);
}
.bd-log__dot {
  flex: 0 0 auto;
  width: 6px;
  height: 6px;
  margin-top: 6px;
  border-radius: 999px;
}
.bd-log__time {
  flex: 0 0 auto;
  color: var(--app-text-dim);
  white-space: nowrap;
}
.bd-log__ev {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}
.bd-log__tag {
  font-weight: 600;
  overflow-wrap: anywhere;
}
.bd-log__det {
  color: var(--app-text-muted);
  overflow-wrap: anywhere;
}
.bd-log__file {
  margin-left: auto;
  flex: 0 0 auto;
  color: var(--app-text-dim);
  white-space: nowrap;
  align-self: center;
}
</style>
