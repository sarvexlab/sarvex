// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { defineStore } from 'pinia'
import { getJourney, openJourneyStream } from '@/api/journey'
import { useStoresStore } from '@/stores/stores'
import { useTasksStore } from '@/stores/tasks'
import { useActivationStore } from '@/stores/activation'
import { useCatalogStore } from '@/stores/catalog'
import { useHealthStore } from '@/stores/health'
import { useOrdersStore } from '@/stores/orders'
import type { components } from '@/api/schema'

type Journey = components['schemas']['journey']

export const useJourneyStore = defineStore('journey', {
  state: () => ({
    journey: null as Journey | null,
    loading: false,
    reloadQueued: false,
    error: null as string | null,
    closeStream: null as (() => void) | null,
  }),
  actions: {
    // A push that lands while a fetch is already in flight queues one more
    // fetch rather than being dropped: the in-flight request may have been
    // issued before the change that pushed, so returning its result and
    // discarding the signal is how the Path ends up a state behind.
    async load() {
      const storeId = useStoresStore().currentStoreId
      if (!storeId) return
      if (this.loading) {
        this.reloadQueued = true
        return
      }
      this.loading = true
      this.error = null
      try {
        this.journey = await getJourney(storeId)
      } catch (e: unknown) {
        this.error = e instanceof Error ? e.message : 'Failed to load journey'
      } finally {
        this.loading = false
      }
      if (this.reloadQueued) {
        this.reloadQueued = false
        await this.load()
      }
    },
    clear() {
      this.journey = null
    },
    // Opens the live journey WebSocket for the current store, re-fetching on
    // every push. A no-op re-entry guard: calling this while already
    // connected to the same store is a cheap no-op (PathSurface calls it on
    // every store-id change, including the initial one). The one connection
    // carries every frame type and fans them out to the store that owns the
    // resource, so none of them needs a socket of its own. Journey is the
    // least of the five: the Path's stage bar reads activation.ucp_state, and
    // the readiness pill, the import task's product count and the audit task's
    // score all read the catalog store. The "Agent orders this week" and
    // "Prices & inventory" stat tiles read the orders and health stores
    // respectively — orders.updated and health.updated are what let those
    // move without a reload while the Path is open.
    connectStream() {
      const storeId = useStoresStore().currentStoreId
      this.disconnectStream()
      if (!storeId) return
      this.closeStream = openJourneyStream(storeId, {
        onJourneyUpdate: () => this.load(),
        onTaskUpdate: (taskId) => useTasksStore().refreshTask(taskId),
        onActivationUpdate: () => useActivationStore().load(),
        onCatalogUpdate: () => useCatalogStore().load(),
        onHealthUpdate: () => useHealthStore().load(),
        onOrdersUpdate: () => useOrdersStore().load(),
      })
    },
    disconnectStream() {
      this.closeStream?.()
      this.closeStream = null
    },
  },
})
