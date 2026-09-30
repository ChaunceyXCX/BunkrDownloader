import type { Router } from 'vue-router'

/**
 * Tiny bridge so non-router modules (stores) can navigate without importing
 * `router/index.ts` — which imports them back — creating an ESM cycle.
 * `router/index.ts` registers the instance at module-evaluation time.
 */
let instance: Router | null = null

export function setRouter(r: Router): void {
  instance = r
}

export function navigate(to: string, replace = true): void {
  if (!instance) return
  const current = instance.currentRoute.value
  if (current.fullPath === to) return
  void (replace ? instance.replace(to) : instance.push(to))
}
