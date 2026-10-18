<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Checkout Escalation Console — session list. Two-mode view (R1): Live
// polls GET /checkout_sessions/live (~15s, paused while the tab is hidden),
// History reads GET /checkout_sessions with server-side filters + cursor
// pagination.
//
// GET /checkout_sessions/live only accepts window_minutes + limit — none of
// the list filters (state/surface/store_id/cause/escalated_only) that
// GET /checkout_sessions supports. Since Live's result set is already
// small and capped, those filters are applied to it client-side via
// matchesCheckoutSessionFilter (see lib/checkoutSession.ts) so the two
// modes behave consistently despite the API asymmetry. This is also how
// Live gets store scoping below, since the endpoint itself has no store_id
// param to send.
//
// Store scoping: like every other analytics tab, this panel is scoped to
// stores.currentStoreId (the header StoreSwitcher's selection) rather than
// exposing its own store filter — a merchant switching stores elsewhere in
// the console expects Checkouts to follow.
//
// Filter state (including mode) round-trips through the URL query so a
// support person can share a filtered link (R4).

import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useCheckoutSessionsStore } from '@/stores/checkoutSessions'
import { useStoresStore } from '@/stores/stores'
import {
  CHECKOUT_CAUSES,
  CHECKOUT_SESSION_STATES,
  causeLabel,
  matchesCheckoutSessionFilter,
} from '@/lib/checkoutSession'
import type { ListCheckoutSessionsFilter } from '@/api/checkoutSessions'
import CheckoutSessionsTable from '@/components/checkout/CheckoutSessionsTable.vue'
import Switch from '@/components/ui/Switch.vue'
import Field from '@/components/ui/Field.vue'
import Icon from '@/components/ui/Icon.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import ErrorBanner from '@/components/ui/ErrorBanner.vue'

type Mode = 'live' | 'history'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const sessions = useCheckoutSessionsStore()
const stores = useStoresStore()

const mode = computed<Mode>(() => (route.query.mode === 'history' ? 'history' : 'live'))
const storeId = computed(() => stores.currentStoreId ?? undefined)

const filters = computed<ListCheckoutSessionsFilter>(() => {
  const q = route.query
  const f: ListCheckoutSessionsFilter = {}
  if (typeof q.state === 'string' && q.state) f.state = q.state as ListCheckoutSessionsFilter['state']
  if (typeof q.surface === 'string' && q.surface) f.surface = q.surface
  if (storeId.value) f.store_id = storeId.value
  if (typeof q.cause === 'string' && q.cause) f.cause = q.cause
  if (q.escalated_only === '1') f.escalated_only = true
  if (mode.value === 'history') {
    if (typeof q.from === 'string' && q.from) f.from = q.from
    if (typeof q.to === 'string' && q.to) f.to = q.to
  }
  return f
})

function setQuery(patch: Record<string, string | undefined>) {
  const next: Record<string, string> = {}
  for (const [k, v] of Object.entries({ ...route.query, ...patch })) {
    if (typeof v === 'string' && v !== '') next[k] = v
  }
  router.replace({ query: next })
}

function setMode(next: Mode) {
  setQuery({ mode: next === 'live' ? undefined : next })
}

function clearFilters() {
  setQuery({ state: undefined, surface: undefined, cause: undefined, escalated_only: undefined, from: undefined, to: undefined })
}

function onStalledBadgeClick() {
  setQuery({
    mode: 'history',
    cause: 'escalation_stalled',
    state: undefined,
    surface: undefined,
    escalated_only: undefined,
    from: undefined,
    to: undefined,
  })
}

// ── Filtered Live view (client-side — see module doc) ──────────────────
const filteredLive = computed(() =>
  sessions.live.filter((s) => matchesCheckoutSessionFilter(s, filters.value)),
)

// ── Live poll: on while mode === 'live', paused while the tab is hidden ──
function isTabVisible() {
  return typeof document === 'undefined' || document.visibilityState === 'visible'
}

function syncLivePolling() {
  if (mode.value === 'live' && isTabVisible()) {
    sessions.loadLive()
    sessions.startPolling()
  } else {
    sessions.stopPolling()
  }
}

function onVisibilityChange() {
  syncLivePolling()
}

onMounted(() => {
  document.addEventListener('visibilitychange', onVisibilityChange)
  sessions.loadStalledCount()
  sessions.startStalledPolling()
  syncLivePolling()
  if (mode.value === 'history') sessions.loadHistory(filters.value)
  stores.load()
})
onBeforeUnmount(() => {
  document.removeEventListener('visibilitychange', onVisibilityChange)
  sessions.stopPolling()
  sessions.stopStalledPolling()
})

