// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// The merchant's stores + the active selection. V1 has one store per merchant
// but the model permits many — every surface reads currentStoreId from here.
//
// The "needs to /connect?" decision lives in needsConnectGuard.ts so it stays
// a non-cached computation across both stores.ts and connectors.ts — a Pinia
// getter that reads from another store can have its dependency set frozen by
// an early return on first eval, and there's no clean way to force it to
// re-track without making the getter return-paths uniform. A small pure
// function is easier to read and impossible to cache-stale.

import { defineStore } from 'pinia'
import { listStores } from '@/api/stores'
import type { components } from '@/api/schema'

type Store = components['schemas']['store']

// Persisted to localStorage so a refresh keeps whichever store was selected
// instead of always resetting to stores[0] — Pinia state is in-memory only
// and starts fresh on every load.
const STORAGE_KEY = 'sarvex.currentStoreId'

export const useStoresStore = defineStore('stores', {
  state: () => ({
    stores: [] as Store[],
    currentStoreId: null as string | null,
    loaded: false,
    loading: false,
    _inflight: null as Promise<void> | null,
  }),

  getters: {
    currentStore: (s): Store | null =>
      s.stores.find((x) => x.store_id === s.currentStoreId) ?? s.stores[0] ?? null,
  },

  actions: {
    async load() {
      // Concurrent calls share the in-flight promise so callers always
      // observe the latest fetched state.
      if (this._inflight) return this._inflight
      this._inflight = (async () => {
        this.loading = true
        try {
          this.stores = await listStores()
          if (!this.currentStoreId) {
            const remembered = localStorage.getItem(STORAGE_KEY)
            const stillExists = remembered && this.stores.some((s) => s.store_id === remembered)
            this.currentStoreId = stillExists ? remembered : (this.stores[0]?.store_id ?? null)
          }
          this.loaded = true
        } finally {
          this.loading = false
          this._inflight = null
        }
      })()
      return this._inflight
    },
    select(storeId: string) {
      if (this.stores.some((s) => s.store_id === storeId)) {
        this.currentStoreId = storeId
        localStorage.setItem(STORAGE_KEY, storeId)
      }
    },
    // The store list's `domain` is denormalized from the store profile
    // (server-resolved from business_profile.storefront_url). Saving the
    // profile's domain doesn't refetch the store list, so patch it in place
    // here to keep "Domain & profile" in sync without a page reload.
    setDomain(storeId: string, domain: string) {
      const s = this.stores.find((x) => x.store_id === storeId)
      if (s) s.domain = domain
    },
  },
})
