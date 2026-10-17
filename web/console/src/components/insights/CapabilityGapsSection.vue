<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Capability gaps (R4-R8) — the page's headline section. Server order
// (estimated_impact_minor desc, request_count desc tiebreak) is rendered
// as-is; this component applies no sort of its own (R4/A2).
//
// R6 is the single most important correctness requirement on this page:
// confidence must be visible on every impact figure, and "unmeasurable"
// must never render as $0 — it renders as an explained "not measurable"
// instead, since $0 reads as "no money at stake", which is the opposite of
// the truth. impactIsMeasurable (lib/insights.ts) gates which of the two
// treatments a row gets; there is no third path that skips this gate.
//
// R7 splits remediation_available into two visually distinct treatments:
// a gap Sarvex can close (badge + "we can fix this" copy) vs. one that
// needs merchant-side work (plain "requires your action" copy) — always
// false in this build (internal/planes/control/insights' package doc
// comment: nothing lets Sarvex serve a UCP capability on a merchant's
// behalf yet), but the split is implemented for both states, not hardcoded
// to the always-false case.
//
// Cards, not a table (R19): this is the one section that must stay fully
// readable on a narrow viewport, and a multi-column table is the first
// thing to break there.

import { useI18n } from 'vue-i18n'
import { capabilityLabel, impactIsMeasurable } from '@/lib/insights'
import { formatMinorUnits } from '@/lib/money'
import type { CapabilityGap, ImpactConfidence } from '@/api/insights'
import Skeleton from '@/components/ui/Skeleton.vue'
import ErrorBanner from '@/components/ui/ErrorBanner.vue'
import Chip from '@/components/ui/Chip.vue'
import Icon from '@/components/ui/Icon.vue'

const props = defineProps<{
  gaps: CapabilityGap[]
  loading: boolean
  error: string | null
  /** Currency to format a measurable estimated_impact_minor in, when one is
   * known. insights_capability_gap carries no currency field of its own
   * (a real API gap — see api/insights.ts's top-of-file note); this stays
   * undefined in every real deployment today since impact_confidence is
   * always "unmeasurable" in this build, and formatCapabilityImpact below
   * degrades honestly (raw minor units, flagged) rather than guessing one. */
  currency?: string
}>()
const emit = defineEmits<{ retry: [] }>()

const i18n = useI18n()
const { t } = i18n

function label(id: string | undefined): string {
  return capabilityLabel(i18n, id ?? '')
}

function confidenceOf(gap: CapabilityGap): ImpactConfidence {
  return gap.impact_confidence ?? 'unmeasurable'
}

function formatImpact(gap: CapabilityGap): string {
  if (!props.currency) {
    return t('insights.capabilityGaps.impactNoCurrency', { amount: gap.estimated_impact_minor ?? 0 })
  }
  return formatMinorUnits(gap.estimated_impact_minor ?? 0, props.currency)
}
</script>

