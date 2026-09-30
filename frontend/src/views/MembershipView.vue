<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { membershipApi } from '@/api/client'
import type { Order, PlanItem, PlanId } from '@/api/types'
import { ApiError } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useI18nStore } from '@/stores/i18n'
import { useFormat } from '@/composables/useFormat'
import { useToast } from '@/components/ui/useToast'
import Icon from '@/components/ui/Icon.vue'
import BaseModal from '@/components/ui/BaseModal.vue'
import BaseButton from '@/components/ui/BaseButton.vue'
import BaseSkeleton from '@/components/ui/BaseSkeleton.vue'
import PlanCard from '@/components/membership/PlanCard.vue'
import OrderTable from '@/components/membership/OrderTable.vue'
import RedeemCard from '@/components/membership/RedeemCard.vue'
import QuotaMeter from '@/components/membership/QuotaMeter.vue'

const auth = useAuthStore()
const i18n = useI18nStore()
const toast = useToast()
const { formatPrice, formatDateTime } = useFormat(() => i18n.locale)

const plans = ref<PlanItem[]>([])
const orders = ref<Order[]>([])
const loading = ref(true)
const ordering = ref(false)
const redeeming = ref(false)

const confirmPay = ref<null | { order: Order; planName: string }>(null)
const paying = ref(false)

// The plan the user currently holds. The backend records membership as a
// generic `plan: "member"`, so the specific tier (monthly vs yearly) is derived
// from the most recent paid order. This keeps only the owned plan marked
// "current" and leaves the other paid plans purchasable (renew / switch).
const currentPlanId = computed<PlanId>(() => {
  const paid = orders.value
    .filter((o) => o.status === 'paid')
    .sort((a, b) => b.id - a.id)[0]
  if (paid && (paid.plan === 'member_monthly' || paid.plan === 'member_yearly')) {
    return paid.plan
  }
  return auth.isMember ? 'member_monthly' : 'free'
})

onMounted(loadAll)

async function loadAll(): Promise<void> {
  loading.value = true
  try {
    const [p, o] = await Promise.all([membershipApi.plans(), membershipApi.orders()])
    plans.value = p.plans
    orders.value = o.orders
  } catch {
    toast.error(i18n.t('error.generic'))
  } finally {
    loading.value = false
  }
}

async function choosePlan(plan: PlanItem): Promise<void> {
  if (ordering.value) return
  ordering.value = true
  try {
    const res = await membershipApi.createOrder({ plan: plan.id })
    confirmPay.value = { order: res.order, planName: plan.name }
    const name = plan.name
    // plan name shown in confirm modal
    confirmPay.value.planName = name
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : i18n.t('error.generic'))
  } finally {
    ordering.value = false
  }
}

async function confirmPayment(): Promise<void> {
  if (!confirmPay.value || paying.value) return
  paying.value = true
  try {
    const res = await membershipApi.pay(confirmPay.value.order.id, { pay_method: 'alipay' })
    auth.setQuota(res.quota)
    auth.setUser(res.user)
    toast.success(i18n.t('membership.paySuccess'))
    confirmPay.value = null
    await loadAll()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : i18n.t('error.generic'))
  } finally {
    paying.value = false
  }
}

async function cancelOrder(order: Order): Promise<void> {
  try {
    const res = await membershipApi.cancelOrder({ order_id: order.id })
    const idx = orders.value.findIndex((o) => o.id === res.order.id)
    if (idx >= 0) {
      const copy = orders.value.slice()
      copy[idx] = res.order
      orders.value = copy
    }
    toast.success(i18n.t('membership.orderCanceled'))
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : i18n.t('error.generic'))
  }
}

async function redeem(code: string): Promise<void> {
  if (redeeming.value) return
  redeeming.value = true
  try {
    const res = await membershipApi.redeem({ code })
    auth.setQuota(res.quota)
    auth.setUser(res.user)
    toast.success(i18n.t('membership.redeemSuccess'))
    await loadAll()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : i18n.t('error.generic'))
  } finally {
    redeeming.value = false
  }
}

const expiresText = computed(() => {
  if (!auth.isMember) return i18n.t('membership.currentFree')
  const exp = auth.user?.plan_expires_at
  if (!exp) return i18n.t('membership.currentMember', { expires: i18n.t('membership.lifetime') })
  return i18n.t('membership.currentMember', {
    expires: i18n.t('membership.expiresOn', { date: formatDateTime(exp) }),
  })
})
</script>

