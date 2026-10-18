// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Channel Dashboard read API — GET /channel/summary, /channel/funnel,
// /channel/daily, /channel/comparison (sarvex-v1-control-plane.yaml).
// Gated server-side at the analytics tier (403 analytics_tier_required,
// same gate as the Lost Revenue Ledger) — the store decides what to render,
// this module only shapes requests.
//
// Resolved contract gaps (were worked around client-side, now fixed
// server-side — see internal/planes/control/api/channel.go and the
// "Close Channel Dashboard API-contract gaps" commit on main):
//   - currency/currency_mixed now ship on every response that sums across
//     days/surfaces (summary, funnel, comparison's agentic/web_baseline);
//     channel_daily_entry carries its own single-row currency. See
//     lib/channel.ts for how the console reads these.
//   - summary/funnel's `surface` is now typed against channel_surface_or_all
//     (channel_surface plus the literal "all" the combined row actually
//     emits) — the generated type here is exact, no override needed.
//   - sessions_confirmed ships alongside sessions_inferred.
//   - prior_period carries aov_minor_delta/completion_rate_delta.
//   - channel_metric_comparison/channel_share_comparison carry a tri-state
//     `confidence` (connector_reported | partial | unavailable);
//     available=true at confidence=partial still carries real numbers.
//
// sessionization_unavailable — the one gap NOT fixed — was evaluated and
// rejected by the backend: checkouts_initiated is defined to equal sessions
// by construction (every Session is checkout-anchored), so "checkout data
// without session data" cannot occur under this model; a flag that's always
// false would be fabricated, not a fix. ChannelFunnel.vue's no-data
// degradation (sessions === 0) stands as the honest signal instead.

import { api } from './client'
import type { components } from './schema'

export type ChannelSurface = components['schemas']['channel_surface']
export type ChannelSurfaceOrAll = components['schemas']['channel_surface_or_all']
export type ChannelBaselineConfidence = components['schemas']['channel_baseline_confidence']
export type ChannelSummary = components['schemas']['channel_summary']
export type ChannelFunnel = components['schemas']['channel_funnel']
export type ChannelDailyEntry = components['schemas']['channel_daily_entry']
export type ChannelComparison = components['schemas']['channel_comparison']

export async function getChannelSummary(params: {
  month?: string
  store_id?: string
}): Promise<ChannelSummary[]> {
  const { data } = await api.get<{ data: ChannelSummary[] }>('/channel/summary', { params })
  return data.data
}

export type ChannelRangeFilter = {
  from: string
  to: string
  surface?: string
  store_id?: string
}

export async function getChannelFunnel(filter: ChannelRangeFilter): Promise<ChannelFunnel> {
  const { data } = await api.get<ChannelFunnel>('/channel/funnel', { params: filter })
  return data
}

export async function getChannelDaily(filter: ChannelRangeFilter): Promise<ChannelDailyEntry[]> {
  const { data } = await api.get<{ data: ChannelDailyEntry[] }>('/channel/daily', { params: filter })
  return data.data
}

export async function getChannelComparison(filter: {
  from: string
  to: string
  store_id?: string
}): Promise<ChannelComparison> {
  const { data } = await api.get<ChannelComparison>('/channel/comparison', { params: filter })
  return data
}
