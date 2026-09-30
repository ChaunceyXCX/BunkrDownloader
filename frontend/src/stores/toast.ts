import { defineStore } from 'pinia'
import { ref } from 'vue'

export type ToastType = 'success' | 'error' | 'warn' | 'info'

export interface Toast {
  id: number
  type: ToastType
  title: string
  message?: string
}

const DISMISS_MS = 4000
const MAX_VISIBLE = 5

let seq = 0

export const useToastStore = defineStore('toast', () => {
  const toasts = ref<Toast[]>([])
  const timers = new Map<number, number>()

  function dismiss(id: number): void {
    const timer = timers.get(id)
    if (timer !== undefined) {
      window.clearTimeout(timer)
      timers.delete(id)
    }
    toasts.value = toasts.value.filter((t) => t.id !== id)
  }

  function push(type: ToastType, title: string, message?: string): number {
    seq += 1
    const id = seq
    toasts.value = [...toasts.value, { id, type, title, message }]
    // Keep at most MAX_VISIBLE on screen: drop the oldest (and its timer).
    while (toasts.value.length > MAX_VISIBLE) {
      const oldest = toasts.value[0]
      if (!oldest) break
      dismiss(oldest.id)
    }
    timers.set(
      id,
      window.setTimeout(() => dismiss(id), DISMISS_MS),
    )
    return id
  }

  function clear(): void {
    toasts.value.forEach((t) => {
      const timer = timers.get(t.id)
      if (timer !== undefined) window.clearTimeout(timer)
    })
    timers.clear()
    toasts.value = []
  }

  return { toasts, push, dismiss, clear }
})