watch(mode, (m) => {
  if (m === 'live') syncLivePolling()
  else {
    sessions.stopPolling()
    sessions.loadHistory(filters.value)
  }
})
watch(filters, () => {
  if (mode.value === 'history') sessions.loadHistory(filters.value)
})
// The stalled-escalation badge poll (loadStalledCount) is intentionally
// left out here — it isn't filtered by store server-side or client-side
// (see stores/checkoutSessions.ts), matching R5's "visible without a
// filter" requirement across all stores on the account.
watch(storeId, () => {
  if (mode.value === 'history') sessions.loadHistory(filters.value)
})

// ── "updated Xs ago" ticker ──────────────────────────────────────────────
const nowTick = ref(Date.now())
let tickHandle: ReturnType<typeof setInterval> | null = null
onMounted(() => {
  tickHandle = setInterval(() => (nowTick.value = Date.now()), 1000)
})
onBeforeUnmount(() => {
  if (tickHandle !== null) clearInterval(tickHandle)
})
const updatedSecondsAgo = computed(() => {
  if (!sessions.lastPolledAt) return null
  return Math.max(0, Math.round((nowTick.value - new Date(sessions.lastPolledAt).getTime()) / 1000))
})

const showLiveSkeleton = computed(() => mode.value === 'live' && !sessions.liveLoaded && sessions.liveLoading)
const showHistorySkeleton = computed(
  () => mode.value === 'history' && !sessions.historyLoaded && sessions.historyLoading,
)
</script>

<template>
  <div class="panel">
    <div class="controls-row">
      <div class="segmented" role="tablist" :aria-label="t('checkout.list.viewModeLabel')">
        <button
          type="button"
          role="tab"
          :aria-selected="mode === 'live'"
          :class="{ active: mode === 'live' }"
          @click="setMode('live')"
        >
          {{ t('checkout.list.live') }}
        </button>
        <button
          type="button"
          role="tab"
          :aria-selected="mode === 'history'"
          :class="{ active: mode === 'history' }"
          @click="setMode('history')"
        >
          {{ t('checkout.list.history') }}
        </button>
        <button
          v-if="sessions.stalledCount > 0"
          type="button"
          class="stalled-badge"
          :title="t('checkout.list.stalledTitle', { count: sessions.stalledBadge })"
          @click="onStalledBadgeClick"
        >
          <Icon name="flag" /> {{ t('checkout.list.stalledBadge', { count: sessions.stalledBadge }) }}
        </button>
      </div>

      <div v-if="mode === 'live'" class="poll-indicator">
        <span class="pulse-dot" :class="{ live: sessions.polling }" />
        <span v-if="updatedSecondsAgo !== null">{{ t('checkout.list.updatedSecondsAgo', { seconds: updatedSecondsAgo }) }}</span>
        <span v-else>{{ t('checkout.list.updating') }}</span>
        <button type="button" class="refresh-btn" :disabled="sessions.liveLoading" @click="sessions.loadLive()">
          <Icon name="swap" /> {{ t('checkout.list.refresh') }}
        </button>
      </div>
    </div>

    <div class="filters card">
      <Field :label="t('checkout.list.filters.state')">
        <select :value="filters.state ?? ''" @change="setQuery({ state: ($event.target as HTMLSelectElement).value })">
          <option value="">{{ t('checkout.list.filters.allStates') }}</option>
          <option v-for="s in CHECKOUT_SESSION_STATES" :key="s" :value="s">{{ s }}</option>
        </select>
      </Field>
      <Field :label="t('checkout.list.filters.cause')">
        <select :value="filters.cause ?? ''" @change="setQuery({ cause: ($event.target as HTMLSelectElement).value })">
          <option value="">{{ t('checkout.list.filters.allCauses') }}</option>
          <option v-for="c in CHECKOUT_CAUSES" :key="c" :value="c">{{ causeLabel(t, c) }}</option>
        </select>
      </Field>
      <template v-if="mode === 'history'">
        <Field :label="t('checkout.list.filters.from')">
          <input type="date" :value="filters.from?.slice(0, 10) ?? ''" @change="setQuery({ from: ($event.target as HTMLInputElement).value ? new Date(($event.target as HTMLInputElement).value).toISOString() : undefined })" />
        </Field>
        <Field :label="t('checkout.list.filters.to')">
          <input type="date" :value="filters.to?.slice(0, 10) ?? ''" @change="setQuery({ to: ($event.target as HTMLInputElement).value ? new Date(($event.target as HTMLInputElement).value).toISOString() : undefined })" />
        </Field>
      </template>
      <label class="escalated-toggle">
        <Switch
          :model-value="!!filters.escalated_only"
          :label="t('checkout.list.filters.escalatedOnly')"
          @update:model-value="(v) => setQuery({ escalated_only: v ? '1' : undefined })"
        />
        {{ t('checkout.list.filters.escalatedOnly') }}
      </label>
      <button type="button" class="clear-btn" @click="clearFilters">{{ t('checkout.list.filters.clearFilters') }}</button>
    </div>

    <ErrorBanner
      v-if="mode === 'live' && sessions.liveError"
      :message="sessions.liveError"
      @retry="sessions.loadLive()"
    />
    <ErrorBanner
      v-if="mode === 'history' && sessions.historyError"
      :message="sessions.historyError"
      @retry="sessions.loadHistory(filters)"
    />

    <template v-if="mode === 'live'">
      <div v-if="showLiveSkeleton" class="skel-block">
        <Skeleton height="240px" radius="11px" />
      </div>
      <div v-else-if="filteredLive.length === 0" class="card empty-note">
        <div class="big">{{ t('checkout.list.liveEmpty.title') }}</div>
        <p>{{ t('checkout.list.liveEmpty.body') }}</p>
        <div class="poll-indicator centered">
          <span class="pulse-dot" :class="{ live: sessions.polling }" />
          <span v-if="updatedSecondsAgo !== null">{{ t('checkout.list.updatedSecondsAgo', { seconds: updatedSecondsAgo }) }}</span>
        </div>
      </div>
      <CheckoutSessionsTable v-else :sessions="filteredLive" mode="live" />
    </template>

    <template v-else>
      <div v-if="showHistorySkeleton" class="skel-block">
        <Skeleton height="240px" radius="11px" />
      </div>
      <div v-else-if="sessions.history.length === 0" class="card empty-note">
        <div class="big">{{ t('checkout.list.historyEmptyTitle') }}</div>
        <p>{{ t('checkout.list.historyEmptyBody') }}</p>
      </div>
      <template v-else>
        <CheckoutSessionsTable :sessions="sessions.history" mode="history" />
        <div v-if="sessions.historyHasMore" class="load-more">
          <button type="button" class="load-more-btn" :disabled="sessions.historyLoadingMore" @click="sessions.loadMoreHistory()">
            {{ sessions.historyLoadingMore ? t('checkout.list.loading') : t('checkout.list.loadMore') }}
          </button>
        </div>
      </template>
    </template>
  </div>
