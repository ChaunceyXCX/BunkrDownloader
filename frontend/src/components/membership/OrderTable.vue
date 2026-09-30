<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Order } from '@/api/types'
import { useI18nStore } from '@/stores/i18n'
import { useFormat } from '@/composables/useFormat'
import { useStatusLabel } from '@/composables/useStatus'
import BaseBadge from '@/components/ui/BaseBadge.vue'
import BaseButton from '@/components/ui/BaseButton.vue'
import BaseEmptyState from '@/components/ui/BaseEmptyState.vue'
import Icon from '@/components/ui/Icon.vue'
import { useToast } from '@/components/ui/useToast'

const props = defineProps<{ orders: Order[] }>()
const emit = defineEmits<{ cancel: [order: Order] }>()

const i18n = useI18nStore()
const toast = useToast()
const { formatPrice, formatDateTime } = useFormat(() => i18n.locale)
const { orderLabel, tone } = useStatusLabel()

const copied = ref<number | null>(null)
let copyTimer: number | undefined

async function copyTradeNo(order: Order): Promise<void> {
  try {
    await navigator.clipboard.writeText(order.trade_no)
    copied.value = order.id
    toast.success(i18n.t('common.copied'))
    if (copyTimer) window.clearTimeout(copyTimer)
    copyTimer = window.setTimeout(() => {
      copied.value = null
    }, 1600)
  } catch {
    toast.error(i18n.t('common.copyFailed'))
  }
}

const empty = computed(() => props.orders.length === 0)
</script>

<template>
  <div class="bd-orders card">
    <template v-if="empty">
      <BaseEmptyState
        :title="i18n.t('membership.noOrders')"
        :hint="i18n.t('membership.noOrdersHint')"
        icon="credit-card"
        compact
      />
    </template>

    <table v-else class="bd-order__table">
      <thead>
        <tr>
          <th scope="col">ID</th>
          <th scope="col">{{ i18n.t('membership.amount') }}</th>
          <th scope="col">{{ i18n.t('membership.tradeNo') }}</th>
          <th scope="col">{{ i18n.t('membership.created') }}</th>
          <th scope="col">{{ i18n.t('membership.paid') }}</th>
          <th scope="col">{{ i18n.t('common.status') }}</th>
          <th scope="col" />
        </tr>
      </thead>
      <tbody>
        <tr v-for="order in orders" :key="order.id">
          <td class="bd-order__id" data-numeric>#{{ order.id }}</td>
          <td class="bd-order__amount" data-numeric>{{ formatPrice(order.amount_cents, order.currency) }}</td>
          <td class="bd-order__tradeno">
            <span class="bd-order__mono" data-numeric>{{ order.trade_no }}</span>
            <button
              type="button"
              class="bd-order__copy"
              :aria-label="`${i18n.t('common.copy')}: ${order.trade_no}`"
              :title="i18n.t('common.copy')"
              @click="copyTradeNo(order)"
            >
              <Icon :name="copied === order.id ? 'check' : 'copy'" :size="14" />
            </button>
          </td>
          <td class="bd-order__date" data-numeric>{{ formatDateTime(order.created_at) }}</td>
          <td class="bd-order__date" data-numeric>{{ formatDateTime(order.paid_at) }}</td>
          <td><BaseBadge :tone="tone(order.status)" size="sm">{{ orderLabel(order.status) }}</BaseBadge></td>
          <td class="bd-order__ops">
            <BaseButton
              v-if="order.status === 'pending'"
              variant="ghost"
              size="sm"
              icon="trash"
              :label="i18n.t('membership.cancelOrder')"
              @click="emit('cancel', order)"
            >
              {{ i18n.t('membership.cancelOrder') }}
            </BaseButton>
          </td>
        </tr>
      </tbody>
    </table>

    <p v-if="!empty" class="bd-order__mock">
      <Icon name="info" :size="13" />
      {{ i18n.t('membership.mockNote') }}
    </p>
  </div>
</template>

<style scoped>
.bd-orders {
  overflow: hidden;
}
.bd-order__table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12.5px;
}
.bd-order__table th {
  text-align: left;
  padding: 9px 12px;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: var(--app-text-dim);
  border-bottom: 1px solid var(--app-border);
  background-color: var(--app-surface-3);
}
.bd-order__table td {
  padding: 9px 12px;
  border-bottom: 1px solid var(--app-border);
  color: var(--app-text-muted);
  vertical-align: middle;
}
.bd-order__table tr:hover td {
  background-color: var(--app-surface-hover);
}
.bd-order__id {
  font-weight: 600;
  color: var(--app-text);
}
.bd-order__tradeno {
  display: flex;
  align-items: center;
  gap: 6px;
  white-space: nowrap;
}
.bd-order__mono {
  font-family: var(--font-mono);
  font-size: 11.5px;
  color: var(--app-text);
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 180px;
}
.bd-order__copy {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border-radius: 6px;
  border: 0;
  background: transparent;
  color: var(--app-text-dim);
  cursor: pointer;
  flex: 0 0 auto;
}
.bd-order__copy:hover {
  color: var(--app-accent);
  background-color: var(--app-accent-soft);
}
.bd-order__ops {
  text-align: right;
}
.bd-order__mock {
  margin: 0;
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 12px;
  font-size: 11.5px;
  color: var(--app-text-dim);
  background-color: var(--app-surface-3);
}
@media (max-width: 767px) {
  .bd-order__table {
    display: block;
  }
  .bd-order__table thead {
    display: none;
  }
  .bd-order__table tbody {
    display: block;
  }
  .bd-order__table tr {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 2px 10px;
    padding: 8px 12px;
  }
  .bd-order__table td {
    border: 0;
    padding: 2px 0;
  }
  .bd-order__id {
    grid-column: 1;
  }
  .bd-order__amount {
    grid-column: 2;
    text-align: right;
  }
  .bd-order__tradeno {
    grid-column: 1 / -1;
  }
  .bd-order__ops {
    grid-column: 1 / -1;
    text-align: left;
  }
}
</style>
