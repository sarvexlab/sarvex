<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Checkout Escalation Console — session detail (R6-R9). The diagnosis
// block, when present, renders above the timeline: "the answer comes
// before the evidence." Read-only reconstruction throughout — there is no
// re-run/retry/re-execute action anywhere on this page (R10).

import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useCheckoutSessionsStore } from '@/stores/checkoutSessions'
import { useStoresStore } from '@/stores/stores'
import { formatDuration, formatRelativeTime, formatUTCTimestamp } from '@/lib/datetime'
import { formatMinorUnits } from '@/lib/money'
import { checkoutStateChipKind, causeLabel } from '@/lib/checkoutSession'
import Chip from '@/components/ui/Chip.vue'
import Icon from '@/components/ui/Icon.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import ErrorBanner from '@/components/ui/ErrorBanner.vue'
import CheckoutTimeline from '@/components/checkout/CheckoutTimeline.vue'

const props = defineProps<{ checkoutId: string }>()

const router = useRouter()
const { t } = useI18n()
const sessions = useCheckoutSessionsStore()
const stores = useStoresStore()

const session = computed(() => sessions.detail)
const store = computed(() => stores.stores.find((s) => s.store_id === session.value?.store_id))

const evidenceSeqs = computed(() => new Set(session.value?.diagnosis?.evidence ?? []))

async function load() {
  sessions.clearDetail()
  await Promise.all([sessions.loadDetail(props.checkoutId), sessions.loadTimeline(props.checkoutId)])
}
onMounted(() => {
  load()
  stores.load()
})
watch(() => props.checkoutId, load)
onBeforeUnmount(() => sessions.clearDetail())

// Elapsed time ticks live while the session is non-terminal.
const nowTick = ref(Date.now())
let tickHandle: ReturnType<typeof setInterval> | null = null
onMounted(() => {
  tickHandle = setInterval(() => (nowTick.value = Date.now()), 1000)
})
onBeforeUnmount(() => {
  if (tickHandle !== null) clearInterval(tickHandle)
})
const elapsed = computed(() => {
  void nowTick.value
  if (!session.value?.started_at) return '—'
  return formatDuration(session.value.started_at, session.value.terminal_at ?? undefined)
})

// The API serialises an unset time as Go's zero value (0001-01-01T00:00:00Z),
// not null, so "was it escalated?" has to treat both as absent.
function isUnsetTime(iso: string | null | undefined): boolean {
  return !iso || new Date(iso).getUTCFullYear() <= 1
}
const wasEscalated = computed(() => !isUnsetTime(session.value?.escalation?.escalated_at))

const escalationElapsed = computed(() => {
  void nowTick.value
  const esc = session.value?.escalation
  if (!esc?.escalated_at || !wasEscalated.value) return '—'
  return formatDuration(esc.escalated_at, esc.returned_at ?? undefined)
})
const isStalled = computed(() => session.value?.diagnosis?.cause === 'escalation_stalled')

// R8: jump-to for a diagnosis evidence seq. If the entry isn't in the
// currently loaded timeline pages, keep paging until it is (or the
// timeline is exhausted) before scrolling to it.
const jumpPending = ref<number | null>(null)
async function jumpToSeq(seq: number) {
  jumpPending.value = seq
  let guard = 0
  while (
    !sessions.timeline.some((e) => e.seq === seq) &&
    sessions.timelineHasMore &&
    guard < 50
  ) {
    // eslint-disable-next-line no-await-in-loop
    await sessions.loadMoreTimeline(props.checkoutId)
    guard += 1
  }
  jumpPending.value = null
  await nextTick()
  const el = document.getElementById(`timeline-entry-${seq}`)
  el?.scrollIntoView({ behavior: 'smooth', block: 'center' })
  el?.classList.add('jump-flash')
  window.setTimeout(() => el?.classList.remove('jump-flash'), 1600)
}

const showSkeleton = computed(() => !sessions.detailLoaded && sessions.detailLoading)
</script>

