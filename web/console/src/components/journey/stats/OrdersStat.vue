<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// "38 · Agent orders this week" — the Live stage's good news.
//
// A metric, not a task: nothing here is work for anyone, which is exactly
// why it must not wear the task card's clothes.
//
// Rolled up client-side from GET /orders over the last seven days. There is
// no orders summary endpoint, and the Path is the only place that wants this
// shape, so the rollup stays here rather than in the orders store.

import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useStoresStore } from '@/stores/stores'
import { useOrdersStore } from '@/stores/orders'
import { dominantCurrency, formatMoney, sumMoney } from '@/lib/money'
import { useWidgetData } from '@/components/task/widgets/useWidgetData'
import StatTile from './StatTile.vue'

const WEEK_MS = 7 * 24 * 60 * 60 * 1000

const stores = useStoresStore()
const orders = useOrdersStore()
const { t } = useI18n()

useWidgetData(async () => {
  if (!orders.loaded && !orders.loading) await orders.load()
})

const thisWeek = computed(() => {
  const cutoff = Date.now() - WEEK_MS
  return orders.orders.filter((o) => {
    if (!o.placed_at) return false
    const t = new Date(o.placed_at).getTime()
    return !Number.isNaN(t) && t >= cutoff
  })
})

const count = computed(() => thisWeek.value.length)

const total = computed(() => {
  const values = thisWeek.value.map((o) => o.value)
  const currency = dominantCurrency(values)
  return currency ? formatMoney(sumMoney(values, currency)) : null
})

const note = computed(() => {
  const platform = stores.currentStore?.platform ?? t('journey.ordersStat.yourStoreFallback')
  if (count.value === 0) return t('journey.ordersStat.zeroNote', { platform })
  return total.value
    ? t('journey.ordersStat.totalNote', { total: total.value, platform })
    : t('journey.ordersStat.flowingNote', { platform })
})
</script>

<template>
  <StatTile
    :value="String(count)"
    :tone="count > 0 ? 'ok' : 'muted'"
    :label="t('journey.ordersStat.label')"
    :note="note"
  />
</template>