<template>
  <div class="bd-mem">
    <header class="bd-mem__hero card">
      <div class="bd-mem__herobody">
        <h1 class="bd-mem__title">{{ i18n.t('membership.title') }}</h1>
        <p class="bd-mem__subtitle">{{ i18n.t('membership.subtitle') }}</p>
        <p class="bd-mem__current">
          <Icon :name="auth.isMember ? 'crown' : 'user'" :size="15" />
          {{ expiresText }}
        </p>
      </div>
      <QuotaMeter />
    </header>

    <!-- plans -->
    <div class="bd-mem__plans">
      <template v-if="loading">
        <div v-for="i in 3" :key="i" class="bd-mem__planskel card">
          <BaseSkeleton width="40%" height="15px" />
          <BaseSkeleton width="70%" height="28px" />
          <BaseSkeleton v-for="j in 4" :key="j" height="12px" />
        </div>
      </template>
      <PlanCard
        v-else
        v-for="(p, i) in plans"
        :key="p.id"
        :plan="p"
        :current="p.id === currentPlanId"
        :recommended="p.id === 'member_yearly'"
        :busy="ordering"
        @select="choosePlan"
      />
    </div>

    <p v-if="!loading" class="bd-mem__note">
      <Icon name="info" :size="13" />
      {{ i18n.t('membership.mockNote') }}
    </p>

    <!-- redeem -->
    <RedeemCard :busy="redeeming" @redeem="redeem" />

    <!-- orders -->
    <h2 class="bd-mem__section">{{ i18n.t('membership.orders') }}</h2>
    <OrderTable :orders="orders" @cancel="cancelOrder" />

    <!-- confirm pay -->
    <BaseModal
      :open="Boolean(confirmPay)"
      :title="i18n.t('membership.confirmPayTitle')"
      size="sm"
      @close="confirmPay = null"
    >
      <p class="bd-mem__confirm">
        {{
          i18n.t('membership.confirmPay', {
            amount: confirmPay ? formatPrice(confirmPay.order.amount_cents, confirmPay.order.currency) : '',
            plan: confirmPay?.planName ?? '',
          })
        }}
      </p>
      <p class="bd-mem__mock-note">
        <Icon name="info" :size="13" />
        {{ i18n.t('membership.mockNote') }}
      </p>
      <template #footer>
        <BaseButton variant="ghost" :label="i18n.t('common.cancel')" @click="confirmPay = null">
          {{ i18n.t('common.cancel') }}
        </BaseButton>
        <BaseButton
          variant="primary"
          :loading="paying"
          :label="paying ? i18n.t('membership.paying') : i18n.t('membership.pay')"
          @click="confirmPayment"
        >
          {{ paying ? i18n.t('membership.paying') : i18n.t('membership.pay') }}
        </BaseButton>
      </template>
    </BaseModal>
  </div>
</template>

<style scoped>
.bd-mem {
  display: flex;
  flex-direction: column;
  gap: 16px;
  max-width: 1080px;
  margin: 0 auto;
}
.bd-mem__hero {
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 20px;
  align-items: center;
  padding: 18px;
}
.bd-mem__herobody {
  min-width: 0;
}
.bd-mem__title {
  margin: 0;
  font-size: 18px;
  font-weight: 650;
  letter-spacing: -0.015em;
}
.bd-mem__subtitle {
  margin: 4px 0 0;
  font-size: 13px;
  color: var(--app-text-muted);
}
.bd-mem__current {
  margin: 12px 0 0;
  display: inline-flex;
  align-items: center;
  gap: 7px;
  padding: 7px 12px;
  border-radius: 999px;
  font-size: 12.5px;
  color: var(--app-accent);
  background-color: var(--app-accent-soft);
  border: 1px solid color-mix(in srgb, var(--app-accent) 26%, transparent);
}
.bd-mem__plans {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 14px;
  align-items: stretch;
}
.bd-mem__planskel {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 16px;
}
.bd-mem__note,
.bd-mem__mock-note {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  margin: 0;
  color: var(--app-text-dim);
  font-size: 12px;
}
.bd-mem__confirm {
  margin: 0;
  font-size: 13.5px;
  color: var(--app-text);
}
.bd-mem__mock-note {
  margin-top: 10px;
  color: var(--app-text-dim);
}
.bd-mem__section {
  margin: 6px 0 -6px;
  font-size: 13px;
  font-weight: 600;
  color: var(--app-text-muted);
}
@media (max-width: 900px) {
  .bd-mem__plans {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 760px) {
  .bd-mem__hero {
    grid-template-columns: 1fr;
  }
}
</style>
