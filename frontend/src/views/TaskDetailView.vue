<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useTasksStore } from '@/stores/tasks'
import { useI18nStore } from '@/stores/i18n'
import { useToast } from '@/components/ui/useToast'
import Icon from '@/components/ui/Icon.vue'
import BaseButton from '@/components/ui/BaseButton.vue'
import BaseModal from '@/components/ui/BaseModal.vue'
import BaseSkeleton from '@/components/ui/BaseSkeleton.vue'
import BaseEmptyState from '@/components/ui/BaseEmptyState.vue'
import TaskDetailHeader from '@/components/task/TaskDetailHeader.vue'
import type { TaskAction } from '@/components/task/TaskCard.vue'
import FileTable from '@/components/task/FileTable.vue'
import EventLog from '@/components/task/EventLog.vue'

const route = useRoute()
const router = useRouter()
const store = useTasksStore()
const i18n = useI18nStore()
const toast = useToast()

const taskId = computed(() => Number(route.params.id))
const task = computed(() => store.currentTask)

const logOpen = ref(true)
const confirm = ref<null | { kind: 'cancel' | 'delete' }>(null)
const pendingDelete = ref(false)

onMounted(() => {
  void store.selectTask(taskId.value)
})

watch(
  () => route.params.id,
  (id) => {
    if (id) void store.selectTask(Number(id))
  },
)

function onAction(action: TaskAction): void {
  if (!task.value) return
  switch (action) {
    case 'start':
      void store.startTask(task.value.id)
      break
    case 'pause':
      void store.pauseTask(task.value.id)
      break
    case 'resume':
      void store.resumeTask(task.value.id)
      break
    case 'retry':
      void store.retryTask(task.value.id)
      break
    case 'cancel':
      confirm.value = { kind: 'cancel' }
      break
    case 'delete':
      confirm.value = { kind: 'delete' }
      break
  }
}

async function runConfirm(): Promise<void> {
  if (!confirm.value || !task.value) return
  const id = task.value.id
  if (confirm.value.kind === 'cancel') {
    await store.cancelTask(id)
    confirm.value = null
  } else {
    pendingDelete.value = true
    try {
      const ok = await store.deleteTask(id)
      if (ok) {
        store.clearSelection()
        await router.replace('/app')
      } else {
        confirm.value = null
      }
    } finally {
      pendingDelete.value = false
    }
  }
}

function back(): void {
  void router.push('/app')
}
</script>

<template>
  <div class="bd-detail">
    <button type="button" class="bd-detail__back" @click="back">
      <Icon name="chevron-left" :size="15" />
      {{ i18n.t('task.backToList') }}
    </button>

    <!-- not found -->
    <BaseEmptyState
      v-if="!store.taskLoading && !task"
      :title="i18n.t('task.notFound')"
      icon="file"
    >
      <BaseButton variant="primary" :label="i18n.t('common.back')" @click="back">
        {{ i18n.t('common.back') }}
      </BaseButton>
    </BaseEmptyState>

    <!-- skeleton -->
    <template v-else-if="store.taskLoading && !task">
      <div class="bd-detail__skelhead">
        <BaseSkeleton width="45%" height="18px" />
        <BaseSkeleton width="100%" height="8px" rounded="99px" />
        <div class="bd-detail__skelgrid">
          <BaseSkeleton v-for="i in 5" :key="i" height="44px" rounded="10px" />
        </div>
      </div>
      <div class="bd-detail__panes">
        <BaseSkeleton height="280px" rounded="12px" />
        <BaseSkeleton height="280px" rounded="12px" />
      </div>
    </template>

    <!-- content -->
    <template v-else-if="task">
      <TaskDetailHeader :task="task" @action="onAction" />

      <div class="bd-detail__panehead">
        <BaseButton
          variant="ghost"
          size="sm"
          :icon="'terminal'"
          :label="logOpen ? i18n.t('task.hideLog') : i18n.t('task.showLog')"
          @click="logOpen = !logOpen"
        >
          {{ logOpen ? i18n.t('task.hideLog') : i18n.t('task.showLog') }}
        </BaseButton>
      </div>

      <div class="bd-detail__panes" :class="{ 'log-closed': !logOpen }">
        <FileTable />
        <EventLog v-if="logOpen" />
      </div>
    </template>

    <BaseModal
      :open="Boolean(confirm)"
      :title="confirm?.kind === 'cancel' ? i18n.t('task.confirmCancelTitle') : i18n.t('task.confirmDeleteTitle')"
      size="sm"
      @close="confirm = null"
    >
      <p class="bd-detail__confirm">
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
.bd-detail {
  display: flex;
  flex-direction: column;
  gap: 14px;
  max-width: 1280px;
  margin: 0 auto;
}
.bd-detail__back {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  align-self: flex-start;
  padding: 5px 8px;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: var(--app-text-muted);
  font-size: 12.5px;
  font-weight: 550;
  cursor: pointer;
  transition:
    background-color 150ms var(--ease-ui),
    color 150ms var(--ease-ui);
}
.bd-detail__back:hover {
  background-color: var(--app-surface-hover);
  color: var(--app-text);
}
.bd-detail__skelhead {
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding: 16px;
  border-radius: 12px;
  border: 1px solid var(--app-border);
  background-color: var(--app-surface-2);
}
.bd-detail__skelgrid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
  gap: 10px;
}
.bd-detail__panes {
  display: grid;
  grid-template-columns: minmax(0, 2fr) minmax(0, 1fr);
  gap: 14px;
  align-items: start;
}
.bd-detail__panes.log-closed {
  grid-template-columns: minmax(0, 1fr);
}
.bd-detail__panes :deep(.bd-log) {
  position: sticky;
  top: 76px;
}
.bd-detail__panehead {
  display: flex;
  justify-content: flex-end;
}
.bd-detail__confirm {
  margin: 0;
  font-size: 13px;
  color: var(--app-text-muted);
  line-height: 1.55;
}
@media (max-width: 1023px) {
  .bd-detail__panes {
    grid-template-columns: 1fr;
  }
  .bd-detail__panes :deep(.bd-log) {
    position: static;
    max-height: 360px;
  }
}
</style>
