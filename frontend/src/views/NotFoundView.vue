<script setup lang="ts">
import { useI18nStore } from '@/stores/i18n'
import { navigate } from '@/router/bridge'
import Icon from '@/components/ui/Icon.vue'
import BaseButton from '@/components/ui/BaseButton.vue'

const i18n = useI18nStore()
</script>

<template>
  <div class="nf">
    <span class="nf__mark" aria-hidden="true">
      <Icon name="alert-triangle" :size="30" :stroke-width="1.5" />
    </span>
    <p class="nf__code" data-numeric>404</p>
    <h1 class="nf__title">{{ i18n.t('notFound.title') }}</h1>
    <p class="nf__hint">{{ i18n.t('notFound.hint') }}</p>
    <BaseButton variant="primary" :icon="'arrow-left'" :label="i18n.t('notFound.back')" @click="navigate('/app')">
      {{ i18n.t('notFound.back') }}
    </BaseButton>
  </div>
</template>

<style scoped>
.nf {
  min-height: 60vh;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  text-align: center;
  gap: 10px;
  padding: 40px 20px;
}
.nf__mark {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 64px;
  height: 64px;
  border-radius: 18px;
  color: var(--app-warn);
  background-color: color-mix(in srgb, var(--app-warn) 12%, transparent);
  border: 1px solid color-mix(in srgb, var(--app-warn) 26%, transparent);
}
.nf__code {
  margin: 6px 0 0;
  font-size: 48px;
  font-weight: 700;
  letter-spacing: -0.04em;
  line-height: 1;
  background-image: linear-gradient(120deg, var(--app-accent), var(--app-accent-3));
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
}
.nf__title {
  margin: 0;
  font-size: 18px;
  font-weight: 650;
}
.nf__hint {
  margin: 0 0 8px;
  font-size: 13px;
  color: var(--app-text-muted);
}
</style>
