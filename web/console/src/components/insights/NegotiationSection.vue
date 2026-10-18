<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Negotiation (R9-R11). Failure reasons render from the API's own enum via
// negotiationFailureReasonLabel (lib/insights.ts), which falls back to a
// humanized form of any reason value this frontend doesn't have a label
// for yet — a fixture reason unknown at build time still renders (A4)
// rather than being dropped or showing a raw key.
//
// Payment-handler mismatch (R10) gets its own card, not a row folded into
// the failures list — it's computed observationally by the backend from
// every negotiation attempt (succeeded or failed) whose offered
// payment-handler set didn't overlap the merchant's declared set, so it
// isn't a "failure reason" at all in the API's own model
// (insights_payment_handler_mismatch is a sibling field on insights_negotiation,
// not an entry in `failures`).
//
// R11 (link failures to the Ledger by cause): sarvex-v1-control-plane.yaml's
// own package doc comment says negotiation failures (profile-negotiation
// time) and the Ledger's causes (checkout-submission time) are disjoint
// pipelines with no shared identifier — verified, no mapping exists in the
// API response. Rendered without a link, per the task's own fallback
// instruction for this exact case.

import { useI18n } from 'vue-i18n'
import { negotiationFailureReasonLabel } from '@/lib/insights'
import type { NegotiationInsights } from '@/api/insights'
import Skeleton from '@/components/ui/Skeleton.vue'
import ErrorBanner from '@/components/ui/ErrorBanner.vue'

defineProps<{
  negotiation: NegotiationInsights | null
  loading: boolean
  error: string | null
}>()
const emit = defineEmits<{ retry: [] }>()

const i18n = useI18n()
const { t } = i18n

function reasonLabel(reason: string): string {
  return negotiationFailureReasonLabel(i18n, reason)
}
</script>

<template>
  <section class="section">
    <div class="section-head">
      <h2 class="section-title display">{{ t('insights.negotiation.title') }}</h2>
      <p class="section-sub">{{ t('insights.negotiation.subtitle') }}</p>
      <p class="scope-note">{{ t('analytics.merchantWideNote') }}</p>
    </div>

    <ErrorBanner v-if="error" :message="error" @retry="emit('retry')" />

    <div v-if="loading" class="skel-block">
      <Skeleton height="160px" radius="var(--radius)" />
    </div>

    <template v-else-if="!error && negotiation">
      <div v-if="negotiation.attempts === 0" class="card empty-note">
        {{ t('insights.negotiation.empty') }}
      </div>
      <template v-else>
        <div class="card kpi-row">
          <div class="kpi">
            <span class="kpi-label">{{ t('insights.negotiation.attempts') }}</span>
            <span class="kpi-value mono">{{ negotiation.attempts }}</span>
          </div>
          <div class="kpi">
            <span class="kpi-label">{{ t('insights.negotiation.successes') }}</span>
            <span class="kpi-value mono">{{ negotiation.successes }}</span>
          </div>
        </div>

        <div v-if="negotiation.failures?.length" class="card">
          <div class="card-inner">
            <h3 class="card-title">{{ t('insights.negotiation.failuresTitle') }}</h3>
            <p class="suppression-note">{{ t('insights.suppressionNote') }}</p>
            <div class="failure-list">
              <div v-for="f in negotiation.failures" :key="f.reason" class="failure-row">
                <div class="failure-head">
                  <span class="failure-reason">{{ reasonLabel(f.reason ?? '') }}</span>
                  <span class="failure-count mono">{{ f.count }}</span>
                </div>
                <div v-if="f.missing_capability_counts && Object.keys(f.missing_capability_counts).length" class="missing-caps">
                  <span class="missing-caps-label">{{ t('insights.negotiation.missingCapabilities') }}</span>
                  <span
                    v-for="(count, cap) in f.missing_capability_counts"
                    :key="cap"
                    class="missing-cap mono"
                  >{{ cap }}: {{ count }}</span>
                </div>
              </div>
            </div>
          </div>
        </div>

        <div v-if="negotiation.payment_handler_mismatch" class="card handler-mismatch">
          <div class="card-inner">
            <h3 class="card-title">{{ t('insights.negotiation.handlerMismatch.title') }}</h3>
            <p class="section-sub">{{ t('insights.negotiation.handlerMismatch.subtitle') }}</p>
            <div class="mismatch-stat">
              <span class="kpi-label">{{ t('insights.negotiation.handlerMismatch.occurrences') }}</span>
              <span class="kpi-value mono">{{ negotiation.payment_handler_mismatch.occurrences }}</span>
            </div>
            <div class="handler-cols">
              <div class="handler-col">
                <span class="handler-col-label">{{ t('insights.negotiation.handlerMismatch.offered') }}</span>
                <ul class="handler-list">
                  <li
                    v-for="(count, handler) in negotiation.payment_handler_mismatch.offered_handler_counts"
                    :key="handler"
                  >
                    <span class="mono">{{ handler }}</span>
                    <span class="mono handler-count">{{ count }}</span>
                  </li>
                </ul>
              </div>
              <div class="handler-col">
                <span class="handler-col-label">{{ t('insights.negotiation.handlerMismatch.declared') }}</span>
                <ul class="handler-list">
                  <li v-for="handler in negotiation.payment_handler_mismatch.merchant_declared_handlers" :key="handler">
                    <span class="mono">{{ handler }}</span>
                  </li>
                </ul>
              </div>
            </div>
          </div>
        </div>
      </template>
    </template>
  </section>