<template>
  <div class="page">
    <button type="button" class="back-link" @click="router.push({ path: '/analytics', query: { tab: 'checkouts' } })">
      <span class="back-arrow"><Icon name="arrowR" /></span> {{ t('checkout.detail.backLink') }}
    </button>

    <ErrorBanner v-if="sessions.detailError" :message="sessions.detailError" @retry="load" />

    <div v-if="showSkeleton" class="skel-block">
      <Skeleton height="140px" radius="11px" />
      <Skeleton height="220px" radius="11px" />
    </div>

    <template v-else-if="session">
      <header class="head card">
        <div class="head-top">
          <Chip :kind="checkoutStateChipKind(session.state)">{{ session.state ?? t('checkout.table.unknown') }}</Chip>
          <span class="eyebrow">{{ t('checkout.detail.eyebrow') }} <span class="mono">{{ session.checkout_id }}</span></span>
        </div>
        <h1 class="display title">
          {{ store?.name ?? session.store_id }}
          <span class="stage-word">— {{ session.current_stage ?? t('checkout.detail.inProgress') }}</span>
        </h1>
        <div class="meta-grid">
          <div class="meta-item">
            <div class="meta-label">{{ t('checkout.detail.surface') }}</div>
            <div class="meta-val">{{ session.surface ?? '—' }}</div>
          </div>
          <div class="meta-item">
            <div class="meta-label">{{ t('checkout.detail.agentPlatform') }}</div>
            <div class="meta-val">{{ session.agent_platform ?? t('checkout.detail.unknownPlatform') }}</div>
          </div>
          <div class="meta-item">
            <div class="meta-label">{{ t('checkout.detail.cart') }}</div>
            <div class="meta-val mono">
              {{ formatMinorUnits(session.cart_summary?.total_minor ?? 0, session.cart_summary?.currency) }}
              <span class="faint">· {{ session.cart_summary?.item_count ?? 0 }} {{ t('checkout.detail.items') }}</span>
            </div>
          </div>
          <div class="meta-item">
            <div class="meta-label">{{ t('checkout.detail.elapsed') }}</div>
            <div class="meta-val mono">{{ elapsed }}</div>
          </div>
          <div class="meta-item">
            <div class="meta-label">{{ t('checkout.detail.started') }}</div>
            <div class="meta-val" :title="formatUTCTimestamp(session.started_at)">
              {{ formatRelativeTime(session.started_at) }}
            </div>
          </div>
          <div class="meta-item">
            <div class="meta-label">{{ t('checkout.detail.lastActivity') }}</div>
            <div class="meta-val" :title="formatUTCTimestamp(session.last_activity_at)">
              {{ formatRelativeTime(session.last_activity_at) }}
            </div>
          </div>
        </div>
      </header>

      <!-- Diagnosis: above the timeline — the answer comes before the evidence (R6). -->
      <section v-if="session.diagnosis" class="card diagnosis" :class="{ stalled: isStalled }">
        <div class="diag-head">
          <Icon name="info" />
          <h2 class="display">{{ t('checkout.detail.whatHappened') }}</h2>
          <Chip :kind="isStalled ? 'warn' : 'danger'">{{ causeLabel(t, session.diagnosis.cause) }}</Chip>
        </div>
        <p class="diag-summary">{{ session.diagnosis.plain_language_summary }}</p>
        <div v-if="session.diagnosis.recommended_action" class="diag-action">
          <span class="diag-action-label">{{ t('checkout.detail.recommendedAction') }}</span>
          {{ session.diagnosis.recommended_action }}
        </div>
        <div v-if="session.diagnosis.evidence && session.diagnosis.evidence.length > 0" class="evidence-row">
          <span class="evidence-label">{{ t('checkout.detail.evidenceInTimeline') }}</span>
          <button
            v-for="seq in session.diagnosis.evidence"
            :key="seq"
            type="button"
            class="evidence-chip"
            :disabled="jumpPending === seq"
            @click="jumpToSeq(seq)"
          >
            {{ t('checkout.detail.seq', { seq }) }} <Icon name="arrowR" />
          </button>
        </div>
      </section>

      <!-- Escalation (R9) -->
      <section v-if="session.escalation" class="card escalation" :class="{ stalled: isStalled }">
        <div class="diag-head">
          <Icon name="flag" />
          <h2 class="display">{{ t('checkout.detail.escalation') }}</h2>
          <Chip v-if="session.escalation.resolved" kind="ok">{{ t('checkout.detail.resolved') }}</Chip>
          <Chip v-else-if="isStalled" kind="warn">{{ t('checkout.detail.stalled') }}</Chip>
          <Chip v-else kind="quiet">{{ t('checkout.detail.open') }}</Chip>
        </div>
        <p v-if="session.escalation.reason" class="diag-summary">{{ session.escalation.reason }}</p>
        <div class="meta-grid">
          <div class="meta-item">
            <div class="meta-label">{{ t('checkout.detail.escalated') }}</div>
            <div v-if="wasEscalated" class="meta-val" :title="formatUTCTimestamp(session.escalation.escalated_at)">
              {{ formatRelativeTime(session.escalation.escalated_at) }}
            </div>
            <div v-else class="meta-val">{{ t('checkout.detail.no') }}</div>
          </div>
          <div class="meta-item">
            <div class="meta-label">{{ t('checkout.detail.continueUrlIssued') }}</div>
            <div class="meta-val">{{ session.escalation.continue_url_issued ? t('checkout.detail.yes') : t('checkout.detail.no') }}</div>
          </div>
          <div class="meta-item">
            <div class="meta-label">{{ t('checkout.detail.returned') }}</div>
            <div v-if="!isUnsetTime(session.escalation.returned_at)" class="meta-val" :title="formatUTCTimestamp(session.escalation.returned_at)">
              {{ formatRelativeTime(session.escalation.returned_at) }}
            </div>
            <div v-else class="meta-val warn-text">
              {{ t('checkout.detail.neverReturned') }}
              <span v-if="wasEscalated" class="faint">· {{ t('checkout.detail.elapsedSuffix', { when: escalationElapsed }) }}</span>
            </div>
          </div>
        </div>
      </section>

      <section class="timeline-section">
        <h2 class="section-title display">{{ t('checkout.detail.timeline') }}</h2>
        <ErrorBanner
          v-if="sessions.timelineError"
          :message="sessions.timelineError"
          @retry="sessions.loadTimeline(props.checkoutId)"
        />
        <div v-if="!sessions.timelineLoaded && sessions.timelineLoading" class="skel-block">
          <Skeleton height="160px" radius="11px" />
        </div>
        <div v-else-if="sessions.timeline.length === 0" class="card empty-note">
          {{ t('checkout.detail.noTimelineEvents') }}
        </div>
        <div v-else class="card timeline-card">
          <CheckoutTimeline :entries="sessions.timeline" :highlighted-seqs="evidenceSeqs" />
        </div>
        <div v-if="sessions.timelineHasMore" class="load-more">
          <button
            type="button"
            class="load-more-btn"
            :disabled="sessions.timelineLoadingMore"
            @click="sessions.loadMoreTimeline(props.checkoutId)"
          >
            {{ sessions.timelineLoadingMore ? t('checkout.detail.loading') : t('checkout.detail.loadMoreEvents') }}
          </button>
        </div>
      </section>
    </template>
  </div>
