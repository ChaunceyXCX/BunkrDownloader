<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useTasksStore } from '@/stores/tasks'
import type { TaskFilter } from '@/stores/tasks'
import type { TaskSort, SortOrder } from '@/stores/tasks'
import { useI18nStore } from '@/stores/i18n'
import Icon from '@/components/ui/Icon.vue'
import BaseTabs from '@/components/ui/BaseTabs.vue'
import type { TabItem } from '@/components/ui/BaseTabs.vue'
import BaseDropdown from '@/components/ui/BaseDropdown.vue'
import type { DropdownItem } from '@/components/ui/BaseDropdown.vue'
import BaseButton from '@/components/ui/BaseButton.vue'

const store = useTasksStore()
const i18n = useI18nStore()

const FILTERS: { value: TaskFilter; key: string }[] = [
  { value: 'all', key: 'task.filter.all' },
  { value: 'active', key: 'task.filter.active' },
  { value: 'pending', key: 'task.filter.pending' },
  { value: 'paused', key: 'task.filter.paused' },
  { value: 'completed', key: 'task.filter.completed' },
  { value: 'failed', key: 'task.filter.failed' },
]

const tabs = computed<TabItem[]>(() => FILTERS.map((f) => ({ value: f.value, label: i18n.t(f.key) })))

/* Debounced search */
const searchInput = ref(store.filters.q)
let timer: number | undefined
function onSearch(v: string): void {
  searchInput.value = v
  if (timer) window.clearTimeout(timer)
  timer = window.setTimeout(() => void store.setQuery(v), 320)
}

const sortItems = computed<DropdownItem[]>(() => {
  const opts: { value: [TaskSort, SortOrder]; key: string }[] = [
    { value: ['created_at', 'desc'], key: 'task.sort.created_desc' },
    { value: ['created_at', 'asc'], key: 'task.sort.created_asc' },
    { value: ['updated_at', 'desc'], key: 'task.sort.updated_desc' },
    { value: ['updated_at', 'asc'], key: 'task.sort.updated_asc' },
  ]
  return opts.map((o) => ({
    key: `${o.value[0]}_${o.value[1]}`,
    label: i18n.t(o.key),
    icon: o.value[1] === 'desc' ? 'arrow-down' : 'arrow-up',
  }))
})

function onSort(item: DropdownItem): void {
  const [by, order] = item.key.split('_') as [TaskSort, SortOrder]
  void store.setSort(by, order)
}

const activeSort = computed(() => `${store.sort.by}_${store.sort.order}`)
// Locale keys use `created` / `updated` while the store uses the SQL column
// names `created_at` / `updated_at`; without the mapping the raw key leaks
// into the dropdown.
const sortColumnToKey = computed(() =>
  store.sort.by === 'updated_at' ? 'updated' : 'created',
)
const activeSortLabel = computed(() =>
  i18n.t(`task.sort.${sortColumnToKey.value}_${store.sort.order}`),
)

onMounted(() => {
  // If a query was already set and a remount happens (e.g. tab preserved), sync.
  searchInput.value = store.filters.q
})
</script>

<template>
  <div class="bd-toolbar">
    <BaseTabs :model-value="store.filters.status" :items="tabs" @update:model-value="(v) => store.setStatusFilter(v as TaskFilter)" />

    <div class="bd-toolbar__spacer" />

    <div class="bd-toolbar__search">
      <Icon name="search" :size="14" class="bd-toolbar__searchicon" />
      <input
        v-model="searchInput"
        type="text"
        class="bd-toolbar__input"
        :placeholder="i18n.t('task.searchPlaceholder')"
        :aria-label="i18n.t('task.searchPlaceholder')"
        @input="onSearch(($event.target as HTMLInputElement).value)"
      />
    </div>

    <BaseDropdown :items="sortItems" :width="170" align="end" :label="i18n.t('task.sort')" @select="onSort">
      <template #trigger="{ toggle, open, controls }">
        <button
          type="button"
          class="bd-toolbar__sort"
          :aria-label="`${i18n.t('task.sort')}: ${activeSortLabel}`"
          :aria-expanded="open"
          :aria-controls="controls"
          aria-haspopup="menu"
          @click="toggle"
        >
          <Icon name="filter" :size="14" />
          <span class="bd-toolbar__sortlabel" data-numeric="">{{ activeSortLabel }}</span>
          <Icon name="chevron-down" :size="13" />
        </button>
      </template>
    </BaseDropdown>

    <BaseButton
      variant="ghost"
      :icon="store.loading ? 'loader' : 'refresh'"
      size="md"
      :loading="store.loading"
      :label="i18n.t('task.refresh')"
      :aria-label="i18n.t('task.refresh')"
      @click="!store.loading && store.fetchTasks()"
    />
  </div>
</template>

<style scoped>
.bd-toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.bd-toolbar__spacer {
  flex: 1 1 auto;
}
.bd-toolbar__search {
  position: relative;
  display: flex;
  align-items: center;
  width: min(270px, 100%);
}
.bd-toolbar__searchicon {
  position: absolute;
  left: 10px;
  color: var(--app-text-dim);
  pointer-events: none;
}
.bd-toolbar__input {
  width: 100%;
  height: 34px;
  padding: 0 10px 0 32px;
  border-radius: 9px;
  border: 1px solid var(--app-border);
  background-color: var(--app-surface-2);
  color: var(--app-text);
  font-size: 12.5px;
  outline: none;
  transition:
    border-color 150ms var(--ease-ui),
    box-shadow 150ms var(--ease-ui);
}
.bd-toolbar__input::placeholder {
  color: var(--app-text-dim);
}
.bd-toolbar__input:focus {
  border-color: var(--app-accent);
  box-shadow: 0 0 0 3px var(--app-accent-soft);
}
.bd-toolbar__sort {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 34px;
  padding: 0 10px;
  border-radius: 9px;
  border: 1px solid var(--app-border);
  background-color: var(--app-surface-2);
  color: var(--app-text-muted);
  font-size: 12.5px;
  cursor: pointer;
  transition:
    background-color 150ms var(--ease-ui),
    color 150ms var(--ease-ui),
    border-color 150ms var(--ease-ui);
}
.bd-toolbar__sort:hover {
  color: var(--app-text);
  background-color: var(--app-surface-hover);
  border-color: var(--app-border-strong);
}
.bd-toolbar__sortlabel {
  white-space: nowrap;
}
</style>
