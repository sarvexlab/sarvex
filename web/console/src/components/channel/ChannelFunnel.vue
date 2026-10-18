<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Discovery-to-checkout funnel (R6): sessions → reached cart → reached
// checkout → completed, with rates derived from the API's raw counts at
// render time (R6 — see lib/channel.ts's stageRate doc comment for why the
// funnel doesn't reuse channel_rates.completion_rate for its last segment).
//
// R7: when any session in the period had to be reconstructed rather than
// observed (sessions_inferred > 0), a visible qualifier sits next to the
// funnel header — not a footnote, and its tooltip now cites the real
// sessions_confirmed/sessions_inferred split the API exposes directly
// rather than a generic caveat. R8: when there's no session data at all for
// the period, the funnel degrades to a short explanation instead of
// rendering an empty/zeroed set of stages.

import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ChannelFunnel } from '@/api/channel'
import { hasInferredSessions, hasNoFunnelData, stageRate } from '@/lib/channel'
import Icon from '@/components/ui/Icon.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import ErrorBanner from '@/components/ui/ErrorBanner.vue'

const props = defineProps<{
  funnel: ChannelFunnel | null
  loading: boolean
  error: string | null
}>()
const emit = defineEmits<{ retry: [] }>()

const { t } = useI18n()

const inferred = computed(() => hasInferredSessions(props.funnel?.sessions_inferred))
const noData = computed(() => hasNoFunnelData(props.funnel?.sessions))
const inferredTooltip = computed(() =>
  t('channel.funnel.inferredTooltip', {
    confirmed: props.funnel?.sessions_confirmed ?? 0,
    inferred: props.funnel?.sessions_inferred ?? 0,
  }),
)

const stages = computed(() => {
  const f = props.funnel
  if (!f) return []
  return [
    { key: 'sessions', label: t('channel.funnel.stages.sessions'), count: f.sessions ?? 0 },
    { key: 'cart', label: t('channel.funnel.stages.cart'), count: f.sessions_reached_cart ?? 0 },
    { key: 'checkout', label: t('channel.funnel.stages.checkout'), count: f.sessions_reached_checkout ?? 0 },
    { key: 'completed', label: t('channel.funnel.stages.completed'), count: f.checkouts_completed ?? 0 },
  ]
})

const rates = computed(() => {
  const f = props.funnel
  if (!f) return [0, 0, 0]
  return [
    stageRate(f.sessions_reached_cart, f.sessions),
    stageRate(f.sessions_reached_checkout, f.sessions_reached_cart),
    stageRate(f.checkouts_completed, f.sessions_reached_checkout),
  ]
})

function pct(n: number): string {
  return `${(n * 100).toFixed(1)}%`
}
</script>

<template>
  <div class="funnel card">
    <ErrorBanner v-if="error" :message="error" @retry="emit('retry')" />
    <div v-if="loading" class="skel-block">
      <Skeleton height="180px" radius="var(--radius)" />
    </div>
    <template v-else-if="funnel">
      <div class="funnel-head">
        <h2 class="funnel-title display">{{ t('channel.funnel.title') }}</h2>
        <span
          v-if="!noData && inferred"
          class="qualifier"
          :title="inferredTooltip"
        >
          <Icon name="info" size="13" />
          {{ t('channel.funnel.inferredLabel') }}
        </span>
      </div>

      <div v-if="noData" class="no-data-note">
        <Icon name="info" size="16" />
        <p>{{ t('channel.funnel.noData') }}</p>
      </div>
      <div v-else class="stages">
        <template v-for="(stage, i) in stages" :key="stage.key">
          <div class="stage">
            <div class="stage-count mono">{{ stage.count }}</div>
            <div class="stage-label">{{ stage.label }}</div>
          </div>
          <div v-if="i < stages.length - 1" class="rate-arrow">
            <Icon name="arrowR" size="14" />
            <span class="rate-value mono">{{ pct(rates[i]) }}</span>
          </div>
        </template>
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
.funnel-head { display: flex; align-items: center; gap: 12px; margin-bottom: 18px; flex-wrap: wrap; }
.funnel-title { font-size: 18px; }
.qualifier {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 11.5px;
  font-weight: 600;
  color: var(--warn);
  background: color-mix(in oklab, var(--warn) 13%, var(--surface));
  padding: 4px 9px;
  border-radius: 99px;
  cursor: help;
}

.stages { display: flex; align-items: stretch; gap: 10px; flex-wrap: wrap; }
.stage {
  flex: 1;
  min-width: 110px;
  text-align: center;
  padding: 14px 10px;
  background: var(--surface2);
  border-radius: var(--radius-sm);
}
.stage-count { font-size: 22px; font-weight: 600; margin-bottom: 4px; }
.stage-label { font-size: 12px; color: var(--muted); font-weight: 600; }
.rate-arrow {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 3px;
  color: var(--faint);
  min-width: 50px;
}
.rate-value { font-size: 11.5px; color: var(--muted); font-weight: 600; }

.no-data-note {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 20px;
  color: var(--muted);
  font-size: 13.5px;
}
.no-data-note :deep(svg) { flex-shrink: 0; color: var(--faint); }

.skel-block { padding: 0; }
</style>