</template>

<style scoped>
.panel { display: flex; flex-direction: column; }

.controls-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
  margin-bottom: 16px;
}

.segmented {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  padding: 3px;
  background: var(--surface2);
  border-radius: 99px;
}
.segmented button {
  padding: 7px 16px;
  border-radius: 99px;
  font-size: 13.5px;
  font-weight: 600;
  color: var(--muted);
}
.segmented button.active { background: var(--surface); color: var(--ink); box-shadow: var(--shadow); }
.segmented button[role='tab']:hover:not(.active) { color: var(--ink); }

.stalled-badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  margin-left: 6px;
  padding: 7px 14px;
  border-radius: 99px;
  background: color-mix(in oklab, var(--warn) 15%, var(--surface));
  color: var(--warn);
  font-size: 13px;
  font-weight: 700;
}
.stalled-badge:hover { background: color-mix(in oklab, var(--warn) 24%, var(--surface)); }
.stalled-badge :deep(svg) { width: 13px; height: 13px; }

.poll-indicator {
  display: flex;
  align-items: center;
  gap: 9px;
  color: var(--faint);
  font-size: 12.5px;
}
.poll-indicator.centered { justify-content: center; margin-top: 14px; }
.pulse-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--line2); flex-shrink: 0; }
.pulse-dot.live { background: var(--ok); }
@media (prefers-reduced-motion: no-preference) {
  .pulse-dot.live { animation: pulse 2s ease-in-out infinite; }
  @keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.35; } }
}
.refresh-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  color: var(--muted);
  font-size: 12.5px;
  font-weight: 600;
}
.refresh-btn:hover:not(:disabled) { color: var(--ink); }
.refresh-btn:disabled { opacity: 0.5; }
.refresh-btn :deep(svg) { width: 12px; height: 12px; }

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
  margin-bottom: 18px;
}
.filters :deep(.field) { margin-bottom: 0; min-width: 150px; }
.escalated-toggle {
  display: flex;
  align-items: center;
  gap: 9px;
  font-size: 13px;
  color: var(--muted);
  padding-bottom: 2px;
}
.clear-btn {
  margin-left: auto;
  color: var(--faint);
  font-size: 12.5px;
  font-weight: 600;
  padding-bottom: 4px;
}
.clear-btn:hover { color: var(--ink); }

.empty-note { text-align: center; padding: 44px 20px; color: var(--muted); }
.empty-note .big {
  font-family: var(--font-display);
  font-weight: var(--display-weight);
  font-size: 20px;
  color: var(--ink);
  margin-bottom: 6px;
}
.empty-note p { max-width: 46ch; margin: 0 auto; font-size: 13.5px; }

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
