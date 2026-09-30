<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useI18nStore } from '@/stores/i18n'
import { useSettingsStore } from '@/stores/settings'
import { useThemeStore } from '@/stores/theme'
import Icon from '@/components/ui/Icon.vue'

const i18n = useI18nStore()
const theme = useThemeStore()
const settings = useSettingsStore()

const version = computed(() => settings.version || '—')

onMounted(() => {
  // Public endpoints: surface the server version in the footer without auth.
  void settings.loadRuntime()
})
</script>

<template>
  <div class="auth-shell">
    <div class="auth-shell__bg" aria-hidden="true">
      <span class="auth-shell__glow auth-shell__glow--a" />
      <span class="auth-shell__glow auth-shell__glow--b" />
      <span class="auth-shell__grid" />
    </div>

    <main class="auth-shell__center">
      <div class="auth-shell__brand">
        <span class="auth-shell__logo" aria-hidden="true">
          <Icon name="download" :size="19" :stroke-width="2" />
        </span>
        <div>
          <p class="auth-shell__name">{{ i18n.t('app.name') }}</p>
          <p class="auth-shell__tag">{{ i18n.t('app.tagline') }}</p>
        </div>
      </div>

      <div class="auth-card bd-anim-pop">
        <slot />
      </div>

      <footer class="auth-shell__foot">
        <span data-numeric>{{ i18n.t('app.copyright') }} · v{{ version }}</span>
        <button
          type="button"
          class="auth-shell__theme"
          :aria-label="i18n.t('nav.theme')"
          @click="theme.toggleFromIcon()"
        >
          <Icon :name="theme.resolved === 'dark' ? 'sun' : 'moon'" :size="15" />
        </button>
      </footer>
    </main>
  </div>
</template>

<style scoped>
.auth-shell {
  position: relative;
  min-height: 100vh;
  min-height: 100dvh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 32px 20px;
  background-color: var(--app-bg);
  overflow: hidden;
}
.auth-shell__bg {
  position: absolute;
  inset: 0;
  pointer-events: none;
}
.auth-shell__grid {
  position: absolute;
  inset: 0;
  background-image:
    linear-gradient(to right, var(--app-border) 1px, transparent 1px),
    linear-gradient(to bottom, var(--app-border) 1px, transparent 1px);
  background-size: 56px 56px;
  mask-image: radial-gradient(ellipse 80% 62% at 50% 42%, #000 20%, transparent 78%);
  -webkit-mask-image: radial-gradient(ellipse 80% 62% at 50% 42%, #000 20%, transparent 78%);
  opacity: 0.55;
}
.auth-shell__glow {
  position: absolute;
  border-radius: 50%;
  filter: blur(90px);
  opacity: 0.4;
}
.auth-shell__glow--a {
  width: 460px;
  height: 460px;
  top: -140px;
  left: -80px;
  background-image: radial-gradient(circle, var(--app-accent), transparent 68%);
}
.auth-shell__glow--b {
  width: 420px;
  height: 420px;
  bottom: -160px;
  right: -70px;
  background-image: radial-gradient(circle, var(--app-accent-3), transparent 68%);
  opacity: 0.28;
}
.auth-shell__center {
  position: relative;
  width: 100%;
  max-width: 408px;
  display: flex;
  flex-direction: column;
  gap: 18px;
}
.auth-shell__brand {
  display: flex;
  align-items: center;
  gap: 11px;
  padding-left: 2px;
}
.auth-shell__logo {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  border-radius: 11px;
  background-image: linear-gradient(125deg, var(--app-accent), var(--app-accent-2) 60%, var(--app-accent-3));
  color: #fff;
  box-shadow: 0 6px 18px -8px var(--app-accent);
}
.auth-shell__name {
  margin: 0;
  font-size: 15px;
  font-weight: 650;
  letter-spacing: -0.01em;
}
.auth-shell__tag {
  margin: 1px 0 0;
  font-size: 12px;
  color: var(--app-text-muted);
}
.auth-card {
  padding: 24px;
  border-radius: 14px;
  background-color: var(--app-surface-2);
  border: 1px solid var(--app-border);
  box-shadow: var(--shadow-pop);
}
.auth-shell__foot {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  font-size: 11.5px;
  color: var(--app-text-dim);
}
.auth-shell__theme {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: 8px;
  border: 1px solid var(--app-border);
  background-color: var(--app-surface-2);
  color: var(--app-text-muted);
  cursor: pointer;
  transition:
    color 150ms var(--ease-ui),
    background-color 150ms var(--ease-ui);
}
.auth-shell__theme:hover {
  color: var(--app-text);
  background-color: var(--app-surface-hover);
}
@media (max-width: 480px) {
  .auth-card {
    padding: 20px;
  }
}
</style>
