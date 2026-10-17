// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Checkout Escalation Console read API. Every route here is read-only —
// there is no mutation surface (no retry/re-run — see sarvex-v1-control-plane.yaml's
// comment on the Checkout Sessions section). Available at every billing tier.

import { api } from './client'
import type { components } from './schema'

export type CheckoutSession = components['schemas']['checkout_session']
export type CheckoutSessionState = components['schemas']['checkout_session_state']
export type CheckoutSessionTimelineEntry = components['schemas']['checkout_session_timeline_entry']

type ListEnvelope<T> = { data: T[]; page: { cursor: string | null; has_more: boolean } }

export type ListCheckoutSessionsFilter = {
  state?: CheckoutSessionState
  surface?: string
  store_id?: string
  cause?: string
  escalated_only?: boolean
  from?: string
  to?: string
  cursor?: string
  limit?: number
}

export async function listCheckoutSessions(
  filter: ListCheckoutSessionsFilter = {},
): Promise<ListEnvelope<CheckoutSession>> {
  const { data } = await api.get<ListEnvelope<CheckoutSession>>('/checkout-sessions', {
    params: filter,
  })
  return data
}

export async function listLiveCheckoutSessions(
  params: { window_minutes?: number; limit?: number } = {},
): Promise<ListEnvelope<CheckoutSession>> {
  const { data } = await api.get<ListEnvelope<CheckoutSession>>('/checkout-sessions/live', {
    params,
  })
  return data
}

export async function getCheckoutSession(checkoutId: string): Promise<CheckoutSession> {
  const { data } = await api.get<CheckoutSession>(
    `/checkout-sessions/${encodeURIComponent(checkoutId)}`,
  )
  return data
}

export async function getCheckoutSessionTimeline(
  checkoutId: string,
  params: { cursor?: string; limit?: number } = {},
): Promise<ListEnvelope<CheckoutSessionTimelineEntry>> {
  const { data } = await api.get<ListEnvelope<CheckoutSessionTimelineEntry>>(
    `/checkout-sessions/${encodeURIComponent(checkoutId)}/timeline`,
    { params },
  )
  return data
}
