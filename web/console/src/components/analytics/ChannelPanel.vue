<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Channel Dashboard page shell (R1-R3, R14-R17). Gated behind TierGate at
// the same analytics tier as the Lost Revenue Ledger (lib/channel.ts's
// isAnalyticsTier re-export). Period + surface filter round-trip through
// the URL (R2/R3) so the page is bookmarkable/shareable, matching
// LostRevenuePanel's convention.
//
// Every section (KPIs, funnel, trend, comparison) loads and renders
// independently via its own store action + loading/error triple (R15) — a
// failing comparison fetch never blanks the KPI row, and each section
// retries on its own.
//
// Scoping note: every request here is scoped to the currently selected
// store (stores.currentStoreId) rather than left combined across stores —
// consistent with V1's one-store-per-merchant reality (sarvex-v1-architecture.md), and
// keeps the funnel/daily/comparison numbers attributable to one store even
// though none of them carry a store_id echo of their own on every row.
//
// Currency: channel_summary/channel_funnel/channel_comparison each carry
// their own currency/currency_mixed directly now (resolved server-side —
// see lib/channel.ts's top-of-file note), so each child component derives
// its own display currency from the data it's already handed rather than
// this view threading a shared `currency` prop through everything. The
// Lost Revenue Ledger's currency (used only by the lost-revenue strip
// below) is a separate, unrelated signal — ledger_summary has always
// carried its own currency.

import { computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useChannelStore } from '@/stores/channel'
import { useLedgerStore } from '@/stores/ledger'
import { useBillingStore } from '@/stores/billing'
import { useStoresStore } from '@/stores/stores'
import {
  isAnalyticsTier,
  isValidRange,
  monthKey,
  resolveChannelPeriod,
  surfaceLabel,
  type ChannelPeriodPreset,
} from '@/lib/channel'
import { formatMinorUnits } from '@/lib/money'
import TierGate from '@/components/ui/TierGate.vue'
import Field from '@/components/ui/Field.vue'
import ChannelKpiRow from '@/components/channel/ChannelKpiRow.vue'
import ChannelFunnel from '@/components/channel/ChannelFunnel.vue'
import ChannelTrendChart from '@/components/channel/ChannelTrendChart.vue'
import ChannelComparison from '@/components/channel/ChannelComparison.vue'
import Icon from '@/components/ui/Icon.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const channel = useChannelStore()
const ledger = useLedgerStore()
const billing = useBillingStore()
const stores = useStoresStore()

const unlocked = computed(() => (billing.billing ? isAnalyticsTier(billing.billing.plan) : false))
const billingLoading = computed(() => billing.loading && !billing.billing)

function setQuery(patch: Record<string, string | undefined>) {
  const next: Record<string, string> = {}
  for (const [k, v] of Object.entries({ ...route.query, ...patch })) {
    if (typeof v === 'string' && v !== '') next[k] = v
  }
  router.replace({ query: next })
}

const PRESETS: ChannelPeriodPreset[] = ['this_month', 'last_month', 'last_90_days', 'custom']

const periodPreset = computed<ChannelPeriodPreset>(() => {
  const q = route.query.period
  return typeof q === 'string' && (PRESETS as string[]).includes(q) ? (q as ChannelPeriodPreset) : 'this_month'
})

const customFrom = computed(() => (typeof route.query.from === 'string' ? route.query.from : ''))
const customTo = computed(() => (typeof route.query.to === 'string' ? route.query.to : ''))

const customRangeError = computed(() => {
  if (periodPreset.value !== 'custom') return null
  if (!customFrom.value || !customTo.value) return t('channel.period.customIncomplete')
  if (!isValidRange(customFrom.value, customTo.value)) return t('channel.period.rangeTooLong')
  return null
})

const period = computed(() => {
  if (periodPreset.value === 'custom') {
    if (customRangeError.value) return null
    return resolveChannelPeriod('custom', { from: customFrom.value, to: customTo.value })
  }
  return resolveChannelPeriod(periodPreset.value)
})

function setPreset(preset: ChannelPeriodPreset) {
  if (preset === 'custom') {
    setQuery({ period: preset, from: customFrom.value || undefined, to: customTo.value || undefined })
  } else {
    setQuery({ period: preset, from: undefined, to: undefined })
  }
}
function setCustomFrom(v: string) {
  setQuery({ period: 'custom', from: v })
}
function setCustomTo(v: string) {
  setQuery({ period: 'custom', to: v })
}

const surfaceParam = computed(() =>
  typeof route.query.surface === 'string' && route.query.surface ? route.query.surface : undefined,
)
function setSurface(v: string) {
  setQuery({ surface: v || undefined })
}

// Driven entirely by GET /channel/summary's response, never a hardcoded
// enum (R3) — a surface added server-side appears here as soon as the
// summary response includes it, with no frontend change.
const surfaceOptions = computed(() => channel.availableSurfaces)

const storeId = computed(() => stores.currentStoreId ?? undefined)

const kpiRow = computed(() => {
  const want = surfaceParam.value ?? 'all'
  return channel.summaries.find((s) => (s.surface ?? 'all') === want) ?? null
})

const ledgerMonth = computed(() => period.value?.summaryMonth ?? (period.value ? monthKey(new Date(`${period.value.to}T00:00:00Z`)) : null))
const ledgerViewHref = computed(() =>
  ledgerMonth.value
    ? { path: '/analytics', query: { tab: 'lost-revenue', period: ledgerMonth.value } }
    : { path: '/analytics', query: { tab: 'lost-revenue' } },
)

