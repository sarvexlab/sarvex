// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

import { defineStore } from 'pinia'
import { createConnector, getConnector, listConnectors } from '@/api/connectors'
import type { components } from '@/api/schema'

type Connector = components['schemas']['connector']
type ConnectorPairing = components['schemas']['connector_pairing']
type Platform = components['schemas']['platform']

export const useConnectorsStore = defineStore('connectors', {
  state: () => ({
    connectors: [] as Connector[],
    pairing: null as ConnectorPairing | null,
    loading: false,
  }),

  getters: {
    forStore: (s) => (storeId: string) =>
      s.connectors.find((c) => c.store_id === storeId) ?? null,
  },

  actions: {
    async load() {
      this.loading = true
      try {
        this.connectors = await listConnectors()
      } finally {
        this.loading = false
      }
    },

    async beginPairing(storeId: string, platform: Platform): Promise<ConnectorPairing> {
      const result = await createConnector(storeId, platform)
      this.pairing = result
      const i = this.connectors.findIndex((c) => c.connector_id === result.connector_id)
      if (i >= 0) this.connectors.splice(i, 1, result)
      else this.connectors.push(result)
      return result
    },

    async refreshOne(connectorId: string): Promise<Connector> {
      const c = await getConnector(connectorId)
      const i = this.connectors.findIndex((x) => x.connector_id === connectorId)
      if (i >= 0) this.connectors.splice(i, 1, c)
      else this.connectors.push(c)
      return c
    },

    clearPairing() {
      this.pairing = null
    },
  },
})
