<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Orders tab — agent-checkout list (pre-live: empty state).
// GET /orders → useOrdersStore

import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useStoresStore } from '@/stores/stores'
import { useOrdersStore } from '@/stores/orders'
import Icon from '@/components/ui/Icon.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import ErrorBanner from '@/components/ui/ErrorBanner.vue'

defineEmits<{ retry: [] }>()

const stores = useStoresStore()
const orders = useOrdersStore()
const { t } = useI18n()

const store = computed(() => stores.currentStore)
const showSkeleton = computed(() => orders.loading && !orders.loaded)
</script>

<template>
  <section>
    <ErrorBanner v-if="orders.error" :message="orders.error" @retry="$emit('retry')" />

    <div v-if="showSkeleton" class="card orders-list" aria-hidden="true">
      <div v-for="i in 3" :key="i" class="order-row" :class="{ first: i === 1 }">
        <Skeleton width="34px" height="34px" radius="9px" />
        <div class="o-main skel-stack">
          <Skeleton width="40%" height="14px" />
          <Skeleton width="65%" height="12px" />
        </div>
      </div>
    </div>

    <div v-else-if="orders.orders.length === 0" class="card empty-note">
      <div class="big">{{ t('store.orders.emptyTitle') }}</div>
      {{ t('store.orders.emptyBody', { storeName: store?.name, platform: store?.platform }) }}
    </div>
    <div v-else class="card orders-list">
      <div v-for="(o, i) in orders.orders" :key="o.order_id" class="order-row" :class="{ first: i === 0 }">
        <div class="o-icon"><Icon name="receipt" /></div>
        <div class="o-main">
          <div class="o-title">{{ t('store.orders.orderTitle', { id: o.order_id }) }}</div>
          <div class="o-sub">
            <span class="mono">{{ o.checkout_session_id }}</span> ·
            <span>{{ o.placed_at }}</span>
            <template v-if="o.store_order_ref"> · → <span class="mono">{{ o.store_order_ref }}</span></template>
          </div>
        </div>
        <div class="o-side">
          <div v-if="o.value" class="mono o-amount">
            {{ ((o.value.amount_minor ?? 0) / 100).toFixed(2) }} {{ o.value.currency }}
          </div>
          <div class="o-status">{{ o.status }} · {{ o.sync_state }}</div>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}

.empty-note {
  text-align: center;
  padding: 40px 20px;
  color: var(--muted);
}
.empty-note .big {
  font-family: var(--font-display);
  font-weight: var(--display-weight);
  font-size: 22px;
  color: var(--ink);
  margin-bottom: 8px;
  letter-spacing: var(--display-tracking);
}

.orders-list { overflow: hidden; }
.order-row {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 16px 20px;
  border-top: 1px solid var(--line);
}
.order-row.first { border-top: 0; }
.o-icon {
  width: 34px; height: 34px; border-radius: 9px;
  display: grid; place-items: center;
  background: transparent;
  color: var(--faint);
  box-shadow: inset 0 0 0 1px var(--line2);
  flex-shrink: 0;
}
.o-main { flex: 1; min-width: 0; }
.skel-stack { display: flex; flex-direction: column; gap: 8px; }
.o-title { font-weight: 600; }
.o-sub { font-size: 12.5px; color: var(--faint); margin-top: 3px; }
.o-side { text-align: right; flex-shrink: 0; }
.o-amount { font-weight: 600; }
.o-status { font-size: 12px; color: var(--ok); margin-top: 3px; text-transform: capitalize; }
</style>
