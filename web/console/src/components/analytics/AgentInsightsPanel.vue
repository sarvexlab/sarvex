<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Agent Behavior Insights page shell (R1-R3, R15). Gated behind TierGate at
// the same analytics tier as the Lost Revenue Ledger / Channel Dashboard.
// Two independent period controls round-trip through the URL: `month`
// (yyyy-MM) drives Capability gaps, since GET /insights/capability_gaps is
// month-scoped like the Ledger; `period`/`from`/`to` (the Channel
// Dashboard's exact preset scheme, reused rather than re-implemented — the
// 92-day-cap date math is identical, and sarvex-v1-control-plane.yaml's own
// insights.go doc comment says the range params mirror channelDateRange's
// rule exactly) drives Negotiation and Product demand, both range-scoped.
// `surface` (Product demand only) round-trips too.
//
// Four sections in the order R3 requires — Capability gaps, Negotiation,
// Product demand, Attribute gaps — each loading and erroring independently
// (R15): a failing negotiation fetch never blanks the capability-gap cards.
// Attribute gaps has no backend endpoint (see AttributeGapsSection.vue) so
// it has no load call of its own.
//
// Store scoping: like every other analytics tab, Capability gaps is scoped
// to stores.currentStoreId (the header StoreSwitcher's selection) — the
// endpoint already accepts store_id (api/insights.ts's
// listCapabilityGaps). Negotiation and Product demand cannot be
// store-scoped — GET /insights/negotiation and GET /insights/product_demand
// have no store_id param at all (sarvex-v1-control-plane.yaml), so they
// stay merchant-wide regardless of the selected store; their sections
// render a small note saying so rather than letting that read as a
// scoping bug.

import { computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useInsightsStore } from '@/stores/insights'
import { useBillingStore } from '@/stores/billing'
import { useStoresStore } from '@/stores/stores'
import { isAnalyticsTier, surfaceLabel } from '@/lib/insights'
import { currentMonth, isValidMonth } from '@/lib/ledger'
import { isValidRange, resolveChannelPeriod, type ChannelPeriodPreset } from '@/lib/channel'
import TierGate from '@/components/ui/TierGate.vue'
import Field from '@/components/ui/Field.vue'
import Icon from '@/components/ui/Icon.vue'
import CapabilityGapsSection from '@/components/insights/CapabilityGapsSection.vue'
import NegotiationSection from '@/components/insights/NegotiationSection.vue'
import ProductDemandSection from '@/components/insights/ProductDemandSection.vue'
import AttributeGapsSection from '@/components/insights/AttributeGapsSection.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const insights = useInsightsStore()
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

// ── Capability gaps: month (R2) ──────────────────────────────────────────

const gapsMonth = computed(() => {
  const q = route.query.month
  return typeof q === 'string' && isValidMonth(q) ? q : currentMonth()
})
function setGapsMonth(next: string) {
  setQuery({ month: next })
}

// ── Negotiation / Product demand: date range (R2) ────────────────────────

const PRESETS: ChannelPeriodPreset[] = ['this_month', 'last_month', 'last_90_days', 'custom']

const periodPreset = computed<ChannelPeriodPreset>(() => {
  const q = route.query.period
  return typeof q === 'string' && (PRESETS as string[]).includes(q) ? (q as ChannelPeriodPreset) : 'this_month'
})
const customFrom = computed(() => (typeof route.query.from === 'string' ? route.query.from : ''))
const customTo = computed(() => (typeof route.query.to === 'string' ? route.query.to : ''))

