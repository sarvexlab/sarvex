<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Lost Revenue Ledger page shell (R1/R2). Gated behind TierGate; the month
// selector and every filter round-trip through the URL so a merchant can
// share/bookmark a filtered link, and so the Channel Dashboard's
// "View ledger →" link can land here pre-scoped to a period via ?period=.
//
// Store scoping: like every other analytics tab, entries are scoped to
// stores.currentStoreId (the header StoreSwitcher's selection) instead of
// a panel-owned store filter. GET /ledger/summary has no store_id param at
// all (sarvex-v1-control-plane.yaml) — it's a merchant-wide monthly
// rollup — so the summary header + cause breakdown stay merchant-wide
// regardless of the selected store; LedgerSummaryHeader.vue renders a
// small note saying so rather than letting that read as a scoping bug.

import { computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useLedgerStore, monthRange } from '@/stores/ledger'
import { useBillingStore } from '@/stores/billing'
import { useStoresStore } from '@/stores/stores'
import { causeLabel, currentMonth, isAnalyticsTier, isValidMonth, LEDGER_CAUSES, LEDGER_ENTRY_STATUSES } from '@/lib/ledger'
import type { ListLedgerEntriesFilter } from '@/api/ledger'
import TierGate from '@/components/ui/TierGate.vue'
import LedgerSummaryHeader from '@/components/ledger/LedgerSummaryHeader.vue'
import LedgerCauseBreakdown from '@/components/ledger/LedgerCauseBreakdown.vue'
import LedgerEntriesTable from '@/components/ledger/LedgerEntriesTable.vue'
import Field from '@/components/ui/Field.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import ErrorBanner from '@/components/ui/ErrorBanner.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const ledger = useLedgerStore()
const billing = useBillingStore()
const stores = useStoresStore()

const unlocked = computed(() => (billing.billing ? isAnalyticsTier(billing.billing.plan) : false))
const billingLoading = computed(() => billing.loading && !billing.billing)

const month = computed(() => {
  const q = route.query.period
  return typeof q === 'string' && isValidMonth(q) ? q : currentMonth()
})

function setQuery(patch: Record<string, string | undefined>) {
  const next: Record<string, string> = {}
  for (const [k, v] of Object.entries({ ...route.query, ...patch })) {
    if (typeof v === 'string' && v !== '') next[k] = v
  }
  router.replace({ query: next })
}

function setMonth(next: string) {
  setQuery({ period: next })
}

const storeId = computed(() => stores.currentStoreId ?? undefined)

const filters = computed<ListLedgerEntriesFilter>(() => {
  const q = route.query
  const { from: monthFrom, to: monthTo } = monthRange(month.value)
  const f: ListLedgerEntriesFilter = {
    from: typeof q.from === 'string' && q.from ? q.from : monthFrom,
    to: typeof q.to === 'string' && q.to ? q.to : monthTo,
  }
  if (typeof q.cause === 'string' && q.cause) f.cause = q.cause as ListLedgerEntriesFilter['cause']
  if (typeof q.status === 'string' && q.status) f.status = q.status as ListLedgerEntriesFilter['status']
  if (storeId.value) f.store_id = storeId.value
  if (typeof q.surface === 'string' && q.surface) f.surface = q.surface
  return f
})

const hasNarrowingFilters = computed(
  () => !!(filters.value.cause || filters.value.status || filters.value.surface),
)

function clearFilters() {
  setQuery({ cause: undefined, status: undefined, surface: undefined, from: undefined, to: undefined })
}

function selectCause(cause: string) {
  setQuery({ cause })
}

async function refresh() {
  await Promise.all([ledger.loadSummaries(month.value), ledger.loadEntries(filters.value)])
}

// The read API is gated server-side at the analytics tier (403
// analytics_tier_required) — there's no reason to hit it while we already
// know from billing state the merchant is locked out, so every fetch here
// waits for billing to resolve and checks `unlocked` first.
onMounted(async () => {
  if (!route.query.period) setQuery({ period: month.value })
  stores.load()
  await billing.load()
  if (unlocked.value) refresh()
})

watch(month, () => {
  if (unlocked.value) ledger.loadSummaries(month.value)
})
watch(filters, () => {
  if (unlocked.value) ledger.loadEntries(filters.value)
})
// Summaries aren't re-fetched on store switch — GET /ledger/summary has no
// store_id param (see module doc above), so the response wouldn't change.
watch(storeId, () => {
  if (unlocked.value) ledger.loadEntries(filters.value)
})

const showSkeleton = computed(() => ledger.entriesLoading && !ledger.entriesLoaded)
const isEmptyPeriod = computed(
  () => ledger.entriesLoaded && ledger.entries.length === 0 && !hasNarrowingFilters.value,
)
const isEmptyFiltered = computed(
  () => ledger.entriesLoaded && ledger.entries.length === 0 && hasNarrowingFilters.value,
)
</script>

