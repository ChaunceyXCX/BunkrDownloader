import { useToastStore } from '@/stores/toast'
import type { ToastType } from '@/stores/toast'

/** Component-friendly facade so views can toast without importing the store id. */
export function useToast() {
  const store = useToastStore()
  return {
    toasts: store.toasts,
    dismiss: store.dismiss,
    clear: store.clear,
    success: (title: string, message?: string) => store.push('success', title, message),
    error: (title: string, message?: string) => store.push('error', title, message),
    warn: (title: string, message?: string) => store.push('warn', title, message),
    info: (title: string, message?: string) => store.push('info', title, message),
    push: (type: ToastType, title: string, message?: string) => store.push(type, title, message),
  }
}
