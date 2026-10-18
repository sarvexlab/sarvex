<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// KPI row (R4): checkouts completed, revenue, AOV, completion rate, each
// from GET /channel/summary. Deltas are rendered exactly as the server
// computed them — never derived client-side (R4). prior_period now carries
// aov_minor_delta/completion_rate_delta alongside the original three, so
// all four cards show a real server-computed delta.
//
// Currency (R5): row.currency/row.currency_mixed come straight off the
// response (see lib/channel.ts's top-of-file note). A mixed-currency
// period can't honestly show a single revenue/AOV number or delta, so
// those two cards show an explicit notice instead — never a total that
// silently picked one currency.

import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatMinorUnits } from '@/lib/money'
import { resolveCurrencyState } from '@/lib/channel'
import type { ChannelSummary } from '@/api/channel'
import Skeleton from '@/components/ui/Skeleton.vue'
import ErrorBanner from '@/components/ui/ErrorBanner.vue'

const props = defineProps<{
  row: ChannelSummary | null
  loading: boolean
  error: string | null
}>()
const emit = defineEmits<{ retry: [] }>()

const { t } = useI18n()

function deltaKind(n: number | undefined): 'up' | 'down' | 'flat' {
  if (!n) return 'flat'
  return n > 0 ? 'up' : 'down'
}

const currencyState = computed(() => resolveCurrencyState(props.row?.currency, props.row?.currency_mixed))
const moneyCurrency = computed(() => (currencyState.value.kind === 'known' ? currencyState.value.currency : undefined))
const moneyMixed = computed(() => currencyState.value.kind === 'mixed')

const completionRatePct = computed(() => {
  const r = props.row?.completion_rate
  return typeof r === 'number' ? `${(r * 100).toFixed(1)}%` : '—'
})
function pctPoints(delta: number): string {
  return `${(delta * 100).toFixed(1)}pp`
}

const revenueDelta = computed(() => props.row?.prior_period?.revenue_minor_delta)
const checkoutsDelta = computed(() => props.row?.prior_period?.checkouts_completed_delta)
const aovDelta = computed(() => props.row?.prior_period?.aov_minor_delta)
const completionRateDelta = computed(() => props.row?.prior_period?.completion_rate_delta)
</script>

<template>
  <div class="kpi-row">
    <ErrorBanner v-if="error" :message="error" @retry="emit('retry')" />
    <div v-if="loading" class="skel-grid">
      <Skeleton v-for="i in 4" :key="i" height="120px" radius="var(--radius)" />
    </div>
    <div v-else-if="row" class="cards">
      <div class="card kpi-card">
        <div class="kpi-label">{{ t('channel.kpi.checkoutsCompleted') }}</div>
        <div class="kpi-value mono display">{{ row.checkouts_completed ?? 0 }}</div>
        <div v-if="checkoutsDelta !== undefined" class="delta" :class="deltaKind(checkoutsDelta)">
          {{ t('channel.kpi.deltaVsPrior', { amount: Math.abs(checkoutsDelta ?? 0) }) }}
        </div>
      </div>
      <div class="card kpi-card">
        <div class="kpi-label">{{ t('channel.kpi.revenue') }}</div>
        <template v-if="moneyMixed">
          <div class="kpi-value mono display mixed-note">{{ t('channel.kpi.mixedCurrency') }}</div>
        </template>
        <template v-else>
          <div class="kpi-value mono display">{{ formatMinorUnits(row.revenue_minor ?? 0, moneyCurrency) }}</div>
          <div v-if="revenueDelta !== undefined" class="delta" :class="deltaKind(revenueDelta)">
            {{ t('channel.kpi.deltaVsPrior', { amount: formatMinorUnits(Math.abs(revenueDelta ?? 0), moneyCurrency) }) }}
          </div>
        </template>
      </div>
      <div class="card kpi-card">
        <div class="kpi-label">{{ t('channel.kpi.aov') }}</div>
        <template v-if="moneyMixed">
          <div class="kpi-value mono display mixed-note">{{ t('channel.kpi.mixedCurrency') }}</div>
        </template>
        <template v-else>
          <div class="kpi-value mono display">{{ formatMinorUnits(row.aov_minor ?? 0, moneyCurrency) }}</div>
          <div v-if="aovDelta !== undefined" class="delta" :class="deltaKind(aovDelta)">
            {{ t('channel.kpi.deltaVsPrior', { amount: formatMinorUnits(Math.abs(aovDelta ?? 0), moneyCurrency) }) }}
          </div>
        </template>
      </div>
      <div class="card kpi-card">
        <div class="kpi-label">{{ t('channel.kpi.completionRate') }}</div>
        <div class="kpi-value mono display">{{ completionRatePct }}</div>
        <div v-if="completionRateDelta !== undefined" class="delta" :class="deltaKind(completionRateDelta)">
          {{ t('channel.kpi.deltaVsPriorPlain', { amount: pctPoints(Math.abs(completionRateDelta ?? 0)) }) }}
        </div>
      </div>
    </div>
    <div v-else class="card empty-note">
      {{ t('channel.kpi.empty') }}
    </div>
  </div>
</template>

<style scoped>
.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}
.cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 16px; }
.skel-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 16px; }
.kpi-card { padding: 20px 22px; }
.kpi-label { font-size: 12.5px; font-weight: 600; color: var(--muted); margin-bottom: 8px; }
.kpi-value { font-size: 30px; font-weight: 600; line-height: 1.1; margin-bottom: 8px; }
.kpi-value.mixed-note { font-size: 15px; font-weight: 600; color: var(--warn); line-height: 1.4; }
.delta { font-size: 12.5px; font-weight: 600; }
.delta.up { color: var(--ok); }
.delta.down { color: var(--danger); }
.delta.flat { color: var(--faint); font-weight: 500; }
.empty-note { padding: 32px; text-align: center; color: var(--muted); }
</style>
