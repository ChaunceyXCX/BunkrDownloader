import { createRouter, createWebHistory } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'
import { getToken } from '@/api/client'
import { setRouter } from './bridge'
import AppLayout from '@/layouts/AppLayout.vue'

/**
 * The Go binary serves the SPA from `dist` (`//go:embed all:dist`); the dev
 * server runs at `/`. History mode works in both because every unknown path is
 * rewritten to `index.html` by the Go fallback route.
 */
const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      // The desktop shell and any root-mounted deployment open at "/", so
      // send it through the auth guard to /app or /login. getToken() is used
      // instead of the auth store because a Pinia store cannot be created
      // while this module is being evaluated.
      path: '/',
      redirect: () => (getToken() ? { name: 'dashboard' } : { name: 'login' }),
    },
    {
      path: '/login',
      name: 'login',
      component: () => import('@/views/LoginView.vue'),
      meta: { public: true, titleKey: 'auth.login' },
    },
    {
      path: '/app',
      component: AppLayout,
      children: [
        {
          path: '',
          name: 'dashboard',
          component: () => import('@/views/DashboardView.vue'),
          meta: { titleKey: 'task.title' },
        },
        {
          path: 'tasks/:id',
          name: 'task-detail',
          component: () => import('@/views/TaskDetailView.vue'),
          props: true,
          meta: { titleKey: 'task.detailTitle' },
        },
        {
          path: 'membership',
          name: 'membership',
          component: () => import('@/views/MembershipView.vue'),
          meta: { titleKey: 'membership.title' },
        },
        {
          path: 'settings',
          name: 'settings',
          component: () => import('@/views/SettingsView.vue'),
          meta: { titleKey: 'settings.title' },
        },
      ],
    },
    {
      path: '/:pathMatch(.*)*',
      name: 'not-found',
      component: () => import('@/views/NotFoundView.vue'),
      meta: { public: true, titleKey: 'notFound.title' },
    },
  ],
  scrollBehavior: () => ({ top: 0 }),
})

setRouter(router)

router.beforeEach((to) => {
  const authed = Boolean(getToken())
  if (!to.meta.public && !authed) {
    return { name: 'login', query: to.fullPath === '/app' ? {} : { redirect: to.fullPath } }
  }
  if (to.name === 'login' && authed) {
    return { name: 'dashboard' }
  }
  return true
})

export default router

export type AppRoute = RouteRecordRaw
