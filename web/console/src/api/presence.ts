// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { api } from './client'
import type { components } from './schema'

type Presence = components['schemas']['presence']
type ManifestView = components['schemas']['manifest_view']

export async function getPresence(storeId: string): Promise<Presence> {
  const { data } = await api.get<Presence>(`/stores/${encodeURIComponent(storeId)}/presence`)
  return data
}

export async function getManifest(storeId: string): Promise<ManifestView> {
  const { data } = await api.get<ManifestView>(
    `/stores/${encodeURIComponent(storeId)}/presence/manifest`,
  )
  return data
}
