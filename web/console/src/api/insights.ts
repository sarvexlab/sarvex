// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Agent Behavior Insights read API — GET /insights/capability_gaps,
// /insights/product_demand, /insights/negotiation (sarvex-v1-control-plane.yaml).
// Gated server-side at the analytics tier (403 analytics_tier_required, same
// gate as the Lost Revenue Ledger / Channel Dashboard) — the store decides
// what to render, this module only shapes requests.
//
// Known contract gaps (verified against internal/planes/control/insights and
// internal/planes/control/api/insights.go on main, not worked around here):
//   - No /insights/attribute_gaps route exists. The OpenAPI's own comment
//     block above /insights/capability_gaps explains why: the UCP request
//     types have no protocol-level way for an agent to name a requested
//     attribute, so there is nothing honest to serve. AttributeGapsSection.vue
//     renders an explanatory unavailable state instead of calling anything.
//   - insights_product_demand has no reached_cart_count/completed_count —
//     only request_count + surface_breakdown ship. No conversion rate is
//     derivable; ProductDemandSection.vue shows counts only.
//   - estimated_impact_minor has no accompanying `currency` field anywhere
//     on insights_capability_gap. In this build impact_confidence is always
//     "unmeasurable" (estimated_impact_minor always 0), so this never
//     surfaces in practice — see lib/insights.ts's formatCapabilityImpact
//     for how a hypothetical nonzero measured/partial figure degrades
//     without a currency to format it in.

import { api } from './client'
import type { components } from './schema'

export type CapabilityGap = components['schemas']['insights_capability_gap']
export type ImpactConfidence = components['schemas']['insights_impact_confidence']
export type ProductDemand = components['schemas']['insights_product_demand']
export type NegotiationInsights = components['schemas']['insights_negotiation']
export type NegotiationFailure = components['schemas']['insights_negotiation_failure']
export type NegotiationFailureReason = components['schemas']['insights_negotiation_failure_reason']
export type PaymentHandlerMismatch = components['schemas']['insights_payment_handler_mismatch']

type ListEnvelope<T> = { data: T[]; page: { cursor: string | null; has_more: boolean } }

export async function listCapabilityGaps(params: {
  month?: string
  store_id?: string
}): Promise<CapabilityGap[]> {
  const { data } = await api.get<ListEnvelope<CapabilityGap>>('/insights/capability_gaps', { params })
  return data.data
}

export type InsightsRangeFilter = {
  from?: string
  to?: string
  surface?: string
  limit?: number
}

export async function listProductDemand(filter: InsightsRangeFilter = {}): Promise<ProductDemand[]> {
  const { data } = await api.get<ListEnvelope<ProductDemand>>('/insights/product_demand', { params: filter })
  return data.data
}

export async function getNegotiationInsights(filter: {
  from?: string
  to?: string
}): Promise<NegotiationInsights> {
  const { data } = await api.get<NegotiationInsights>('/insights/negotiation', { params: filter })
  return data
}
