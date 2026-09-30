<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { useTasksStore } from '@/stores/tasks'
import type { TaskFilter } from '@/stores/tasks'
import { useI18nStore } from '@/stores/i18n'
import { useFormat } from '@/composables/useFormat'
import { useToast } from '@/components/ui/useToast'
import Icon from '@/components/ui/Icon.vue'
import StatCard from '@/components/ui/StatCard.vue'
import BaseButton from '@/components/ui/BaseButton.vue'
import BaseSkeleton from '@/components/ui/BaseSkeleton.vue'
import BaseEmptyState from '@/components/ui/BaseEmptyState.vue'
import BaseModal from '@/components/ui/BaseModal.vue'
import TaskToolbar from '@/components/task/TaskToolbar.vue'
import TaskCard from '@/components/task/TaskCard.vue'
import type { TaskAction } from '@/components/task/TaskCard.vue'
import NewTaskModal from '@/components/task/NewTaskModal.vue'

const store = useTasksStore()
const i18n = useI18nStore()
const toast = useToast()
const { formatCount, formatSpeed } = useFormat(() => i18n.locale)

const newModalOpen = ref(false)
const confirm = ref<null | { taskId: number; kind: 'cancel' | 'delete' }>(null)
const pendingDelete = ref(false)

const stats = computed(() => store.globalStats)

const visible = computed(() => store.visibleTasks)
const showLoadMore = computed(() => store.hasMoreTasks && store.filters.status !== 'active')
const showEmpty = computed(() => store.isEmptyTasks && !store.isFiltered)

onMounted(() => {
  if (store.tasks.length === 0) void store.fetchTasks()
})

/* ---------------- confirm dialogs ---------------- */
function onAction(action: TaskAction, taskId: number): void {
  switch (action) {
    case 'start':
      void store.startTask(taskId)
      break
    case 'pause':
      void store.pauseTask(taskId)
      break
    case 'resume':
      void store.resumeTask(taskId)
      break
    case 'retry':
      void store.retryTask(taskId)
      break
    case 'cancel':
      confirm.value = { taskId, kind: 'cancel' }
      break
    case 'delete':
      confirm.value = { taskId, kind: 'delete' }
      break
  }
}

async function runConfirm(): Promise<void> {
  if (!confirm.value) return
  const { taskId, kind } = confirm.value
  const ok =
    kind === 'cancel'
      ? await store.cancelTask(taskId)
      : await runDelete(taskId)
  if (ok) confirm.value = null
}

async function runDelete(taskId: number): Promise<boolean> {
  pendingDelete.value = true
  try {
    return await store.deleteTask(taskId)
  } finally {
    pendingDelete.value = false
  }
}
</script>

