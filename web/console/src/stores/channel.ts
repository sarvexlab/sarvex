// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Channel Dashboard state. Four independent sections (R15 — progressive
// rendering: a failing comparison must not blank the KPIs), each with its
// own loading/loaded/error triple and its own load action so the view can
// retry one section without refetching the others.

import { defineStore } from 'pinia'
import {
  getChannelSummary,
  getChannelFunnel,
  getChannelDaily,
  getChannelComparison,
  type ChannelSummary,
  type ChannelFunnel,
  type ChannelDailyEntry,
  type ChannelComparison,
} from '@/api/channel'

type RangeArgs = { from: string; to: string; surface?: string; store_id?: string }

export const useChannelStore = defineStore('channel', {
  state: () => ({
    summaries: [] as ChannelSummary[],
    summaryLoading: false,
    summaryLoaded: false,
    summaryError: null as string | null,

    funnel: null as ChannelFunnel | null,
    funnelLoading: false,
    funnelLoaded: false,
    funnelError: null as string | null,

    daily: [] as ChannelDailyEntry[],
    dailyLoading: false,
    dailyLoaded: false,
    dailyError: null as string | null,

    comparison: null as ChannelComparison | null,
    comparisonLoading: false,
    comparisonLoaded: false,
    comparisonError: null as string | null,
  }),

  getters: {
    /** Distinct surfaces the merchant actually has summary data for this
     * period, in server order, "all" excluded — the surface filter's option
     * list (R3). Driven entirely by response data, never a hardcoded enum,
     * so a surface introduced server-side appears with no frontend change. */
    availableSurfaces: (s): string[] =>
      s.summaries.map((row) => row.surface as string | undefined).filter((v): v is string => !!v && v !== 'all'),
  },

  actions: {
    async loadSummary(args: { month?: string; store_id?: string }) {
      this.summaryLoading = true
      this.summaryError = null
      try {
        this.summaries = await getChannelSummary(args)
        this.summaryLoaded = true
      } catch (e: unknown) {
        this.summaryError = e instanceof Error ? e.message : 'Failed to load the channel summary'
      } finally {
        this.summaryLoading = false
      }
    },

    async loadFunnel(args: RangeArgs) {
      this.funnelLoading = true
      this.funnelError = null
      try {
        this.funnel = await getChannelFunnel(args)
        this.funnelLoaded = true
      } catch (e: unknown) {
        this.funnelError = e instanceof Error ? e.message : 'Failed to load the funnel'
      } finally {
        this.funnelLoading = false
      }
    },

    async loadDaily(args: RangeArgs) {
      this.dailyLoading = true
      this.dailyError = null
      try {
        this.daily = await getChannelDaily(args)
        this.dailyLoaded = true
      } catch (e: unknown) {
        this.dailyError = e instanceof Error ? e.message : 'Failed to load the daily trend'
      } finally {
        this.dailyLoading = false
      }
    },

    async loadComparison(args: { from: string; to: string; store_id?: string }) {
      this.comparisonLoading = true
      this.comparisonError = null
      try {
        this.comparison = await getChannelComparison(args)
        this.comparisonLoaded = true
      } catch (e: unknown) {
        this.comparisonError = e instanceof Error ? e.message : 'Failed to load the web-baseline comparison'
      } finally {
        this.comparisonLoading = false
      }
    },
  },
})
