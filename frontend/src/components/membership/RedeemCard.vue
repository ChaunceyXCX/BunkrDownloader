<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18nStore } from '@/stores/i18n'
import Icon from '@/components/ui/Icon.vue'
import BaseButton from '@/components/ui/BaseButton.vue'

const props = defineProps<{ busy?: boolean }>()
const emit = defineEmits<{ redeem: [code: string] }>()

const i18n = useI18nStore()
const code = ref('')
const touched = ref(false)

const empty = computed(() => code.value.trim() === '')
const invalid = computed(() => touched.value && empty.value)

function submit(): void {
  touched.value = true
  const trimmed = code.value.trim()
  if (!trimmed) return
  emit('redeem', trimmed)
}
</script>

<template>
  <div class="bd-redeem card">
    <div class="bd-redeem__head">
      <span class="bd-redeem__icon"><Icon name="ticket" :size="17" /></span>
      <div>
        <h3 class="bd-redeem__title">{{ i18n.t('membership.redeem') }}</h3>
        <p class="bd-redeem__hint">{{ i18n.t('membership.redeemHint') }}</p>
      </div>
    </div>

    <form class="bd-redeem__form" @submit.prevent="submit">
      <div class="bd-redeem__field" :class="{ 'is-invalid': invalid }">
        <Icon name="ticket" :size="14" class="bd-redeem__lead" />
        <input
          v-model="code"
          type="text"
          class="bd-redeem__input"
          :placeholder="i18n.t('membership.redeemPlaceholder')"
          :aria-label="i18n.t('membership.redeemPlaceholder')"
          :aria-invalid="invalid || undefined"
          spellcheck="false"
        />
      </div>
      <BaseButton
        variant="primary"
        type="submit"
        :label="props.busy ? i18n.t('membership.redeemSubmitting') : i18n.t('membership.redeemSubmit')"
        :loading="props.busy"
        :disabled="empty || props.busy"
        @click="submit"
      >
        {{ props.busy ? i18n.t('membership.redeemSubmitting') : i18n.t('membership.redeemSubmit') }}
      </BaseButton>
    </form>
    <p v-if="invalid" class="bd-redeem__err" role="alert">{{ i18n.t('membership.redeemEmpty') }}</p>
  </div>
</template>

<style scoped>
.bd-redeem {
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.bd-redeem__head {
  display: flex;
  gap: 11px;
  align-items: flex-start;
}
.bd-redeem__icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  flex: 0 0 auto;
  border-radius: 9px;
  color: var(--app-accent);
  background-color: var(--app-accent-soft);
}
.bd-redeem__title {
  margin: 0;
  font-size: 13.5px;
  font-weight: 600;
}
.bd-redeem__hint {
  margin: 2px 0 0;
  font-size: 12px;
  color: var(--app-text-muted);
}
.bd-redeem__form {
  display: flex;
  gap: 8px;
}
.bd-redeem__field {
  position: relative;
  flex: 1 1 auto;
  display: flex;
  align-items: center;
  min-width: 0;
}
.bd-redeem__lead {
  position: absolute;
  left: 10px;
  color: var(--app-text-dim);
  pointer-events: none;
}
.bd-redeem__input {
  width: 100%;
  height: 36px;
  padding: 0 10px 0 32px;
  border-radius: 8px;
  border: 1px solid var(--app-border);
  background-color: var(--app-surface);
  color: var(--app-text);
  font-family: var(--font-mono);
  font-size: 12.5px;
  outline: none;
  transition:
    border-color 150ms var(--ease-ui),
    box-shadow 150ms var(--ease-ui);
}
.bd-redeem__input:focus {
  border-color: var(--app-accent);
  box-shadow: 0 0 0 3px var(--app-accent-soft);
}
.bd-redeem__field.is-invalid .bd-redeem__input {
  border-color: var(--app-danger);
}
.bd-redeem__err {
  margin: -6px 0 0;
  font-size: 11.5px;
  color: var(--app-danger);
}
@media (max-width: 480px) {
  .bd-redeem__form {
    flex-direction: column;
  }
}
</style>