<template>
  <div class="panel">
    <TierGate
      :unlocked="unlocked"
      :loading="billingLoading"
      :locked-title="t('ledger.locked.title')"
      :locked-description="t('ledger.locked.description')"
      :upgrade-cta="t('ledger.locked.cta')"
    >
      <div class="head-row">
        <Field :label="t('ledger.monthLabel')" class="month-field">
          <input
            type="month"
            :value="month"
            @change="setMonth(($event.target as HTMLInputElement).value)"
          />
        </Field>
      </div>

      <LedgerSummaryHeader
        :summaries="ledger.summaries"
        :currencies="ledger.currencies"
        :loading="ledger.summariesLoading && !ledger.summariesLoaded"
        :error="ledger.summariesError"
        @retry="ledger.loadSummaries(month)"
      />

      <LedgerCauseBreakdown
        v-if="ledger.currencies.length"
        :summaries="ledger.summaries"
        :currencies="ledger.currencies"
        :sort="ledger.breakdownSort"
        @update:sort="ledger.setBreakdownSort"
        @select-cause="selectCause"
      />

      <div class="filters card">
        <Field :label="t('ledger.entries.filters.cause')">
          <select :value="filters.cause ?? ''" @change="setQuery({ cause: ($event.target as HTMLSelectElement).value })">
            <option value="">{{ t('ledger.entries.filters.allCauses') }}</option>
            <option v-for="c in LEDGER_CAUSES" :key="c" :value="c">{{ causeLabel(t, c) }}</option>
          </select>
        </Field>
        <Field :label="t('ledger.entries.filters.status')">
          <select :value="filters.status ?? ''" @change="setQuery({ status: ($event.target as HTMLSelectElement).value })">
            <option value="">{{ t('ledger.entries.filters.allStatuses') }}</option>
            <option v-for="s in LEDGER_ENTRY_STATUSES" :key="s" :value="s">{{ t(`ledger.entries.status.${s}`) }}</option>
          </select>
        </Field>
        <Field :label="t('ledger.entries.filters.from')">
          <input
            type="date"
            :value="(route.query.from as string | undefined)?.slice(0, 10) ?? ''"
            @change="setQuery({ from: ($event.target as HTMLInputElement).value ? new Date(($event.target as HTMLInputElement).value).toISOString() : undefined })"
          />
        </Field>
        <Field :label="t('ledger.entries.filters.to')">
          <input
            type="date"
            :value="(route.query.to as string | undefined)?.slice(0, 10) ?? ''"
            @change="setQuery({ to: ($event.target as HTMLInputElement).value ? new Date(($event.target as HTMLInputElement).value).toISOString() : undefined })"
          />
        </Field>
        <button type="button" class="clear-btn" @click="clearFilters">
          {{ t('ledger.entries.filters.clear') }}
        </button>
      </div>

      <ErrorBanner v-if="ledger.entriesError" :message="ledger.entriesError" @retry="ledger.loadEntries(filters)" />
      <ErrorBanner
        v-if="ledger.lastStatusError"
        :message="t('ledger.entries.statusUpdateFailed')"
        @retry="ledger.clearStatusError()"
      />
      <div v-if="ledger.bulkResult" class="bulk-result">
        {{ t('ledger.entries.bulk.result', ledger.bulkResult) }}
      </div>

      <div v-if="showSkeleton" class="skel-block">
        <Skeleton height="240px" radius="11px" />
      </div>
      <div v-else-if="isEmptyPeriod" class="card empty-note">
        <div class="big">{{ t('ledger.entries.empty.none.title') }}</div>
        <p>{{ t('ledger.entries.empty.none.body') }}</p>
      </div>
      <div v-else-if="isEmptyFiltered" class="card empty-note">
        <div class="big">{{ t('ledger.entries.empty.filtered.title') }}</div>
        <p>{{ t('ledger.entries.empty.filtered.body') }}</p>
      </div>
      <template v-else>
        <LedgerEntriesTable
          :entries="ledger.entries"
          :status-pending="ledger.statusPending"
          @change-status="(id, status) => ledger.applyStatus(id, status)"
          @bulk-change-status="(ids, status) => ledger.bulkApplyStatus(ids, status)"
        />
        <div v-if="ledger.entriesHasMore" class="load-more">
          <button
            type="button"
            class="load-more-btn"
            :disabled="ledger.entriesLoadingMore"
            @click="ledger.loadMoreEntries()"
          >
            {{ ledger.entriesLoadingMore ? t('ledger.entries.loading') : t('ledger.entries.loadMore') }}
          </button>
        </div>
      </template>
    </TierGate>
  </div>
</template>

<style scoped>
.panel { display: flex; flex-direction: column; }
.head-row { display: flex; align-items: flex-end; justify-content: flex-end; gap: 20px; flex-wrap: wrap; margin-bottom: 18px; }
.month-field { margin-bottom: 0; min-width: 160px; }

.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}
.filters {
  display: flex;
  align-items: flex-end;
  gap: 18px;
  flex-wrap: wrap;
  padding: 16px 20px;
  margin: 22px 0 18px;
}
.filters :deep(.field) { margin-bottom: 0; min-width: 150px; }
.clear-btn { margin-left: auto; color: var(--faint); font-size: 12.5px; font-weight: 600; padding-bottom: 4px; }
.clear-btn:hover { color: var(--ink); }

.bulk-result {
  background: var(--accent-softer);
  color: var(--accent-deep);
  border-radius: var(--radius-sm);
  padding: 10px 16px;
  margin-bottom: 14px;
  font-size: 13px;
  font-weight: 600;
}

.empty-note { text-align: center; padding: 44px 20px; color: var(--muted); }
.empty-note .big {
  font-family: var(--font-display);
  font-weight: var(--display-weight);
  font-size: 20px;
  color: var(--ink);
  margin-bottom: 6px;
}
.empty-note p { max-width: 52ch; margin: 0 auto; font-size: 13.5px; }

.skel-block { display: flex; flex-direction: column; gap: 14px; }

.load-more { display: flex; justify-content: center; margin-top: 18px; }
.load-more-btn {
  padding: 9px 20px;
  border-radius: var(--radius);
  background: var(--surface);
  border: 1px solid var(--line);
  box-shadow: var(--shadow);
  color: var(--ink);
  font-weight: 600;
  font-size: 13px;
}
.load-more-btn:hover:not(:disabled) { box-shadow: var(--shadow-lg); }
.load-more-btn:disabled { opacity: 0.6; }
</style>