<template>
  <section class="section">
    <div class="section-head">
      <div>
        <h2 class="section-title display">{{ t('insights.capabilityGaps.title') }}</h2>
        <p class="section-sub">{{ t('insights.capabilityGaps.subtitle') }}</p>
      </div>
    </div>

    <ErrorBanner v-if="error" :message="error" @retry="emit('retry')" />

    <div v-if="loading" class="skel-block">
      <Skeleton height="120px" radius="var(--radius)" />
      <Skeleton height="120px" radius="var(--radius)" />
    </div>

    <template v-else-if="!error">
      <div v-if="gaps.length === 0" class="card empty-note">
        <div class="big">{{ t('insights.capabilityGaps.empty.title') }}</div>
        <p>{{ t('insights.capabilityGaps.empty.body') }}</p>
      </div>

      <template v-else>
        <p class="suppression-note">{{ t('insights.suppressionNote') }}</p>
        <div class="gap-list">
          <article
            v-for="gap in gaps"
            :key="gap.capability_id"
            class="card gap-card"
            :class="{ remediable: gap.remediation_available }"
          >
            <div class="gap-head">
              <div class="gap-title-block">
                <h3 class="gap-title">{{ label(gap.capability_id) }}</h3>
                <span class="gap-id mono">{{ gap.capability_id }}</span>
              </div>
              <Chip v-if="gap.remediation_available" kind="ok" class="remediation-chip">
                <Icon name="zap" size="11" />
                {{ t('insights.capabilityGaps.remediation.available') }}
              </Chip>
            </div>

            <div class="gap-stats">
              <div class="stat">
                <span class="stat-label">{{ t('insights.capabilityGaps.columns.requestCount') }}</span>
                <span class="stat-value mono">{{ gap.request_count }}</span>
              </div>
              <div class="stat">
                <span class="stat-label">{{ t('insights.capabilityGaps.columns.distinctAgents') }}</span>
                <span class="stat-value mono">{{ gap.distinct_agent_count }}</span>
              </div>
              <div class="stat stat-impact">
                <span class="stat-label">{{ t('insights.capabilityGaps.columns.impact') }}</span>
                <template v-if="impactIsMeasurable(confidenceOf(gap))">
                  <span class="stat-value mono impact-value">{{ formatImpact(gap) }}</span>
                  <Chip :kind="confidenceOf(gap) === 'measured' ? 'ok' : 'warn'" class="confidence-chip">
                    {{ t(`insights.capabilityGaps.confidence.${confidenceOf(gap)}`) }}
                  </Chip>
                  <p v-if="confidenceOf(gap) === 'partial'" class="confidence-note">
                    {{ t('insights.capabilityGaps.partialExplanation') }}
                  </p>
                </template>
                <template v-else>
                  <span class="stat-value impact-unmeasurable">{{ t('insights.capabilityGaps.confidence.unmeasurable') }}</span>
                  <Chip kind="quiet" class="confidence-chip">
                    {{ t('insights.capabilityGaps.confidence.unmeasurable') }}
                  </Chip>
                  <p class="confidence-note">{{ t('insights.capabilityGaps.unmeasurableExplanation') }}</p>
                </template>
              </div>
            </div>

            <div class="gap-action">
              <span class="action-label">
                {{ gap.remediation_available ? t('insights.capabilityGaps.remediation.availableAction') : t('insights.capabilityGaps.remediation.merchantAction') }}
              </span>
              <p class="action-text">{{ gap.recommended_action }}</p>
            </div>
          </article>
        </div>
      </template>
    </template>
  </section>
</template>

<style scoped>
.section { display: flex; flex-direction: column; gap: 12px; }
.section-head { display: flex; align-items: flex-end; justify-content: space-between; gap: 12px; flex-wrap: wrap; }
.section-title { font-size: 22px; }
.section-sub { color: var(--muted); font-size: 14px; margin-top: 6px; max-width: 68ch; }

.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}

.suppression-note { font-size: 12px; color: var(--faint); margin: 0; }

.skel-block { display: flex; flex-direction: column; gap: 12px; }

.empty-note { text-align: center; padding: 44px 20px; color: var(--muted); }
.empty-note .big {
  font-family: var(--font-display);
  font-weight: var(--display-weight);
  font-size: 18px;
  color: var(--ink);
  margin-bottom: 6px;
}
.empty-note p { max-width: 52ch; margin: 0 auto; font-size: 13.5px; }

.gap-list { display: flex; flex-direction: column; gap: 12px; }
.gap-card { padding: 18px 22px; display: flex; flex-direction: column; gap: 14px; }
.gap-card.remediable { box-shadow: var(--shadow), inset 0 0 0 1px color-mix(in oklab, var(--ok) 35%, transparent); }

.gap-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; flex-wrap: wrap; }
.gap-title-block { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
.gap-title { font-size: 16px; font-weight: 600; color: var(--ink); }
.gap-id { font-size: 11px; color: var(--faint); word-break: break-all; }
.remediation-chip { display: inline-flex; align-items: center; gap: 4px; flex-shrink: 0; }

.gap-stats {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 16px;
}
.stat { display: flex; flex-direction: column; gap: 4px; }
.stat-label { font-size: 11px; font-weight: 600; letter-spacing: 0.05em; text-transform: uppercase; color: var(--faint); }
.stat-value { font-size: 16px; font-weight: 600; color: var(--ink); }
.stat-impact { gap: 6px; }
.impact-value { color: var(--ink); }
.impact-unmeasurable { font-size: 13.5px; font-weight: 600; color: var(--muted); font-style: italic; }
.confidence-chip { align-self: flex-start; }
.confidence-note { font-size: 12px; color: var(--faint); line-height: 1.5; margin: 0; }

.gap-action { border-top: 1px solid var(--line); padding-top: 12px; }
.action-label { font-size: 11px; font-weight: 700; letter-spacing: 0.05em; text-transform: uppercase; color: var(--accent-deep); }
.action-text { font-size: 13.5px; color: var(--muted); margin-top: 4px; line-height: 1.5; }

@media (max-width: 520px) {
  .gap-stats { grid-template-columns: 1fr 1fr; }
}
</style>
