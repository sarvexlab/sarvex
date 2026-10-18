// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { api } from './client'
import type { components } from './schema'

type Billing = components['schemas']['billing']

export async function getBilling(): Promise<Billing> {
  const { data } = await api.get<Billing>('/billing')
  return data
}

// Plan switching and cancellation happen inside the Stripe portal session
// itself (self_serve is never offered as a switch target) — there is no
// separate checkout-link endpoint.
export async function createPortalLink(): Promise<string> {
  const { data } = await api.post<{ url: string }>('/billing/portal-link')
  return data.url
}
