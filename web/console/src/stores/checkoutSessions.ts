// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Checkout Escalation Console state: the Live poll, the History list, the
// stalled-escalation count badge, and a single session's detail + timeline.
//
// Endpoints (all read-only, no mutation surface — see api/checkoutSessions.ts):
//   GET /checkout-sessions/live
//   GET /checkout-sessions
//   GET /checkout-sessions/{id}
//   GET /checkout-sessions/{id}/timeline
//
// Stalled-escalation badge: EscalationStalledThreshold defaults to 60
// minutes (checkoutescalation/sweep.go) while the Live window defaults to
// 30 minutes (sarvex-v1-control-plane.yaml) — a session flagged stalled has,
// by construction, gone quiet for longer than the Live window covers, so it
// generally will NOT appear in /checkout-sessions/live. The badge is
// therefore backed by its own independent poll of
// GET /checkout-sessions?cause=escalation_stalled (History's endpoint, the
// only one with a `cause` filter), decoupled from which list mode is on
// screen, so R5 ("must be visible without the user constructing a filter")
// holds in both Live and History mode.

import { defineStore } from 'pinia'
import {
  getCheckoutSession,
  getCheckoutSessionTimeline,
  listCheckoutSessions,
  listLiveCheckoutSessions,
  type CheckoutSession,
  type CheckoutSessionTimelineEntry,
  type ListCheckoutSessionsFilter,
} from '@/api/checkoutSessions'

const POLL_INTERVAL_MS = 15_000
const STALLED_CAUSE = 'escalation_stalled'
const STALLED_LIMIT = 100