function loadSummary() {
  if (!unlocked.value || !period.value) return
  channel.loadSummary({ month: period.value.summaryMonth, store_id: storeId.value })
}
function loadFunnel() {
  if (!unlocked.value || !period.value) return
  channel.loadFunnel({ from: period.value.from, to: period.value.to, surface: surfaceParam.value, store_id: storeId.value })
}
function loadDaily() {
  if (!unlocked.value || !period.value) return
  channel.loadDaily({ from: period.value.from, to: period.value.to, surface: surfaceParam.value, store_id: storeId.value })
}
function loadComparison() {
  if (!unlocked.value || !period.value) return
  channel.loadComparison({ from: period.value.from, to: period.value.to, store_id: storeId.value })
}
function loadLostRevenue() {
  if (!unlocked.value || !ledgerMonth.value) return
  ledger.loadSummaries(ledgerMonth.value)
}

function refreshAll() {
  loadSummary()
  loadFunnel()
  loadDaily()
  loadComparison()
  loadLostRevenue()
}

onMounted(async () => {
  if (!route.query.period) setQuery({ period: 'this_month' })
  stores.load()
  await billing.load()
  if (unlocked.value) refreshAll()
})

watch([period, surfaceParam, storeId], () => {
  if (unlocked.value) refreshAll()
})
</script>

<template>
  <div class="panel">
    <TierGate
      :unlocked="unlocked"
      :loading="billingLoading"
      :locked-title="t('channel.locked.title')"
      :locked-description="t('channel.locked.description')"
      :upgrade-cta="t('channel.locked.cta')"
    >
      <div class="controls card">
        <Field :label="t('channel.period.label')" class="period-field">
          <select :value="periodPreset" @change="setPreset(($event.target as HTMLSelectElement).value as ChannelPeriodPreset)">
            <option value="this_month">{{ t('channel.period.thisMonth') }}</option>
            <option value="last_month">{{ t('channel.period.lastMonth') }}</option>
            <option value="last_90_days">{{ t('channel.period.last90Days') }}</option>
            <option value="custom">{{ t('channel.period.custom') }}</option>
          </select>
        </Field>
        <template v-if="periodPreset === 'custom'">
          <Field :label="t('channel.period.from')">
            <input type="date" :value="customFrom" @change="setCustomFrom(($event.target as HTMLInputElement).value)" />
          </Field>
          <Field :label="t('channel.period.to')">
            <input type="date" :value="customTo" @change="setCustomTo(($event.target as HTMLInputElement).value)" />
          </Field>
        </template>
        <Field :label="t('channel.surface.label')" class="surface-field">
          <select :value="surfaceParam ?? ''" @change="setSurface(($event.target as HTMLSelectElement).value)">
            <option value="">{{ t('channel.surface.all') }}</option>
            <option v-for="s in surfaceOptions" :key="s" :value="s">{{ surfaceLabel(t, s) }}</option>
          </select>
        </Field>
      </div>
      <p v-if="customRangeError" class="range-error">
        <Icon name="info" size="14" />
        {{ customRangeError }}
      </p>

      <ChannelKpiRow
        :row="kpiRow"
        :loading="channel.summaryLoading && !channel.summaryLoaded"
        :error="channel.summaryError"
        @retry="loadSummary"
      />

      <div v-if="ledger.currencies.length" class="lost-revenue-strip card">
        <div class="lr-label">{{ t('channel.lostRevenue.label') }}</div>
        <div class="lr-values">
          <span v-for="curr in ledger.currencies" :key="curr" class="lr-value-group">
            <span class="lr-currency-tag mono">{{ curr }}</span>
            <span class="lr-value mono">{{ formatMinorUnits(ledger.summaries[curr]?.total_lost_revenue_minor ?? 0, curr) }}</span>
          </span>
        </div>
        <router-link :to="ledgerViewHref" class="lr-link">
          {{ t('channel.lostRevenue.viewLedger') }}
          <Icon name="arrowR" size="13" />
        </router-link>
      </div>

      <ChannelFunnel
        :funnel="channel.funnel"
        :loading="channel.funnelLoading && !channel.funnelLoaded"
        :error="channel.funnelError"
        @retry="loadFunnel"
      />

      <ChannelTrendChart
        :daily="channel.daily"
        :loading="channel.dailyLoading && !channel.dailyLoaded"
        :error="channel.dailyError"
        @retry="loadDaily"
      />

      <ChannelComparison
        :comparison="channel.comparison"
        :loading="channel.comparisonLoading && !channel.comparisonLoaded"
        :error="channel.comparisonError"
        @retry="loadComparison"
      />
    </TierGate>
  </div>
</template>

<style scoped>
.panel { display: flex; flex-direction: column; gap: 22px; }

.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}
.controls { display: flex; align-items: flex-end; gap: 18px; flex-wrap: wrap; padding: 16px 20px; }
.controls :deep(.field) { margin-bottom: 0; min-width: 170px; }
.range-error { display: flex; align-items: center; gap: 6px; color: var(--danger); font-size: 13px; font-weight: 600; margin: -10px 0 0; }

.lost-revenue-strip {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 16px 20px;
}
.lr-label { font-size: 13px; font-weight: 600; color: var(--muted); }
.lr-values { display: flex; align-items: baseline; gap: 18px; flex-wrap: wrap; }
.lr-value-group { display: inline-flex; align-items: baseline; gap: 6px; }
.lr-currency-tag { font-size: 10.5px; font-weight: 700; letter-spacing: 0.05em; color: var(--faint); }
.lr-value { font-size: 20px; font-weight: 600; color: var(--danger); }
.lr-link {
  margin-left: auto;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 13px;
  font-weight: 600;
  color: var(--accent-deep);
}
.lr-link:hover { text-decoration: underline; }

@media (max-width: 720px) {
  .lost-revenue-strip { flex-wrap: wrap; }
  .lr-link { margin-left: 0; }
}
</style>
