<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink, RouterView, useRoute } from 'vue-router'
import { useI18nStore } from '@/stores/i18n'
import { useTasksStore } from '@/stores/tasks'
import { useFormat } from '@/composables/useFormat'
import Icon from '@/components/ui/Icon.vue'
import type { IconName } from '@/components/ui/Icon.vue'
import BaseTooltip from '@/components/ui/BaseTooltip.vue'
import ConnectionIndicator from '@/components/layout/ConnectionIndicator.vue'
import QuotaPill from '@/components/layout/QuotaPill.vue'
import LanguageSwitcher from '@/components/layout/LanguageSwitcher.vue'
import ThemeToggle from '@/components/layout/ThemeToggle.vue'
import UserMenu from '@/components/layout/UserMenu.vue'

const i18n = useI18nStore()
const tasks = useTasksStore()
const route = useRoute()
const { formatCount, formatSpeed } = useFormat(() => i18n.locale)

/* ---------------- sidebar state ---------------- */
const collapsed = ref(false)
const drawerOpen = ref(false)
const isNarrow = ref(false)
let mql: MediaQueryList | null = null

function syncWidth(): void {
  isNarrow.value = mql?.matches ?? false
  if (!isNarrow.value) drawerOpen.value = false
}

onMounted(() => {
  try {
    collapsed.value = localStorage.getItem('bunkr_sidebar') === 'collapsed'
  } catch {
    /* ignore */
  }
  if (typeof window.matchMedia === 'function') {
    mql = window.matchMedia('(max-width: 1023px)')
    syncWidth()
    mql.addEventListener('change', syncWidth)
  }
})

onBeforeUnmount(() => mql?.removeEventListener('change', syncWidth))

watch(collapsed, (v) => {
  try {
    localStorage.setItem('bunkr_sidebar', v ? 'collapsed' : 'expanded')
  } catch {
    /* ignore */
  }
})

watch(() => route.fullPath, () => {
  drawerOpen.value = false
})

/* ---------------- nav model ---------------- */
interface NavEntry {
  to: string
  labelKey: string
  icon: IconName
}
const NAV: NavEntry[] = [
  { to: '/app', labelKey: 'nav.dashboard', icon: 'download' },
  { to: '/app/membership', labelKey: 'nav.membership', icon: 'crown' },
  { to: '/app/settings', labelKey: 'nav.settings', icon: 'settings' },
]

function isActive(to: string): boolean {
  if (to === '/app') return route.path === '/app' || route.path.startsWith('/app/tasks/')
  return route.path.startsWith(to)
}

const pageTitle = computed(() => {
  const key = (route.meta.titleKey as string | undefined) ?? 'task.title'
  const full = i18n.t(key)
  return full === key ? i18n.t('task.title') : full
})

const crumbs = computed(() => {
  const out: { label: string; to?: string }[] = [{ label: i18n.t('app.name') }]
  out.push({ label: pageTitle.value, to: route.path.startsWith('/app/tasks/') ? '/app' : undefined })
  return out
})

const stats = computed(() => tasks.globalStats)
const speedText = computed(() => formatSpeed(stats.value?.speed ?? 0))
</script>

