// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { api } from './client'
import type { components } from './schema'

type Store = components['schemas']['store']
type ListEnvelope<T> = { data: T[]; page: { cursor: string | null; has_more: boolean } }

export async function listStores(): Promise<Store[]> {
  const { data } = await api.get<ListEnvelope<Store>>('/stores')
  return data.data
}