<template>
  <div class="bd-dash">
    <!-- stat row -->
    <div class="bd-dash__stats">
      <StatCard
        :label="i18n.t('topbar.statTasks')"
        :value="formatCount(stats?.total_tasks ?? store.total)"
        :icon="'list'"
        :tone="'neutral'"
      />
      <StatCard
        :label="i18n.t('topbar.statActive')"
        :value="formatCount((stats?.running ?? 0) + (stats?.pending ?? 0))"
        :icon="'zap'"
        :tone="'accent'"
      />
      <StatCard
        :label="i18n.t('task.filter.completed')"
        :value="formatCount(stats?.completed ?? 0)"
        :icon="'check'"
        :tone="'success'"
      />
      <StatCard
        :label="i18n.t('task.filter.failed')"
        :value="formatCount(stats?.failed ?? 0)"
        :icon="'alert-triangle'"
        :tone="'danger'"
      />
      <StatCard
        :label="i18n.t('topbar.statFiles')"
        :value="formatCount(stats?.total_files ?? 0)"
        :icon="'database'"
        :tone="'info'"
      />
      <StatCard
        :label="i18n.t('topbar.statSpeed')"
        :value="formatSpeed(stats?.speed ?? 0)"
        :icon="'download'"
        :tone="'accent'"
      />
    </div>

    <!-- toolbar row -->
    <div class="bd-dash__toolbar">
      <TaskToolbar />
      <BaseButton
        variant="primary"
        :icon="'plus'"
        :label="i18n.t('task.new')"
        @click="newModalOpen = true"
      >
        {{ i18n.t('task.new') }}
      </BaseButton>
    </div>

    <!-- list -->
    <div class="bd-dash__list">
      <!-- skeletons -->
      <div v-if="store.loading && store.tasks.length === 0" class="bd-dash__skel">
        <div v-for="i in 6" :key="i" class="bd-dash__skelcard">
          <div class="bd-dash__skelrow">
            <BaseSkeleton width="34px" height="34px" rounded="9px" />
            <div class="bd-dash__skelcol">
              <BaseSkeleton width="48%" height="13px" />
              <BaseSkeleton width="72%" height="11px" />
            </div>
          </div>
          <BaseSkeleton width="100%" height="5px" rounded="99px" />
        </div>
      </div>

      <!-- empty -->
      <BaseEmptyState
        v-else-if="showEmpty"
        :title="i18n.t('task.empty')"
        :hint="i18n.t('task.emptyHint')"
        icon="inbox"
      >
        <BaseButton variant="primary" :icon="'plus'" :label="i18n.t('task.new')" @click="newModalOpen = true">
          {{ i18n.t('task.new') }}
        </BaseButton>
      </BaseEmptyState>

      <BaseEmptyState
        v-else-if="store.tasks.length > 0 && visible.length === 0"
        :title="i18n.t('task.emptyFiltered')"
        :hint="i18n.t('task.emptyFilteredHint')"
        icon="search"
        compact
      />

      <!-- list -->
      <div v-else class="bd-dash__cards">
        <TaskCard v-for="t in visible" :key="t.id" :task="t" @action="(a) => onAction(a, t.id)" />
      </div>
    </div>

    <!-- load more -->
    <div v-if="showLoadMore" class="bd-dash__more">
      <BaseButton
        variant="ghost"
        :loading="store.loadingMore"
        :label="i18n.t('task.loadMore')"
        @click="store.loadMore()"
      >
        {{ i18n.t('task.loadMore') }}
      </BaseButton>
    </div>
    <p v-else-if="store.tasks.length > 0 && !store.loading" class="bd-dash__all" data-numeric>
      {{ i18n.t('task.loadedAll') }}
    </p>

    <NewTaskModal :open="newModalOpen" @close="newModalOpen = false" />

    <BaseModal
      :open="Boolean(confirm)"
      :title="confirm?.kind === 'cancel' ? i18n.t('task.confirmCancelTitle') : i18n.t('task.confirmDeleteTitle')"
      size="sm"
      @close="confirm = null"
    >
      <p class="bd-dash__confirm">
        {{ confirm?.kind === 'cancel' ? i18n.t('task.confirmCancel') : i18n.t('task.confirmDelete') }}
      </p>
      <template #footer>
        <BaseButton variant="ghost" :label="i18n.t('common.cancel')" @click="confirm = null">
          {{ i18n.t('common.cancel') }}
        </BaseButton>
        <BaseButton
          :variant="confirm?.kind === 'delete' ? 'danger' : 'primary'"
          :loading="pendingDelete"
          :label="i18n.t('common.confirm')"
          @click="runConfirm"
        >
          {{ i18n.t('common.confirm') }}
        </BaseButton>
      </template>
    </BaseModal>
  </div>
</template>

<style scoped>
.bd-dash {
  display: flex;
  flex-direction: column;
  gap: 16px;
  max-width: 1160px;
  margin: 0 auto;
}
.bd-dash__stats {
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));
  gap: 12px;
}
.bd-dash__toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}
.bd-dash__toolbar :deep(.bd-toolbar) {
  flex: 1 1 auto;
  min-width: 0;
}
.bd-dash__list {
  display: flex;
  flex-direction: column;
}
.bd-dash__cards {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.bd-dash__skel {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.bd-dash__skelcard {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 13px 14px;
  border-radius: 12px;
  border: 1px solid var(--app-border);
  background-color: var(--app-surface-2);
}
.bd-dash__skelrow {
  display: flex;
  align-items: center;
  gap: 11px;
}
.bd-dash__skelcol {
  display: flex;
  flex-direction: column;
  gap: 6px;
  flex: 1 1 auto;
}
.bd-dash__more {
  display: flex;
  justify-content: center;
}
.bd-dash__all {
  margin: 0;
  text-align: center;
  font-size: 12px;
  color: var(--app-text-dim);
}
.bd-dash__confirm {
  margin: 0;
  font-size: 13px;
  color: var(--app-text-muted);
  line-height: 1.55;
}
@media (max-width: 1023px) {
  .bd-dash__stats {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}
@media (max-width: 639px) {
  .bd-dash__stats {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
