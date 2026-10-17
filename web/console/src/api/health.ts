// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

import { api } from './client'
import type { components } from './schema'

type Health = components['schemas']['health']
type ConsistencyFinding = components['schemas']['consistency_finding']
type ListEnvelope<T> = { data: T[]; page: { cursor: string | null; has_more: boolean } }

export async function getHealth(storeId: string): Promise<Health> {
  const { data } = await api.get<Health>(`/stores/${encodeURIComponent(storeId)}/health`)
  return data
}

export async function listConsistency(storeId: string): Promise<ConsistencyFinding[]> {
  const { data } = await api.get<ListEnvelope<ConsistencyFinding>>(
    `/stores/${encodeURIComponent(storeId)}/health/consistency`,
  )
  return data.data
}

