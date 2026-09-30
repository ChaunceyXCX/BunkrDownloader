<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useTasksStore } from '@/stores/tasks'
import { useI18nStore } from '@/stores/i18n'
import type { FileStatus } from '@/api/types'
import Icon from '@/components/ui/Icon.vue'
import BaseButton from '@/components/ui/BaseButton.vue'
import BaseEmptyState from '@/components/ui/BaseEmptyState.vue'
import FileRow from './FileRow.vue'

const store = useTasksStore()
const i18n = useI18nStore()

const files = computed(() => store.files)
const loading = computed(() => store.filesLoading)
const hasMore = computed(() => store.hasMoreFiles)

const searchInput = ref(store.filesQuery.q)
let timer: number | undefined
function onSearch(v: string): void {
  searchInput.value = v
  if (timer) window.clearTimeout(timer)
  timer = window.setTimeout(() => {
    store.setFileQuery(v)
    void store.fetchFiles()
  }, 320)
}

const CHIPS: { value: FileStatus | ''; key: string }[] = [
  { value: '', key: 'file.filter.all' },
  { value: 'pending', key: 'file.filter.pending' },
  { value: 'downloading', key: 'file.filter.downloading' },
  { value: 'completed', key: 'file.filter.completed' },
  { value: 'failed', key: 'file.filter.failed' },
  { value: 'skipped', key: 'file.filter.skipped' },
]

function setFilter(v: FileStatus | ''): void {
  void store.setFileStatusFilter(v)
}

onMounted(() => {
  if (files.value.length === 0 && !loading.value) void store.fetchFiles()
})
</script>

<template>
  <div class="bd-files card">
    <header class="bd-files__head">
      <div class="bd-files__chips" role="group" :aria-label="i18n.t('file.allFiles')">
        <button
          v-for="c in CHIPS"
          :key="c.value || 'all'"
          type="button"
          class="chip"
          :class="{ 'is-active': store.filesQuery.status === c.value }"
          @click="setFilter(c.value)"
        >
          {{ i18n.t(c.key) }}
        </button>
      </div>
      <div class="bd-files__search">
        <Icon name="search" :size="13" class="bd-files__searchicon" />
        <input
          v-model="searchInput"
          type="text"
          class="bd-files__searchinput"
          :placeholder="i18n.t('file.searchPlaceholder')"
          :aria-label="i18n.t('file.searchPlaceholder')"
          @input="onSearch(($event.target as HTMLInputElement).value)"
        />
      </div>
    </header>

    <!-- desktop header row -->
    <div class="bd-files__thead" aria-hidden="true">
      <span class="bd-filerow__name">{{ i18n.t('file.name') }}</span>
      <span class="bd-files__th bd-files__th--size">{{ i18n.t('file.size') }}</span>
      <span class="bd-files__th bd-files__th--download">{{ i18n.t('file.downloaded') }}</span>
      <span class="bd-files__th bd-files__th--speed">{{ i18n.t('file.speed') }}</span>
      <span class="bd-files__th bd-files__th--status">{{ i18n.t('file.status') }}</span>
      <span class="bd-files__th bd-files__th--retry" />
    </div>

    <div class="bd-files__body">
      <div v-if="loading && files.length === 0" class="bd-files__skeleton">
        <div v-for="i in 6" :key="i" class="bd-files__skrow">
          <span class="bd-files__sk bd-files__sk--name" />
          <span class="bd-files__sk bd-files__sk--cell" />
          <span class="bd-files__sk bd-files__sk--cell" />
          <span class="bd-files__sk bd-files__sk--cell" />
        </div>
      </div>

      <BaseEmptyState
        v-else-if="files.length === 0"
        :title="i18n.t('file.empty')"
        :hint="i18n.t('file.emptyHint')"
        icon="database"
        compact
      />

      <div v-else>
        <FileRow v-for="f in files" :key="f.id" :file="f" @retry="(id) => store.retryFile(id)" />

        <div v-if="hasMore" class="bd-files__more">
          <BaseButton variant="ghost" size="sm" :loading="loading" :label="i18n.t('file.loadMore')" @click="store.loadMoreFiles()">
            {{ i18n.t('file.loadMore') }}
          </BaseButton>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.bd-files {
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.bd-files__head {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border-bottom: 1px solid var(--app-border);
  flex-wrap: wrap;
}
.bd-files__chips {
  display: flex;
  gap: 4px;
  flex-wrap: wrap;
}
.chip {
  height: 26px;
  padding: 0 10px;
  border-radius: 999px;
  border: 1px solid var(--app-border);
  background-color: transparent;
  color: var(--app-text-muted);
  font-size: 11.5px;
  font-weight: 550;
  cursor: pointer;
  transition:
    background-color 150ms var(--ease-ui),
    color 150ms var(--ease-ui),
    border-color 150ms var(--ease-ui);
}
.chip:hover {
  color: var(--app-text);
}
.chip.is-active {
  background-color: var(--app-accent-soft);
  border-color: color-mix(in srgb, var(--app-accent) 34%, transparent);
  color: var(--app-accent);
}
.bd-files__search {
  position: relative;
  margin-left: auto;
  display: flex;
  align-items: center;
}
.bd-files__searchicon {
  position: absolute;
  left: 9px;
  color: var(--app-text-dim);
  pointer-events: none;
}
.bd-files__searchinput {
  width: 200px;
  height: 30px;
  padding: 0 9px 0 30px;
  border-radius: 8px;
  border: 1px solid var(--app-border);
  background-color: var(--app-surface);
  color: var(--app-text);
  font-size: 12px;
  outline: none;
}
.bd-files__searchinput:focus {
  border-color: var(--app-accent);
  box-shadow: 0 0 0 3px var(--app-accent-soft);
}
.bd-files__thead {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 88px 84px 74px 92px 28px;
  gap: 10px;
  align-items: center;
  padding: 7px 12px;
  border-bottom: 1px solid var(--app-border);
  background-color: var(--app-surface-3);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--app-text-dim);
}
.bd-files__th {
  text-align: right;
  white-space: nowrap;
}
.bd-files__body {
  min-height: 120px;
}
.bd-files__more {
  display: flex;
  justify-content: center;
  padding: 8px;
}
.bd-files__skeleton {
  padding: 0 12px;
}
.bd-files__skrow {
  display: grid;
  grid-template-columns: 1fr 88px 84px 74px;
  gap: 10px;
  align-items: center;
  height: 42px;
  border-bottom: 1px solid var(--app-border);
}
.bd-files__sk {
  display: block;
  height: 12px;
  border-radius: 6px;
  background-color: var(--app-surface-3);
  background-image: linear-gradient(90deg, transparent, color-mix(in srgb, var(--app-text) 5%, transparent), transparent);
  background-size: 220% 100%;
  animation: bd-skeleton 1.5s infinite;
}
@keyframes bd-skeleton {
  from {
    background-position: 140% 0;
  }
  to {
    background-position: -40% 0;
  }
}
.bd-files__sk--name {
  width: 55%;
}
.bd-files__sk--cell {
  width: 70%;
  justify-self: end;
}
@media (max-width: 767px) {
  .bd-files__thead {
    display: none;
  }
  .bd-files__searchinput {
    width: 160px;
  }
}
</style>
