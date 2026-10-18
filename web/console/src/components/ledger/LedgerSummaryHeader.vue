<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// The money-first header (R3). One card per currency (R4) — never a
// combined total; a merchant with three currencies sees three cards, a
// merchant with one sees one. The unknown-revenue count is disclosed
// per-currency as a plain line, never folded into the total or hidden (R5).

import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatMinorUnits } from '@/lib/money'
import { causeLabel } from '@/lib/ledger'
import type { LedgerSummary } from '@/api/ledger'
import Skeleton from '@/components/ui/Skeleton.vue'
import ErrorBanner from '@/components/ui/ErrorBanner.vue'

const props = defineProps<{
  summaries: Record<string, LedgerSummary>
  currencies: string[]
  loading: boolean
  error: string | null
}>()
const emit = defineEmits<{ retry: [] }>()

const { t } = useI18n()

function topCause(s: LedgerSummary): string | null {
  const top = [...(s.causes ?? [])].sort(
    (a, b) => (b.lost_revenue_minor ?? 0) - (a.lost_revenue_minor ?? 0),
  )[0]
  return top?.cause ? causeLabel(t, top.cause) : null
}

function deltaKind(minor: number): 'up' | 'down' | 'flat' {
  if (minor > 0) return 'up'
  if (minor < 0) return 'down'
  return 'flat'
}

const cards = computed(() =>
  props.currencies.map((currency) => ({ currency, summary: props.summaries[currency] })),
)
</script>

<template>
  <div class="summary-head">
    <ErrorBanner v-if="error" :message="error" @retry="emit('retry')" />
    <p v-if="!loading && !error && cards.length" class="scope-note">{{ t('analytics.merchantWideNote') }}</p>
    <div v-if="loading" class="skel-row">
      <Skeleton height="168px" radius="var(--radius)" />
    </div>
    <div v-else class="cards">
      <div v-for="card in cards" :key="card.currency" class="card summary-card">
        <div class="currency-tag mono">{{ card.currency }}</div>
        <div class="total mono display">
          {{ formatMinorUnits(card.summary.total_lost_revenue_minor ?? 0, card.currency) }}
        </div>
        <div class="meta-row">
          <span>{{ t('ledger.summary.entryCount', { count: card.summary.total_count ?? 0 }) }}</span>
          <span class="dot-sep" />
          <span v-if="topCause(card.summary)">
            {{ t('ledger.summary.topCause', { cause: topCause(card.summary) }) }}
          </span>
          <span v-else>{{ t('ledger.summary.noCause') }}</span>
        </div>
        <div
          v-if="card.summary.prior_month_delta_minor"
          class="delta"
          :class="deltaKind(card.summary.prior_month_delta_minor)"
        >
          {{
            t(
              deltaKind(card.summary.prior_month_delta_minor) === 'up'
                ? 'ledger.summary.deltaUp'
                : 'ledger.summary.deltaDown',
              { amount: formatMinorUnits(Math.abs(card.summary.prior_month_delta_minor), card.currency) },
            )
          }}
        </div>
        <div v-else class="delta flat">{{ t('ledger.summary.deltaFlat') }}</div>
        <div v-if="card.summary.entries_with_unknown_revenue" class="unknown-note">
          {{ t('ledger.summary.unknownDisclosure', { count: card.summary.entries_with_unknown_revenue }) }}
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
.cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 16px; }
.summary-card { padding: 24px 26px; }
.currency-tag {
  display: inline-block;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.08em;
  color: var(--faint);
  background: var(--surface2);
  padding: 3px 8px;
  border-radius: 99px;
  margin-bottom: 10px;
}
.total { font-size: 42px; font-weight: 600; line-height: 1.1; margin-bottom: 10px; }
.meta-row {
  display: flex;
  align-items: center;
  gap: 9px;
  color: var(--muted);
  font-size: 13.5px;
  margin-bottom: 8px;
}
.delta { font-size: 13px; font-weight: 600; }
.delta.up { color: var(--danger); }
.delta.down { color: var(--ok); }
.delta.flat { color: var(--faint); font-weight: 500; }
.unknown-note {
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid var(--line);
  color: var(--faint);
  font-size: 12.5px;
}
.skel-row { padding: 4px 0; }
.scope-note { color: var(--faint); font-size: 12px; margin: 0 0 10px; }
</style>
