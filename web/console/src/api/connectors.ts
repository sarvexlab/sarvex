// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { api } from './client'
import type { components } from './schema'

type Connector = components['schemas']['connector']
type ConnectorPairing = components['schemas']['connector_pairing']
type Platform = components['schemas']['platform']
type ListEnvelope<T> = { data: T[]; page: { cursor: string | null; has_more: boolean } }

export async function listConnectors(): Promise<Connector[]> {
  const { data } = await api.get<ListEnvelope<Connector>>('/connectors')
  return data.data
}

export async function getConnector(connectorId: string): Promise<Connector> {
  const { data } = await api.get<Connector>(`/connectors/${encodeURIComponent(connectorId)}`)
  return data
}

// Pairing is store-scoped (sarvex-v1-control-plane.yaml
// POST /stores/{store_id}/connectors/pairing): a merchant may have several
// stores, so the backend cannot infer which one from the bearer.
export async function createConnector(storeId: string, platform: Platform): Promise<ConnectorPairing> {
  const { data } = await api.post<ConnectorPairing>(
    `/stores/${encodeURIComponent(storeId)}/connectors/pairing`,
    { platform },
  )
  return data
}

export async function resyncConnector(connectorId: string): Promise<{ job_id?: string; status?: string }> {
  const { data } = await api.post<{ job_id?: string; status?: string }>(
    `/connectors/${encodeURIComponent(connectorId)}/resync`,
  )
  return data
}

export async function disconnectConnector(connectorId: string): Promise<void> {
  await api.delete(`/connectors/${encodeURIComponent(connectorId)}`)
}