<template>
  <div class="shell" :class="{ 'is-collapsed': collapsed && !isNarrow }">
    <!-- ============ sidebar ============ -->
    <aside
      class="side"
      :class="{ 'is-drawer': isNarrow, 'is-open': drawerOpen }"
      :aria-hidden="isNarrow && !drawerOpen ? 'true' : undefined"
    >
      <div class="side__head">
        <RouterLink to="/app" class="side__brand" :aria-label="i18n.t('app.name')">
          <span class="side__logo" aria-hidden="true"><Icon name="download" :size="17" :stroke-width="2" /></span>
          <span v-if="!collapsed || isNarrow" class="side__name">{{ i18n.t('app.name') }}</span>
        </RouterLink>
        <button
          v-if="isNarrow"
          type="button"
          class="side__close"
          :aria-label="i18n.t('nav.closeMenu')"
          @click="drawerOpen = false"
        >
          <Icon name="x" :size="16" />
        </button>
      </div>

      <nav class="side__nav" :aria-label="i18n.t('app.name')">
        <RouterLink
          v-for="item in NAV"
          :key="item.to"
          :to="item.to"
          class="side__link"
          :class="{ 'is-active': isActive(item.to) }"
        >
          <span class="side__icon"><Icon :name="item.icon" :size="17" /></span>
          <span v-if="!collapsed || isNarrow" class="side__label">{{ i18n.t(item.labelKey) }}</span>
          <span v-if="item.to === '/app' && (collapsed && !isNarrow)" class="sr-only">
            {{ i18n.t(item.labelKey) }}
          </span>
        </RouterLink>
      </nav>

      <div class="side__foot">
        <button
          v-if="!isNarrow"
          type="button"
          class="side__collapse"
          :aria-label="collapsed ? i18n.t('nav.expand') : i18n.t('nav.collapse')"
          @click="collapsed = !collapsed"
        >
          <Icon :name="collapsed ? 'chevron-right' : 'chevron-left'" :size="15" />
          <span v-if="!collapsed" class="side__label">{{ i18n.t('nav.collapse') }}</span>
        </button>
      </div>
    </aside>

    <div
      v-if="isNarrow && drawerOpen"
      class="shell__overlay"
      @click="drawerOpen = false"
    />

    <!-- ============ main column ============ -->
    <div class="main">
      <header class="top">
        <button
          v-if="isNarrow"
          type="button"
          class="top__menu"
          :aria-label="i18n.t('nav.menu')"
          @click="drawerOpen = true"
        >
          <Icon name="menu" :size="18" />
        </button>

        <div class="top__lead">
          <h1 class="top__title">{{ pageTitle }}</h1>
          <nav class="top__crumbs" :aria-label="pageTitle">
            <template v-for="(c, i) in crumbs" :key="c.label + i">
              <Icon name="chevron-right" :size="12" class="top__crumbsep" />
              <RouterLink v-if="c.to" :to="c.to" class="top__crumb">{{ c.label }}</RouterLink>
              <span v-else class="top__crumb is-current" aria-current="page">{{ c.label }}</span>
            </template>
          </nav>
        </div>

        <div class="top__stats">
          <BaseTooltip :text="`${i18n.t('topbar.statTasks')}: ${formatCount(stats?.total_tasks ?? 0)}`" placement="bottom">
            <span class="pill" data-numeric>
              <Icon name="list" :size="13" />
              {{ formatCount(stats?.total_tasks ?? 0) }}
            </span>
          </BaseTooltip>
          <BaseTooltip :text="`${i18n.t('topbar.statFiles')}: ${formatCount(stats?.total_files ?? 0)}`" placement="bottom">
            <span class="pill" data-numeric>
              <Icon name="database" :size="13" />
              {{ formatCount(stats?.total_files ?? 0) }}
            </span>
          </BaseTooltip>
          <BaseTooltip :text="`${i18n.t('topbar.statSpeed')}: ${speedText}`" placement="bottom">
            <span class="pill is-accent" data-numeric>
              <Icon name="zap" :size="13" />
              {{ speedText }}
            </span>
          </BaseTooltip>
        </div>

        <div class="top__actions">
          <QuotaPill />
          <ConnectionIndicator />
          <LanguageSwitcher />
          <ThemeToggle />
          <UserMenu />
        </div>
      </header>

      <main class="content">
        <RouterView v-slot="{ Component }">
          <Transition name="bd-fade" mode="out-in">
            <component :is="Component" />
          </Transition>
        </RouterView>
      </main>
    </div>
  </div>
</template>

<style scoped>
.shell {
  display: flex;
  min-height: 100vh;
  min-height: 100dvh;
  background-color: var(--app-bg);
}