</template>

<style scoped>
.section { display: flex; flex-direction: column; gap: 12px; }
.section-title { font-size: 22px; }
.section-sub { color: var(--muted); font-size: 14px; margin-top: 6px; max-width: 68ch; }
.scope-note { color: var(--faint); font-size: 12px; margin-top: 4px; }

.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}
.card-inner { padding: 18px 22px; }
.card-title { font-size: 15px; font-weight: 600; color: var(--ink); margin-bottom: 4px; }
.suppression-note { font-size: 12px; color: var(--faint); margin: 0 0 12px; }

.skel-block { display: flex; flex-direction: column; gap: 12px; }
.empty-note { text-align: center; padding: 32px 20px; color: var(--muted); }

.kpi-row { display: flex; gap: 32px; padding: 18px 22px; }
.kpi { display: flex; flex-direction: column; gap: 4px; }
.kpi-label { font-size: 11px; font-weight: 600; letter-spacing: 0.05em; text-transform: uppercase; color: var(--faint); }
.kpi-value { font-size: 24px; font-weight: 600; color: var(--ink); }

.failure-list { display: flex; flex-direction: column; gap: 10px; }
.failure-row { border-top: 1px solid var(--line); padding-top: 10px; }
.failure-row:first-child { border-top: none; padding-top: 0; }
.failure-head { display: flex; align-items: center; justify-content: space-between; }
.failure-reason { font-size: 14px; font-weight: 600; color: var(--ink); }
.failure-count { font-size: 14px; color: var(--muted); }
.missing-caps { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 6px; align-items: center; }
.missing-caps-label { font-size: 11px; color: var(--faint); }
.missing-cap { font-size: 11px; background: var(--surface2); color: var(--muted); padding: 2px 8px; border-radius: 99px; }

.handler-mismatch { box-shadow: var(--shadow), inset 0 0 0 1px color-mix(in oklab, var(--warn) 30%, transparent); }
.mismatch-stat { display: flex; flex-direction: column; gap: 4px; margin: 14px 0; }
.handler-cols { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 20px; }
.handler-col-label { font-size: 11px; font-weight: 600; letter-spacing: 0.05em; text-transform: uppercase; color: var(--faint); }
.handler-list { list-style: none; margin: 8px 0 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
.handler-list li { display: flex; align-items: center; justify-content: space-between; font-size: 13px; color: var(--ink); }
.handler-count { color: var(--muted); }
</style>
