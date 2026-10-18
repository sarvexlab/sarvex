<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Web-baseline comparison (R10-R13): agentic vs. web on the metrics
// GET /channel/comparison says are comparable. Renders only what the API
// returns as available (R10) — an unavailable metric shows its server
// reason in place of the number, never a placeholder, estimate, or bare
// dash (R11). No session-count comparison exists anywhere in this
// component (R12) — the API's channel_comparison schema has no
// session-count field at all. agentic_share_of_total is rendered exactly
// as channel_share_comparison.share, never recomputed from agentic/baseline
// (R13).
//
// Confidence (R11): channel_metric_comparison now carries a tri-state
// `confidence` (connector_reported | partial | unavailable) instead of a
// strict available boolean — available=true at confidence=partial still
// carries real (not estimated) numbers, with a reason explaining what's
// incomplete. This component shows a visible confidence indicator only in
// that partial case; connector_reported renders clean, matching R7's
// "confirmed carries no qualifier" convention elsewhere on this page.
//
// Currency: agentic and web_baseline each carry their own
// currency/currency_mixed (mirrored shapes) rather than sharing one prop —
// see lib/channel.ts's resolveCurrencyState.

import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatMinorUnits } from '@/lib/money'
import { resolveCurrencyState } from '@/lib/channel'
import type { ChannelComparison } from '@/api/channel'
import Skeleton from '@/components/ui/Skeleton.vue'
import ErrorBanner from '@/components/ui/ErrorBanner.vue'

const props = defineProps<{
  comparison: ChannelComparison | null
  loading: boolean
  error: string | null
}>()
const emit = defineEmits<{ retry: [] }>()

const { t } = useI18n()

const agenticCurrency = computed(() => {
  const s = resolveCurrencyState(props.comparison?.agentic?.currency, props.comparison?.agentic?.currency_mixed)
  return s.kind === 'known' ? s.currency : undefined
})
const agenticMixed = computed(
  () => resolveCurrencyState(props.comparison?.agentic?.currency, props.comparison?.agentic?.currency_mixed).kind === 'mixed',
)
const baselineCurrency = computed(() => {
  const s = resolveCurrencyState(props.comparison?.web_baseline?.currency, props.comparison?.web_baseline?.currency_mixed)
  return s.kind === 'known' ? s.currency : undefined
})
const baselineMixed = computed(
  () =>
    resolveCurrencyState(props.comparison?.web_baseline?.currency, props.comparison?.web_baseline?.currency_mixed).kind ===
    'mixed',
)

const sharePct = computed(() => {
  const share = props.comparison?.agentic_share_of_total
  if (!share?.available || typeof share.share !== 'number') return null
  return `${(share.share * 100).toFixed(1)}%`
})
const sharePartial = computed(() => props.comparison?.agentic_share_of_total?.confidence === 'partial')
</script>

