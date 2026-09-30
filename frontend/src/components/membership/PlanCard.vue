<script setup lang="ts">
import { computed } from 'vue'
import type { PlanId, PlanItem } from '@/api/types'
import { useI18nStore } from '@/stores/i18n'
import { useFormat } from '@/composables/useFormat'
import Icon from '@/components/ui/Icon.vue'
import BaseButton from '@/components/ui/BaseButton.vue'

const props = defineProps<{
  plan: PlanItem
  current: boolean
  recommended?: boolean
  busy?: boolean
}>()

const emit = defineEmits<{ select: [plan: PlanItem] }>()

const i18n = useI18nStore()
const { formatPrice } = useFormat(() => i18n.locale)

const isFree = computed(() => props.plan.id === 'free')
const unlimited = computed(() => (props.plan.limits?.links ?? 0) < 0)

const periodKey = computed(() => {
  if (isFree.value) return 'membership.forever'
  if (props.plan.period_days >= 360) return 'membership.perYear'
  return 'membership.perMonth'
})

const price = computed(() => formatPrice(props.plan.price_cents, props.plan.currency))

// The backend already describes each plan's allowance in `features` (and does
// so from the same numbers that drive the quota), so the list is rendered
// as-is. Appending derived lines here duplicated the free plan's limits.
const features = computed(() => props.plan.features ?? [])

// The "current" highlight (and the disabled CTA badge) is driven solely by
// the parent's `current` prop, which knows the user's actual plan from order
// history. It must NOT default to a hardcoded tier when isMember — otherwise,
// after buying one tier every paid plan would look "current" and the other
// buy buttons would disappear.
const isActiveHere = computed(() => props.current)
</script>

<template>
  <div
    class="bd-plan"
    :class="{
      'is-recommended': recommended && !isFree,
      'is-current': isActiveHere,
    }"
  >
    <span v-if="recommended && !isFree" class="bd-plan__flag">
      <Icon name="zap" :size="12" :stroke-width="2" />
      {{ i18n.t('membership.recommended') }}
    </span>

    <div class="bd-plan__head">
      <h3 class="bd-plan__name">{{ plan.name }}</h3>
      <div class="bd-plan__price">
        <span class="bd-plan__amount" data-numeric>{{ price }}</span>
        <span class="bd-plan__period">{{ i18n.t(periodKey) }}</span>
      </div>
    </div>

    <ul class="bd-plan__features">
      <li v-for="(f, i) in features" :key="i">
        <Icon name="check" :size="14" :stroke-width="2.4" class="bd-plan__check" />
        {{ f }}
      </li>
    </ul>

    <div class="bd-plan__cta">
      <BaseButton
        v-if="isActiveHere"
        variant="ghost"
        block
        :label="i18n.t('membership.currentBadge')"
        disabled
      >
        {{ i18n.t('membership.currentBadge') }}
      </BaseButton>
      <BaseButton
        v-else-if="!isFree"
        variant="primary"
        block
        :label="i18n.t('membership.choose')"
        :loading="busy"
        @click="emit('select', plan)"
      >
        {{ i18n.t('membership.choose') }}
      </BaseButton>
      <!-- The free tier renders no CTA when it is not the active plan (an
           active member cannot downgrade mid-term) so that exactly ONE plan
           ever shows the "当前方案" badge. -->
    </div>
  </div>
</template>

<style scoped>
.bd-plan {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding: 16px;
  border-radius: 14px;
  border: 1px solid var(--app-border);
  background-color: var(--app-surface-2);
  box-shadow: var(--shadow-card);
  transition:
    transform 160ms var(--ease-ui),
    border-color 160ms var(--ease-ui);
}
.bd-plan:hover {
  transform: translateY(-1px);
}
.bd-plan.is-recommended {
  border-color: color-mix(in srgb, var(--app-accent) 55%, transparent);
  background-image: linear-gradient(
    180deg,
    color-mix(in srgb, var(--app-accent) 7%, transparent),
    transparent 42%
  );
}
.bd-plan.is-current {
  outline: 1px solid color-mix(in srgb, var(--app-accent) 40%, transparent);
}
.bd-plan__flag {
  position: absolute;
  top: -11px;
  right: 12px;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 3px 9px;
  border-radius: 999px;
  background-image: linear-gradient(120deg, var(--app-accent), var(--app-accent-2));
  color: #fff;
  font-size: 11px;
  font-weight: 600;
}
.bd-plan__head {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.bd-plan__name {
  margin: 0;
  font-size: 13.5px;
  font-weight: 600;
}
.bd-plan__price {
  display: flex;
  align-items: baseline;
  gap: 6px;
}
.bd-plan__amount {
  font-size: 26px;
  font-weight: 680;
  letter-spacing: -0.02em;
  color: var(--app-text);
}
.bd-plan__period {
  font-size: 12px;
  color: var(--app-text-muted);
}
.bd-plan__features {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 7px;
  flex: 1 1 auto;
}
.bd-plan__features li {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  font-size: 12.5px;
  color: var(--app-text-muted);
  line-height: 1.45;
}
.bd-plan__check {
  flex: 0 0 auto;
  margin-top: 1px;
  color: var(--app-accent);
}
.bd-plan__cta {
  margin-top: 2px;
}
</style>
