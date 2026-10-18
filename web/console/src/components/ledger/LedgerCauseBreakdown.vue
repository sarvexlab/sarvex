<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Cause breakdown: horizontal bars, sorted by lost revenue by default (R6).
// buyer_abandoned is split into its own group (R7) — it's the buyer's
// choice, not something fixable, and mixing it into the fixable list makes
// the whole list read as broken.
//
// One block per currency the merchant actually has entries in this month —
// a cause's lost_revenue_minor is money, so it can never be pooled across
// currencies the way the summary header's totals can't either.

import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatMinorUnits } from '@/lib/money'
import { causeLabel, isFixableCause, sortCausesByCount, sortCausesByRevenue } from '@/lib/ledger'
import type { LedgerSummary } from '@/api/ledger'
import type { BreakdownSort } from '@/stores/ledger'

const props = defineProps<{
  summaries: Record<string, LedgerSummary>
  currencies: string[]
  sort: BreakdownSort
}>()
const emit = defineEmits<{
  'update:sort': [sort: BreakdownSort]
  'select-cause': [cause: string]
}>()

const { t } = useI18n()

function rowsFor(currency: string) {
  const causes = props.summaries[currency]?.causes ?? []
  const sorted = props.sort === 'count' ? sortCausesByCount(causes) : sortCausesByRevenue(causes)
  return {
    fixable: sorted.filter((c) => isFixableCause(c.cause)),
    abandoned: sorted.filter((c) => !isFixableCause(c.cause)),
  }
}

function maxRevenue(rows: LedgerSummary['causes']): number {
  return Math.max(1, ...(rows ?? []).map((r) => r.lost_revenue_minor ?? 0))
}

function barWidth(minor: number | undefined, max: number): string {
  return `${Math.max(2, Math.round(((minor ?? 0) / max) * 100))}%`
}

const blocks = computed(() => props.currencies.map((currency) => ({ currency, ...rowsFor(currency) })))
</script>

<template>
  <div class="breakdown-wrap">
    <div class="breakdown-head">
      <h2 class="section-title display">{{ t('ledger.causes.title') }}</h2>
      <div class="sort-toggle" role="tablist" :aria-label="t('ledger.causes.sortAriaLabel')">
        <button
          type="button"
          role="tab"
          :aria-selected="sort === 'revenue'"
          :class="{ active: sort === 'revenue' }"
          @click="emit('update:sort', 'revenue')"
        >
          {{ t('ledger.causes.sortByRevenue') }}
        </button>
        <button
          type="button"
          role="tab"
          :aria-selected="sort === 'count'"
          :class="{ active: sort === 'count' }"
          @click="emit('update:sort', 'count')"
        >
          {{ t('ledger.causes.sortByCount') }}
        </button>
      </div>
    </div>

    <div v-for="block in blocks" :key="block.currency" class="currency-block">
      <div v-if="currencies.length > 1" class="currency-label mono">{{ block.currency }}</div>

      <div v-if="block.fixable.length" class="card bars-card">
        <div
          v-for="row in block.fixable"
          :key="row.cause"
          class="bar-row"
          role="button"
          tabindex="0"
          @click="row.cause && emit('select-cause', row.cause)"
          @keydown.enter="row.cause && emit('select-cause', row.cause)"
        >
          <div class="bar-label">{{ causeLabel(t, row.cause) }}</div>
          <div class="bar-track">
            <div class="bar-fill" :style="{ width: barWidth(row.lost_revenue_minor, maxRevenue(block.fixable)) }" />
          </div>
          <div class="bar-value mono">{{ formatMinorUnits(row.lost_revenue_minor ?? 0, block.currency) }}</div>
          <div class="bar-count">{{ t('ledger.causes.entryCount', { count: row.count ?? 0 }) }}</div>
        </div>
      </div>

      <div v-if="block.abandoned.length" class="card abandoned-card">
        <div class="abandoned-head">
          <div class="abandoned-title">{{ t('ledger.causes.buyerAbandonedTitle') }}</div>
          <p class="abandoned-note">{{ t('ledger.causes.buyerAbandonedNote') }}</p>
        </div>
        <div
          v-for="row in block.abandoned"
          :key="row.cause"
          class="bar-row"
          role="button"
          tabindex="0"
          @click="row.cause && emit('select-cause', row.cause)"
          @keydown.enter="row.cause && emit('select-cause', row.cause)"
        >
          <div class="bar-label">{{ causeLabel(t, row.cause) }}</div>
          <div class="bar-track quiet">
            <div class="bar-fill quiet" :style="{ width: barWidth(row.lost_revenue_minor, maxRevenue(block.abandoned)) }" />
          </div>
          <div class="bar-value mono">{{ formatMinorUnits(row.lost_revenue_minor ?? 0, block.currency) }}</div>
          <div class="bar-count">{{ t('ledger.causes.entryCount', { count: row.count ?? 0 }) }}</div>
        </div>
      </div>
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
.breakdown-head { display: flex; align-items: center; justify-content: space-between; margin: 30px 0 14px; }
.section-title { font-size: 19px; }
.sort-toggle { display: inline-flex; gap: 2px; padding: 3px; background: var(--surface2); border-radius: 99px; }
.sort-toggle button { padding: 6px 13px; border-radius: 99px; font-size: 12.5px; font-weight: 600; color: var(--muted); }
.sort-toggle button.active { background: var(--surface); color: var(--ink); box-shadow: var(--shadow); }

.currency-block { margin-bottom: 18px; }
.currency-label {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.08em;
  color: var(--faint);
  margin-bottom: 8px;
}

.bars-card, .abandoned-card { padding: 6px 20px; margin-bottom: 12px; }
.abandoned-head { padding: 14px 0 6px; }
.abandoned-title { font-weight: 600; font-size: 13.5px; color: var(--muted); }
.abandoned-note { color: var(--faint); font-size: 12.5px; margin-top: 4px; max-width: 62ch; line-height: 1.5; }

.bar-row {
  display: grid;
  grid-template-columns: 160px 1fr 100px 90px;
  align-items: center;
  gap: 14px;
  padding: 12px 0;
  border-top: 1px solid var(--line);
  cursor: pointer;
}
.bars-card .bar-row:first-child,
.abandoned-card .bar-row:first-child { border-top: 1px solid var(--line); }
.bar-row:hover { background: var(--surface2); margin: 0 -20px; padding: 12px 20px; }
.bar-row:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; }

.bar-label { font-size: 13.5px; font-weight: 500; color: var(--ink); }
.bar-track { height: 8px; border-radius: 99px; background: var(--surface2); overflow: hidden; }
.bar-fill { height: 100%; background: var(--accent); border-radius: 99px; }
.bar-fill.quiet { background: var(--faint); }
.bar-value { text-align: right; font-size: 13px; font-weight: 600; }
.bar-count { text-align: right; font-size: 12px; color: var(--faint); }

@media (max-width: 720px) {
  .bar-row { grid-template-columns: 1fr; gap: 4px; }
  .bar-value, .bar-count { text-align: left; }
}
</style>