<template>
  <div class="comparison card">
    <ErrorBanner v-if="error" :message="error" @retry="emit('retry')" />
    <div v-if="loading" class="skel-block">
      <Skeleton height="200px" radius="var(--radius)" />
    </div>
    <template v-else-if="comparison">
      <h2 class="comparison-title display">{{ t('channel.comparison.title') }}</h2>
      <div class="metrics">
        <div class="metric">
          <div class="metric-label-row">
            <div class="metric-label">{{ t('channel.comparison.revenue') }}</div>
            <span v-if="comparison.revenue?.confidence === 'partial'" class="confidence-badge" :title="comparison.revenue?.reason">
              {{ t('channel.comparison.partial') }}
            </span>
          </div>
          <template v-if="comparison.revenue?.available">
            <div class="metric-row">
              <span class="metric-tag agentic">{{ t('channel.comparison.agentic') }}</span>
              <span v-if="agenticMixed" class="mono mixed-note">{{ t('channel.kpi.mixedCurrency') }}</span>
              <span v-else class="mono">{{ formatMinorUnits(comparison.revenue.agentic_minor ?? 0, agenticCurrency) }}</span>
            </div>
            <div class="metric-row">
              <span class="metric-tag web">{{ t('channel.comparison.web') }}</span>
              <span v-if="baselineMixed" class="mono mixed-note">{{ t('channel.kpi.mixedCurrency') }}</span>
              <span v-else class="mono">{{ formatMinorUnits(comparison.revenue.baseline_minor ?? 0, baselineCurrency) }}</span>
            </div>
            <p v-if="comparison.revenue?.confidence === 'partial'" class="partial-reason">{{ comparison.revenue.reason }}</p>
          </template>
          <div v-else class="unavailable">
            <span class="unavailable-tag">{{ t('channel.comparison.unavailable') }}</span>
            <p>{{ comparison.revenue?.reason }}</p>
          </div>
        </div>

        <div class="metric">
          <div class="metric-label-row">
            <div class="metric-label">{{ t('channel.comparison.aov') }}</div>
            <span v-if="comparison.aov?.confidence === 'partial'" class="confidence-badge" :title="comparison.aov?.reason">
              {{ t('channel.comparison.partial') }}
            </span>
          </div>
          <template v-if="comparison.aov?.available">
            <div class="metric-row">
              <span class="metric-tag agentic">{{ t('channel.comparison.agentic') }}</span>
              <span v-if="agenticMixed" class="mono mixed-note">{{ t('channel.kpi.mixedCurrency') }}</span>
              <span v-else class="mono">{{ formatMinorUnits(comparison.aov.agentic_minor ?? 0, agenticCurrency) }}</span>
            </div>
            <div class="metric-row">
              <span class="metric-tag web">{{ t('channel.comparison.web') }}</span>
              <span v-if="baselineMixed" class="mono mixed-note">{{ t('channel.kpi.mixedCurrency') }}</span>
              <span v-else class="mono">{{ formatMinorUnits(comparison.aov.baseline_minor ?? 0, baselineCurrency) }}</span>
            </div>
            <p v-if="comparison.aov?.confidence === 'partial'" class="partial-reason">{{ comparison.aov.reason }}</p>
          </template>
          <div v-else class="unavailable">
            <span class="unavailable-tag">{{ t('channel.comparison.unavailable') }}</span>
            <p>{{ comparison.aov?.reason }}</p>
          </div>
        </div>

        <div class="metric">
          <div class="metric-label-row">
            <div class="metric-label">{{ t('channel.comparison.shareOfTotal') }}</div>
            <span v-if="sharePartial" class="confidence-badge" :title="comparison.agentic_share_of_total?.reason">
              {{ t('channel.comparison.partial') }}
            </span>
          </div>
          <template v-if="sharePct">
            <div class="metric-row">
              <span class="mono share-value">{{ sharePct }}</span>
            </div>
            <p v-if="sharePartial" class="partial-reason">{{ comparison.agentic_share_of_total?.reason }}</p>
          </template>
          <div v-else class="unavailable">
            <span class="unavailable-tag">{{ t('channel.comparison.unavailable') }}</span>
            <p>{{ comparison.agentic_share_of_total?.reason }}</p>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
  padding: 22px 24px;
}
.comparison-title { font-size: 18px; margin-bottom: 18px; }
.metrics { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 18px; }
.metric { padding: 16px 18px; background: var(--surface2); border-radius: var(--radius-sm); }
.metric-label-row { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-bottom: 10px; }
.metric-label { font-size: 12.5px; font-weight: 600; color: var(--muted); }
.metric-row { display: flex; align-items: center; justify-content: space-between; gap: 10px; padding: 4px 0; font-size: 15px; }
.metric-tag { font-size: 11px; font-weight: 700; letter-spacing: 0.05em; text-transform: uppercase; color: var(--faint); }
.metric-tag.agentic { color: var(--accent-deep); }
.share-value { font-size: 22px; font-weight: 600; }
.mixed-note { font-size: 12.5px; font-weight: 600; color: var(--warn); }

.confidence-badge {
  display: inline-block;
  font-size: 10.5px;
  font-weight: 700;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: var(--warn);
  background: color-mix(in oklab, var(--warn) 13%, var(--surface));
  padding: 3px 8px;
  border-radius: 99px;
  cursor: help;
  white-space: nowrap;
}
.partial-reason { font-size: 12px; color: var(--muted); line-height: 1.5; margin: 8px 0 0; }

.unavailable { color: var(--muted); }
.unavailable-tag {
  display: inline-block;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: var(--faint);
  background: var(--surface);
  padding: 3px 8px;
  border-radius: 99px;
  margin-bottom: 8px;
}
.unavailable p { font-size: 13px; line-height: 1.5; margin: 0; }

.skel-block { padding: 0; }
</style>
