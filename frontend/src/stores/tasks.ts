import { defineStore } from 'pinia'
import { computed, reactive, ref } from 'vue'
import { ApiError, tasksApi } from '@/api/client'
import type {
  DownloadFile,
  FileStatus,
  ListFilesQuery,
  ListTasksQuery,
  Quota,
  Stats,
  Task,
  TaskEvent,
  TaskOptions,
  TaskStatus,
  WsFrame,
  WsFileData,
  WsHelloData,
  WsLogData,
  WsQuotaData,
  WsStatsData,
  WsTaskData,
} from '@/api/types'
import { socket } from '@/api/ws'
import { useAuthStore } from './auth'
import { useI18nStore } from './i18n'
import { useToastStore } from './toast'

/** `active` is a UI-only chip (running + crawling) and filters client-side. */
export type TaskFilter = 'all' | 'active' | TaskStatus
export type TaskSort = NonNullable<ListTasksQuery['sort']>
export type SortOrder = 'asc' | 'desc'

export const TASK_PAGE_SIZE = 20
export const FILE_PAGE_SIZE = 50
export const EVENTS_CAP = 500
const EVENTS_PAGE_SIZE = 200

const ACTIVE_STATUSES: TaskStatus[] = ['crawling', 'running']

export const useTasksStore = defineStore('tasks', () => {
  const auth = useAuthStore()
  const i18n = useI18nStore()
  const toast = useToastStore()

  /* ---------------- state ---------------- */
  const tasks = ref<Task[]>([])
  const total = ref(0)
  const loading = ref(false)
  const loadingMore = ref(false)
  const offset = ref(0)

  const filters = reactive<{ status: TaskFilter; q: string }>({ status: 'all', q: '' })
  const sort = ref<{ by: TaskSort; order: SortOrder }>({ by: 'created_at', order: 'desc' })

  const selectedTaskId = ref<number | null>(null)
  const currentTask = ref<Task | null>(null)
  const taskLoading = ref(false)

  const files = ref<DownloadFile[]>([])
  const filesTotal = ref(0)
  const filesOffset = ref(0)
  const filesLoading = ref(false)
  const filesQuery = reactive<{ status: FileStatus | ''; q: string }>({ status: '', q: '' })
  const filesSort = ref<{ by: NonNullable<ListFilesQuery['sort']>; order: SortOrder }>({
    by: 'filename',
    order: 'asc',
  })

  const events = ref<TaskEvent[]>([])
  const eventsLoading = ref(false)

  const globalStats = ref<Stats | null>(null)
  const submitting = ref(false)

  /* ---------------- derived ---------------- */
  const hasMoreTasks = computed(() => tasks.value.length < total.value)
  const hasMoreFiles = computed(() => files.value.length < filesTotal.value)
  const isEmptyTasks = computed(() => !loading.value && tasks.value.length === 0)
  const isFiltered = computed(() => filters.status !== 'all' || filters.q.trim() !== '')

  /** Rows actually shown for the current chip (client-side for `active`). */
  const visibleTasks = computed<Task[]>(() =>
    filters.status === 'active' ? tasks.value.filter((t) => ACTIVE_STATUSES.includes(t.status)) : tasks.value,
  )

  function matchesFilter(task: Task): boolean {
    return filters.status === 'all' || task.status === filters.status
  }

  function listQuery(off: number): ListTasksQuery {
    const q: ListTasksQuery = {
      limit: TASK_PAGE_SIZE,
      offset: off,
      sort: sort.value.by,
      order: sort.value.order,
    }
    const q2 = filters.q.trim()
    if (q2) q.q = q2
    if (filters.status !== 'all' && filters.status !== 'active') q.status = filters.status
    return q
  }

  /* ---------------- task list ---------------- */
  async function fetchTasks(): Promise<void> {
    loading.value = true
    try {
      const res = await tasksApi.list(listQuery(0))
      tasks.value = res.tasks
      total.value = res.total
      offset.value = res.tasks.length
    } finally {
      loading.value = false
    }
  }

  async function loadMore(): Promise<void> {
    if (loadingMore.value || !hasMoreTasks.value) return
    loadingMore.value = true
    try {
      const res = await tasksApi.list(listQuery(offset.value))
      const known = new Set(tasks.value.map((t) => t.id))
      tasks.value = [...tasks.value, ...res.tasks.filter((t) => !known.has(t.id))]
      total.value = res.total
      offset.value = tasks.value.length
    } finally {
      loadingMore.value = false
    }
  }

  async function setStatusFilter(next: TaskFilter): Promise<void> {
    if (filters.status === next) return
    filters.status = next
    await fetchTasks()
  }

  async function setQuery(next: string): Promise<void> {
    filters.q = next
    await fetchTasks()
  }

  async function setSort(by: TaskSort, order: SortOrder): Promise<void> {
    sort.value = { by, order }
    await fetchTasks()
  }

  /* ---------------- create ---------------- */
  async function createTask(
    urls: string[],
    options: TaskOptions,
    autoStart: boolean,
  ): Promise<number[]> {
    submitting.value = true
    try {
      const res = await tasksApi.create({ url: urls.join('\n'), options, auto_start: autoStart })
      auth.setQuota(res.quota)
      return res.task_ids
    } finally {
      submitting.value = false
    }
  }

  /* ---------------- task actions ---------------- */
  function replaceTask(next: Task): void {
    const idx = tasks.value.findIndex((t) => t.id === next.id)
    if (idx >= 0) {
      const copy = tasks.value.slice()
      copy[idx] = next
      tasks.value = copy
    }
    if (currentTask.value?.id === next.id) currentTask.value = next
  }

  function patchTask(id: number, patch: Partial<Task>): Task | null {
    const idx = tasks.value.findIndex((t) => t.id === id)
    if (idx < 0) return null
    const merged: Task = { ...tasks.value[idx], ...patch }
    const copy = tasks.value.slice()
    copy[idx] = merged
    tasks.value = copy
    if (currentTask.value?.id === id) currentTask.value = { ...currentTask.value, ...patch }
    return merged
  }

  /**
   * Optimistically flip the status, then reconcile with the server response.
   * Any failure restores the snapshot and surfaces a toast.
   */
  async function runAction(
    id: number,
    act: 'start' | 'pause' | 'resume' | 'cancel' | 'retry',
    optimistic: Partial<Task> | null,
    successKey: string,
  ): Promise<boolean> {
    const snapshot = tasks.value.find((t) => t.id === id) ?? currentTask.value
    const previous = snapshot && snapshot.id === id ? { ...snapshot } : null
    if (optimistic) patchTask(id, optimistic)
    try {
      const res = await tasksApi.action(id, act)
      replaceTask(res.task)
      toast.push('success', i18n.t(successKey))
      return true
    } catch (e) {
      if (previous) patchTask(id, previous)
      if (!(e instanceof ApiError)) toast.push('error', i18n.t('error.generic'))
      return false
    }
  }

  const startTask = (id: number) =>
    runAction(id, 'start', { status: 'running' }, 'task.started')
  const pauseTask = (id: number) =>
    runAction(id, 'pause', { status: 'paused', speed: 0 }, 'task.paused')
  const resumeTask = (id: number) =>
    runAction(id, 'resume', { status: 'running' }, 'task.resumed')
  const cancelTask = (id: number) =>
    runAction(id, 'cancel', { status: 'canceled', speed: 0 }, 'task.canceled')
  const retryTask = (id: number) =>
    runAction(id, 'retry', { status: 'running', error_message: null }, 'task.retried')

  async function deleteTask(id: number): Promise<boolean> {
    try {
      await tasksApi.remove(id)
      tasks.value = tasks.value.filter((t) => t.id !== id)
      total.value = Math.max(0, total.value - 1)
      if (currentTask.value?.id === id) clearSelection()
      toast.push('success', i18n.t('task.deleted'))
      return true
    } catch (e) {
      if (!(e instanceof ApiError)) toast.push('error', i18n.t('error.generic'))
      return false
    }
  }

  /* ---------------- selection ---------------- */
  function clearSelection(): void {
    if (selectedTaskId.value !== null) socket.unsubscribe(selectedTaskId.value)
    selectedTaskId.value = null
    currentTask.value = null
    files.value = []
    filesTotal.value = 0
    filesOffset.value = 0
    filesQuery.status = ''
    filesQuery.q = ''
    events.value = []
  }

  async function selectTask(id: number): Promise<void> {
    selectedTaskId.value = id
    taskLoading.value = true
    events.value = []
    files.value = []
    filesTotal.value = 0
    filesOffset.value = 0
    try {
      const detail = await tasksApi.detail(id)
      currentTask.value = detail.task
      socket.subscribe(id)
      const existing = tasks.value.find((t) => t.id === id)
      if (existing) replaceTask(detail.task)
      else tasks.value = [detail.task, ...tasks.value]
      // Fire both list loads in parallel; neither blocks the other.
      await Promise.allSettled([fetchFiles(), fetchEvents()])
    } catch (e) {
      currentTask.value = null
      if (e instanceof ApiError && (e.status === 404 || e.status === 403)) {
        selectedTaskId.value = null
      }
    } finally {
      taskLoading.value = false
    }
  }

  /* ---------------- files ---------------- */
  function filesListQuery(off: number): ListFilesQuery {
    const q: ListFilesQuery = {
      limit: FILE_PAGE_SIZE,
      offset: off,
      sort: filesSort.value.by,
      order: filesSort.value.order,
    }
    if (filesQuery.status) q.status = filesQuery.status
    if (filesQuery.q.trim()) q.q = filesQuery.q.trim()
    return q
  }

  async function fetchFiles(append = false): Promise<void> {
    const id = selectedTaskId.value
    if (id === null) return
    filesLoading.value = true
    const off = append ? files.value.length : 0
    try {
      const res = await tasksApi.files(id, filesListQuery(off))
      files.value = append ? [...files.value, ...res.files] : res.files
      filesTotal.value = res.total
      filesOffset.value = res.files.length
    } finally {
      filesLoading.value = false
    }
  }

  async function loadMoreFiles(): Promise<void> {
    if (!hasMoreFiles.value || filesLoading.value) return
    await fetchFiles(true)
  }

  async function setFileStatusFilter(next: FileStatus | ''): Promise<void> {
    filesQuery.status = next
    await fetchFiles()
  }

  function setFileQuery(next: string): void {
    filesQuery.q = next
  }

  async function retryFile(fileId: number): Promise<void> {
    const id = selectedTaskId.value
    if (id === null) return
    try {
      const res = await tasksApi.retryFile(id, fileId)
      mergeFile(res.file)
      toast.push('success', i18n.t('file.retried'))
    } catch (e) {
      if (!(e instanceof ApiError)) toast.push('error', i18n.t('error.generic'))
    }
  }

  function mergeFile(next: DownloadFile): void {
    const idx = files.value.findIndex((f) => f.id === next.id)
    if (idx >= 0) {
      const copy = files.value.slice()
      copy[idx] = next
      files.value = copy
    } else if (next.task_id === selectedTaskId.value) {
      files.value = [next, ...files.value]
    }
  }

  /* ---------------- events ---------------- */
  async function fetchEvents(beforeId?: number): Promise<void> {
    const id = selectedTaskId.value
    if (id === null) return
    eventsLoading.value = true
    try {
      const res = await tasksApi.events(id, { limit: EVENTS_PAGE_SIZE, before_id: beforeId })
      // The endpoint returns newest-first; the log renders oldest-first.
      const incoming = res.events.slice().reverse()
      const known = new Set(events.value.map((e) => e.id))
      events.value = [...events.value, ...incoming.filter((e) => !known.has(e.id))].slice(-EVENTS_CAP)
    } finally {
      eventsLoading.value = false
    }
  }

  function appendEvent(event: TaskEvent): void {
    if (event.task_id !== selectedTaskId.value) return
    if (events.value.some((e) => e.id === event.id)) return
    events.value = [...events.value, event].slice(-EVENTS_CAP)
  }

  /* ---------------- WebSocket wiring ---------------- */
  let subscribed = false

  function mergeTaskFromFrame(data: WsTaskData, onCompleted: boolean): void {
    const task = data.task
    if (!task) return
    const exists = tasks.value.some((t) => t.id === task.id)
    if (exists) replaceTask(task)
    else tasks.value = [task, ...tasks.value]
    if (currentTask.value?.id === task.id) currentTask.value = task
    if (!onCompleted) return
    if (task.status === 'completed') {
      toast.push('success', task.album_name || i18n.t('app.name'), i18n.t('task.status.completed'))
    } else if (task.status === 'failed') {
      toast.push('error', task.album_name || i18n.t('app.name'), task.error_message || undefined)
    }
  }

  function handleFrame(frame: WsFrame): void {
    switch (frame.type) {
      case 'hello': {
        const d = frame.data as WsHelloData
        if (d.user) auth.setUser(d.user)
        if (d.quota) auth.setQuota(d.quota)
        if (d.stats) globalStats.value = d.stats
        break
      }
      case 'task_created':
        mergeTaskFromFrame(frame.data as WsTaskData, false)
        break
      case 'task_updated':
      case 'task_progress':
        mergeTaskFromFrame(frame.data as WsTaskData, false)
        break
      case 'task_completed':
        mergeTaskFromFrame(frame.data as WsTaskData, true)
        break
      case 'file_created':
      case 'file_progress':
      case 'file_updated': {
        const d = frame.data as WsFileData
        // Only the file pane of the task currently on screen tracks live rows.
        if (d.task_id === selectedTaskId.value && d.file) mergeFile(d.file)
        break
      }
      case 'log': {
        const d = frame.data as WsLogData
        if (d.event) appendEvent(d.event)
        break
      }
      case 'quota':
        auth.setQuota((frame.data as WsQuotaData).quota)
        break
      case 'stats':
        globalStats.value = (frame.data as WsStatsData).stats
        break
      case 'error': {
        const d = frame.data as { code?: string; message?: string }
        toast.push('error', d.message || i18n.t(`error.${d.code ?? 'generic'}`))
        break
      }
      default:
        break
    }
  }

  /** Idempotent: the singleton socket is shared, so we only bind our handler once. */
  function ensureSocketSubscription(): void {
    if (subscribed) return
    subscribed = true
    socket.onFrame(handleFrame)
  }

  function disconnectSocket(): void {
    socket.close()
  }

  function setGlobalStats(stats: Stats | null): void {
    globalStats.value = stats
  }

  function setQuota(quota: Quota): void {
    auth.setQuota(quota)
  }

  return {
    tasks,
    total,
    loading,
    loadingMore,
    filters,
    sort,
    selectedTaskId,
    currentTask,
    taskLoading,
    files,
    filesTotal,
    filesOffset,
    filesLoading,
    filesQuery,
    filesSort,
    events,
    eventsLoading,
    globalStats,
    submitting,
    hasMoreTasks,
    hasMoreFiles,
    isEmptyTasks,
    isFiltered,
    visibleTasks,
    fetchTasks,
    loadMore,
    setStatusFilter,
    setQuery,
    setSort,
    createTask,
    startTask,
    pauseTask,
    resumeTask,
    cancelTask,
    retryTask,
    deleteTask,
    clearSelection,
    selectTask,
    fetchFiles,
    loadMoreFiles,
    setFileStatusFilter,
    setFileQuery,
    retryFile,
    fetchEvents,
    ensureSocketSubscription,
    disconnectSocket,
    setGlobalStats,
    setQuota,
  }
})
