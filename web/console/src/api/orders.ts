// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

import { api } from './client'
import type { components } from './schema'

type Order = components['schemas']['order']
type OrderStatus = components['schemas']['order_status']
type ListEnvelope<T> = { data: T[]; page: { cursor: string | null; has_more: boolean } }

export async function listOrders(
  storeId: string,
  filter: { status?: OrderStatus } = {},
): Promise<Order[]> {
  const { data } = await api.get<ListEnvelope<Order>>(
    `/stores/${encodeURIComponent(storeId)}/orders`,
    { params: filter },
  )
  return data.data
}