</template>

<style scoped>
.page { max-width: 900px; margin: 0 auto; padding: 34px 26px 130px; }

.back-link {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--muted);
  font-size: 13px;
  font-weight: 600;
  margin-bottom: 20px;
}
.back-link:hover { color: var(--ink); }
.back-arrow { display: inline-flex; transform: rotate(180deg); }
.back-link :deep(svg) { width: 13px; height: 13px; }

.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}

.head { padding: 24px 26px; margin-bottom: 18px; }
.head-top { display: flex; align-items: center; gap: 12px; margin-bottom: 10px; }
.title { font-size: 26px; }
.stage-word { color: var(--muted); font-weight: 400; font-size: 0.7em; }

.meta-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 14px;
  margin-top: 18px;
}
.meta-item { min-width: 0; }
.meta-label {
  font-size: 11px;
  color: var(--faint);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  margin-bottom: 4px;
}
.meta-val { font-size: 14px; font-weight: 500; }
.meta-val .faint { color: var(--faint); font-weight: 400; }
.warn-text { color: var(--warn); }

.diagnosis, .escalation { padding: 20px 24px; margin-bottom: 18px; }
.diagnosis.stalled, .escalation.stalled {
  border-color: color-mix(in oklab, var(--warn) 40%, var(--line));
  background: color-mix(in oklab, var(--warn) 5%, var(--surface));
}
.diag-head { display: flex; align-items: center; gap: 10px; margin-bottom: 10px; }
.diag-head :deep(svg) { width: 18px; height: 18px; color: var(--faint); }
.diag-head h2 { font-size: 17px; flex: 1; }
.diag-summary { font-size: 15px; line-height: 1.5; margin-bottom: 10px; }
.diag-action {
  font-size: 13.5px;
  background: var(--surface2);
  border-radius: var(--radius-sm);
  padding: 10px 14px;
  margin-bottom: 6px;
}
.diag-action-label {
  display: block;
  font-size: 11px;
  color: var(--faint);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  margin-bottom: 3px;
}

.evidence-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin-top: 10px; }
.evidence-label { font-size: 12.5px; color: var(--muted); }
.evidence-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-family: var(--font-mono);
  font-size: 12px;
  font-weight: 600;
  color: var(--accent-deep);
  background: var(--accent-soft);
  border-radius: 99px;
  padding: 4px 10px;
}
.evidence-chip:hover:not(:disabled) { background: color-mix(in oklab, var(--accent) 20%, var(--surface)); }
.evidence-chip:disabled { opacity: 0.6; }
.evidence-chip :deep(svg) { width: 10px; height: 10px; }

.section-title { font-size: 19px; margin: 30px 0 14px; }
.timeline-card { padding: 22px 24px; }
.empty-note { text-align: center; padding: 30px 20px; color: var(--muted); font-size: 13.5px; }

.skel-block { display: flex; flex-direction: column; gap: 14px; }

.load-more { display: flex; justify-content: center; margin-top: 16px; }
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