export const useCheckoutSessionsStore = defineStore('checkoutSessions', {
  state: () => ({
    // Live
    live: [] as CheckoutSession[],
    liveLoading: false,
    liveLoaded: false,
    liveError: null as string | null,
    liveHasMore: false,
    lastPolledAt: null as string | null,
    pollHandle: null as ReturnType<typeof setInterval> | null,
    polling: false,

    // History
    history: [] as CheckoutSession[],
    historyFilter: {} as ListCheckoutSessionsFilter,
    historyCursor: null as string | null,
    historyHasMore: false,
    historyLoading: false,
    historyLoadingMore: false,
    historyLoaded: false,
    historyError: null as string | null,

    // Stalled-escalation badge (see module doc above)
    stalledCount: 0,
    stalledHasMore: false,
    stalledLoading: false,
    stalledError: null as string | null,
    stalledPollHandle: null as ReturnType<typeof setInterval> | null,

    // Detail
    detail: null as CheckoutSession | null,
    detailLoading: false,
    detailLoaded: false,
    detailError: null as string | null,

    // Timeline (of the currently open detail session)
    timeline: [] as CheckoutSessionTimelineEntry[],
    timelineCursor: null as string | null,
    timelineHasMore: false,
    timelineLoading: false,
    timelineLoadingMore: false,
    timelineLoaded: false,
    timelineError: null as string | null,
  }),

  getters: {
    stalledBadge: (s): string => (s.stalledHasMore ? `${STALLED_LIMIT}+` : String(s.stalledCount)),
  },

  actions: {
    // ── Live ────────────────────────────────────────────────────────────
    async loadLive() {
      this.liveLoading = true
      this.liveError = null
      try {
        const page = await listLiveCheckoutSessions()
        this.live = page.data
        this.liveHasMore = page.page.has_more
        this.liveLoaded = true
        this.lastPolledAt = new Date().toISOString()
      } catch (e: unknown) {
        this.liveError = e instanceof Error ? e.message : 'Failed to load live checkout sessions'
      } finally {
        this.liveLoading = false
      }
    },

    startPolling() {
      this.stopPolling()
      this.polling = true
      this.pollHandle = setInterval(() => this.loadLive(), POLL_INTERVAL_MS)
    },

    stopPolling() {
      if (this.pollHandle !== null) {
        clearInterval(this.pollHandle)
        this.pollHandle = null
      }
      this.polling = false
    },

    // ── History ─────────────────────────────────────────────────────────
    async loadHistory(filter: ListCheckoutSessionsFilter = {}) {
      this.historyLoading = true
      this.historyError = null
      this.historyFilter = filter
      try {
        const page = await listCheckoutSessions(filter)
        this.history = page.data
        this.historyCursor = page.page.cursor
        this.historyHasMore = page.page.has_more
        this.historyLoaded = true
      } catch (e: unknown) {
        this.historyError = e instanceof Error ? e.message : 'Failed to load checkout sessions'
      } finally {
        this.historyLoading = false
      }
    },

    async loadMoreHistory() {
      if (!this.historyHasMore || this.historyLoadingMore || !this.historyCursor) return
      this.historyLoadingMore = true
      this.historyError = null
      try {
        const page = await listCheckoutSessions({
          ...this.historyFilter,
          cursor: this.historyCursor,
        })
        this.history = [...this.history, ...page.data]
        this.historyCursor = page.page.cursor
        this.historyHasMore = page.page.has_more
      } catch (e: unknown) {
        this.historyError = e instanceof Error ? e.message : 'Failed to load more checkout sessions'
      } finally {
        this.historyLoadingMore = false
      }
    },

    // ── Stalled-escalation badge ───────────────────────────────────────
    async loadStalledCount() {
      this.stalledLoading = true
      this.stalledError = null
      try {
        const page = await listCheckoutSessions({ cause: STALLED_CAUSE, limit: STALLED_LIMIT })
        this.stalledCount = page.data.length
        this.stalledHasMore = page.page.has_more
      } catch (e: unknown) {
        this.stalledError = e instanceof Error ? e.message : 'Failed to load stalled escalations'
      } finally {
        this.stalledLoading = false
      }
    },

    startStalledPolling() {
      this.stopStalledPolling()
      this.stalledPollHandle = setInterval(() => this.loadStalledCount(), POLL_INTERVAL_MS)
    },

    stopStalledPolling() {
      if (this.stalledPollHandle !== null) {
        clearInterval(this.stalledPollHandle)
        this.stalledPollHandle = null
      }
    },

    // ── Detail ──────────────────────────────────────────────────────────
    async loadDetail(checkoutId: string) {
      this.detailLoading = true
      this.detailError = null
      try {
        this.detail = await getCheckoutSession(checkoutId)
        this.detailLoaded = true
      } catch (e: unknown) {
        this.detailError = e instanceof Error ? e.message : 'Failed to load checkout session'
      } finally {
        this.detailLoading = false
      }
    },

    clearDetail() {
      this.detail = null
      this.detailLoaded = false
      this.detailError = null
      this.timeline = []
      this.timelineCursor = null
      this.timelineHasMore = false
      this.timelineLoaded = false
      this.timelineError = null
    },

    // ── Timeline ────────────────────────────────────────────────────────
    async loadTimeline(checkoutId: string) {
      this.timelineLoading = true
      this.timelineError = null
      try {
        const page = await getCheckoutSessionTimeline(checkoutId)
        this.timeline = page.data
        this.timelineCursor = page.page.cursor
        this.timelineHasMore = page.page.has_more
        this.timelineLoaded = true
      } catch (e: unknown) {
        this.timelineError = e instanceof Error ? e.message : 'Failed to load timeline'
      } finally {
        this.timelineLoading = false
      }
    },

    async loadMoreTimeline(checkoutId: string) {
      if (!this.timelineHasMore || this.timelineLoadingMore || this.timelineCursor === null) return
      this.timelineLoadingMore = true
      this.timelineError = null
      try {
        const page = await getCheckoutSessionTimeline(checkoutId, { cursor: this.timelineCursor })
        this.timeline = [...this.timeline, ...page.data]
        this.timelineCursor = page.page.cursor
        this.timelineHasMore = page.page.has_more
      } catch (e: unknown) {
        this.timelineError = e instanceof Error ? e.message : 'Failed to load more timeline'
      } finally {
        this.timelineLoadingMore = false
      }
    },
  },
})