const customRangeError = computed(() => {
  if (periodPreset.value !== 'custom') return null
  if (!customFrom.value || !customTo.value) return t('insights.period.customIncomplete')
  if (!isValidRange(customFrom.value, customTo.value)) return t('insights.period.rangeTooLong')
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

// ── Product demand: surface filter (R2 — driven by API data, never a
// hardcoded list; see stores/insights.ts's productDemandSurfacesSeen) + top-N ──

const surfaceParam = computed(() =>
  typeof route.query.surface === 'string' && route.query.surface ? route.query.surface : undefined,
)
function setSurface(v: string) {
  setQuery({ surface: v || undefined })
}
const surfaceOptions = computed(() => insights.productDemandSurfaces)

const demandLimit = computed(() => {
  const q = route.query.limit
  const n = typeof q === 'string' ? Number(q) : NaN
  return Number.isFinite(n) && n > 0 ? n : 100
})
function setDemandLimit(n: number) {
  setQuery({ limit: String(n) })
}

// ── Loaders ───────────────────────────────────────────────────────────────

function loadCapabilityGaps() {
  if (!unlocked.value) return
  insights.loadCapabilityGaps({ month: gapsMonth.value, store_id: stores.currentStoreId ?? undefined })
}
function loadNegotiation() {
  if (!unlocked.value || !period.value) return
  insights.loadNegotiation({ from: period.value.from, to: period.value.to })
}
function loadProductDemand() {
  if (!unlocked.value || !period.value) return
  insights.loadProductDemand({ from: period.value.from, to: period.value.to, surface: surfaceParam.value, limit: demandLimit.value })
}

onMounted(async () => {
  const defaults: Record<string, string | undefined> = {}
  if (!route.query.month) defaults.month = gapsMonth.value
  if (!route.query.period) defaults.period = 'this_month'
  if (Object.keys(defaults).length) setQuery(defaults)
  stores.load()
  await billing.load()
  if (unlocked.value) {
    loadCapabilityGaps()
    loadNegotiation()
    loadProductDemand()
  }
})

watch(gapsMonth, () => {
  if (unlocked.value) loadCapabilityGaps()
})
watch([period, surfaceParam, demandLimit], () => {
  if (unlocked.value) {
    loadNegotiation()
    loadProductDemand()
  }
})
// Negotiation/Product demand deliberately excluded — see module doc above,
// neither endpoint accepts store_id so re-fetching on store switch would
// be a wasted, identical request.
watch(
  () => stores.currentStoreId,
  () => {
    if (unlocked.value) loadCapabilityGaps()
  },
)
</script>

<template>
  <div class="panel">
    <TierGate
      :unlocked="unlocked"
      :loading="billingLoading"
      :locked-title="t('insights.locked.title')"
      :locked-description="t('insights.locked.description')"
      :upgrade-cta="t('insights.locked.cta')"
    >
      <div class="controls card">
        <Field :label="t('insights.gapsMonth.label')" class="month-field">
          <input type="month" :value="gapsMonth" @change="setGapsMonth(($event.target as HTMLInputElement).value)" />
        </Field>
        <Field :label="t('insights.period.label')" class="period-field">
          <select :value="periodPreset" @change="setPreset(($event.target as HTMLSelectElement).value as ChannelPeriodPreset)">
            <option value="this_month">{{ t('insights.period.thisMonth') }}</option>
            <option value="last_month">{{ t('insights.period.lastMonth') }}</option>
            <option value="last_90_days">{{ t('insights.period.last90Days') }}</option>
            <option value="custom">{{ t('insights.period.custom') }}</option>
          </select>
        </Field>
        <template v-if="periodPreset === 'custom'">
          <Field :label="t('insights.period.from')">
            <input type="date" :value="customFrom" @change="setCustomFrom(($event.target as HTMLInputElement).value)" />
          </Field>
          <Field :label="t('insights.period.to')">
            <input type="date" :value="customTo" @change="setCustomTo(($event.target as HTMLInputElement).value)" />
          </Field>
        </template>
        <Field :label="t('insights.surface.label')" class="surface-field">
          <select :value="surfaceParam ?? ''" @change="setSurface(($event.target as HTMLSelectElement).value)">
            <option value="">{{ t('insights.surface.all') }}</option>
            <option v-for="s in surfaceOptions" :key="s" :value="s">{{ surfaceLabel(t, s) }}</option>
          </select>
        </Field>
      </div>
      <p v-if="customRangeError" class="range-error">
        <Icon name="info" size="14" />
        {{ customRangeError }}
      </p>

      <CapabilityGapsSection
        :gaps="insights.capabilityGaps"
        :loading="insights.capabilityGapsLoading && !insights.capabilityGapsLoaded"
        :error="insights.capabilityGapsError"
        @retry="loadCapabilityGaps"
      />

      <NegotiationSection
        :negotiation="insights.negotiation"
        :loading="insights.negotiationLoading && !insights.negotiationLoaded"
        :error="insights.negotiationError"
        @retry="loadNegotiation"
      />

      <ProductDemandSection
        :rows="insights.productDemand"
        :loading="insights.productDemandLoading && !insights.productDemandLoaded"
        :error="insights.productDemandError"
        :limit="demandLimit"
        @retry="loadProductDemand"
        @update:limit="setDemandLimit"
      />

      <AttributeGapsSection />
    </TierGate>
  </div>
</template>

<style scoped>
.panel { display: flex; flex-direction: column; gap: 28px; }

.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}
.controls { display: flex; align-items: flex-end; gap: 18px; flex-wrap: wrap; padding: 16px 20px; }
.controls :deep(.field) { margin-bottom: 0; min-width: 160px; }
.range-error { display: flex; align-items: center; gap: 6px; color: var(--danger); font-size: 13px; font-weight: 600; margin: -18px 0 0; }

@media (max-width: 720px) {
  .panel { gap: 22px; }
}
</style>
