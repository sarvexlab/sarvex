// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

import { api } from './client'
import type { components } from './schema'

type StoreProfile = components['schemas']['store_profile']
type StoreProfileUpdate = components['schemas']['store_profile_update']

export async function getStoreProfile(storeId: string): Promise<StoreProfile> {
  const { data } = await api.get<StoreProfile>(
    `/stores/${encodeURIComponent(storeId)}/profile`,
  )
  return data
}

export async function updateStoreProfile(
  storeId: string,
  update: StoreProfileUpdate,
): Promise<StoreProfile> {
  const { data } = await api.put<StoreProfile>(
    `/stores/${encodeURIComponent(storeId)}/profile`,
    update,
  )
  return data
}