/* ---------------- sidebar ---------------- */
.side {
  position: sticky;
  top: 0;
  z-index: 40;
  flex: 0 0 auto;
  width: 232px;
  height: 100vh;
  height: 100dvh;
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 14px 12px;
  background-color: var(--app-surface);
  border-right: 1px solid var(--app-border);
  transition: width 200ms var(--ease-ui);
}
.is-collapsed .side {
  width: 68px;
}
.side__head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 2px 4px 8px;
}
.side__brand {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
  color: inherit;
  text-decoration: none;
}
.side__logo {
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border-radius: 10px;
  background-image: linear-gradient(125deg, var(--app-accent), var(--app-accent-2) 62%, var(--app-accent-3));
  color: #fff;
}
.side__name {
  font-size: 14px;
  font-weight: 650;
  letter-spacing: -0.015em;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.side__close {
  margin-left: auto;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: 8px;
  border: 0;
  background: transparent;
  color: var(--app-text-muted);
  cursor: pointer;
}
.side__close:hover {
  background-color: var(--app-surface-hover);
}
.side__nav {
  display: flex;
  flex-direction: column;
  gap: 2px;
  flex: 1 1 auto;
  overflow-y: auto;
}
.side__link {
  display: flex;
  align-items: center;
  gap: 10px;
  height: 36px;
  padding: 0 10px;
  border-radius: 9px;
  color: var(--app-text-muted);
  font-size: 13.5px;
  font-weight: 550;
  text-decoration: none;
  white-space: nowrap;
  transition:
    background-color 150ms var(--ease-ui),
    color 150ms var(--ease-ui);
}
.side__link:hover {
  background-color: var(--app-surface-hover);
  color: var(--app-text);
}
.side__link.is-active {
  background-color: var(--app-accent-soft);
  color: var(--app-accent);
}
.side__icon {
  display: inline-flex;
  flex: 0 0 auto;
}
.side__label {
  overflow: hidden;
  text-overflow: ellipsis;
}
.side__foot {
  padding-top: 8px;
  border-top: 1px solid var(--app-border);
}
.side__collapse {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  height: 34px;
  padding: 0 10px;
  border: 0;
  border-radius: 9px;
  background: transparent;
  color: var(--app-text-dim);
  font-size: 12.5px;
  cursor: pointer;
  transition:
    background-color 150ms var(--ease-ui),
    color 150ms var(--ease-ui);
}
.side__collapse:hover {
  background-color: var(--app-surface-hover);
  color: var(--app-text);
}

.side.is-drawer {
  position: fixed;
  inset: 0 auto 0 0;
  width: 250px;
  transform: translateX(-100%);
  transition: transform 220ms var(--ease-ui);
  box-shadow: var(--shadow-pop);
}
.side.is-drawer.is-open {
  transform: translateX(0);
}
.shell__overlay {
  position: fixed;
  inset: 0;
  z-index: 35;
  background-color: color-mix(in srgb, var(--app-text) 38%, transparent);
  backdrop-filter: blur(3px);
  -webkit-backdrop-filter: blur(3px);
}

/* ---------------- main column ---------------- */
.main {
  flex: 1 1 auto;
  min-width: 0;
  display: flex;
  flex-direction: column;
}
.top {
  position: sticky;
  top: 0;
  z-index: 30;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 18px;
  min-height: 60px;
  background-color: color-mix(in srgb, var(--app-bg) 82%, transparent);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
  border-bottom: 1px solid var(--app-border);
}
.top__menu {
  display: none;
  align-items: center;
  justify-content: center;
  width: 34px;
  height: 34px;
  border-radius: 9px;
  border: 1px solid var(--app-border);
  background-color: var(--app-surface-2);
  color: var(--app-text-muted);
  cursor: pointer;
}
.top__lead {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}
.top__title {
  margin: 0;
  font-size: 15px;
  font-weight: 620;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.top__crumbs {
  display: flex;
  align-items: center;
  gap: 3px;
  font-size: 11.5px;
  color: var(--app-text-dim);
  white-space: nowrap;
}
.top__crumb {
  color: inherit;
  text-decoration: none;
}
a.top__crumb:hover {
  color: var(--app-accent);
}
.top__crumb.is-current {
  color: var(--app-text-muted);
}
.top__crumbsep {
  opacity: 0.6;
}
.top__stats {
  display: none;
  align-items: center;
  gap: 6px;
  margin-left: 6px;
}
.pill {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 28px;
  padding: 0 10px;
  border-radius: 999px;
  border: 1px solid var(--app-border);
  background-color: var(--app-surface-2);
  color: var(--app-text-muted);
  font-size: 11.5px;
  font-weight: 550;
}
.pill.is-accent {
  color: var(--app-accent);
  border-color: color-mix(in srgb, var(--app-accent) 26%, transparent);
  background-color: var(--app-accent-soft);
}
.top__actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 8px;
}
.content {
  flex: 1 1 auto;
  min-width: 0;
  padding: 18px;
}

@media (min-width: 1280px) {
  .top__stats {
    display: flex;
  }
}
@media (max-width: 1023px) {
  .top__menu {
    display: inline-flex;
  }
  .side {
    position: fixed;
  }
}
@media (max-width: 767px) {
  .content {
    padding: 14px 12px 22px;
  }
  .top {
    padding: 10px 12px;
    gap: 9px;
  }
  .top__crumbs {
    display: none;
  }
}
</style>
