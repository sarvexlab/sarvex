// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

import { defineStore } from 'pinia'
import { listOrders } from '@/api/orders'
import { useStoresStore } from '@/stores/stores'
import type { components } from '@/api/schema'

type Order = components['schemas']['order']

export const useOrdersStore = defineStore('orders', {
  state: () => ({
    orders: [] as Order[],
    loading: false,
    loaded: false,
    error: null as string | null,
  }),
  actions: {
    async load() {
      const storeId = useStoresStore().currentStoreId
      if (!storeId) return
      this.loading = true
      this.error = null
      try {
        this.orders = await listOrders(storeId)
        this.loaded = true
      } catch (e: unknown) {
        this.error = e instanceof Error ? e.message : 'Failed to load orders'
      } finally {
        this.loading = false
      }
    },
  },
})
