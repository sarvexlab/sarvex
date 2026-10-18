// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { defineStore } from 'pinia'
import { getProduct, getReadiness, listProducts } from '@/api/catalog'
import { useStoresStore } from '@/stores/stores'
import type { components } from '@/api/schema'

type Readiness = components['schemas']['readiness']
type ProductSummary = components['schemas']['product_summary']
type ProductDetail = components['schemas']['product_detail']

export const useCatalogStore = defineStore('catalog', {
  state: () => ({
    readiness: null as Readiness | null,
    products: [] as ProductSummary[],
    cursor: null as string | null,
    hasMore: false,
    // Which store `readiness`/`products` belong to. Every self-loading
    // widget (CatalogSyncTask, CatalogAuditTask, ReadinessPill, StageLayout)
    // used "readiness == null" as its "already loaded, skip" guard — true
    // the first time, but false forever after, including once the merchant
    // switches to a *different* store: the old store's readiness/product
    // count stayed in this singleton and got rendered as the new store's.
    // Comparing against storeId is what those guards actually meant.
    storeId: null as string | null,
    detail: null as ProductDetail | null,
    loading: false,
    loadingMore: false,
    loaded: false,
    reloadQueued: false,
    detailLoading: false,
    error: null as string | null,
    detailError: null as string | null,
  }),

  actions: {
    // Also driven by catalog.updated pushes (see stores/journey.ts
    // connectStream), which is why the in-flight case queues a single re-fetch
    // rather than either stacking parallel loads or dropping the signal: the
    // sync plane republishes per rebuild, so several can land in a burst while
    // one fetch is still out, and the last one is the one that matters.
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
        const [r, p] = await Promise.all([getReadiness(storeId), listProducts(storeId)])
        this.readiness = r
        this.products = p.products
        this.cursor = p.cursor
        this.hasMore = p.hasMore
        this.storeId = storeId
        this.loaded = true
      } catch (e: unknown) {
        this.error = e instanceof Error ? e.message : 'Failed to load catalog'
      } finally {
        this.loading = false
      }
      if (this.reloadQueued) {
        this.reloadQueued = false
        await this.load()
      }
    },

    async loadMore() {
      const storeId = useStoresStore().currentStoreId
      if (!storeId || !this.hasMore || this.loadingMore || !this.cursor) return
      this.loadingMore = true
      this.error = null
      try {
        const p = await listProducts(storeId, { cursor: this.cursor })
        this.products = [...this.products, ...p.products]
        this.cursor = p.cursor
        this.hasMore = p.hasMore
      } catch (e: unknown) {
        this.error = e instanceof Error ? e.message : 'Failed to load more products'
      } finally {
        this.loadingMore = false
      }
    },
    async loadDetail(sku: string) {
      const storeId = useStoresStore().currentStoreId
      if (!storeId) return
      this.detailLoading = true
      this.detail = null
      this.detailError = null
      try {
        this.detail = await getProduct(storeId, sku)
      } catch (e: unknown) {
        this.detailError = e instanceof Error ? e.message : 'Failed to load product'
      } finally {
        this.detailLoading = false
      }
    },
    clearDetail() {
      this.detail = null
    },
  },
})
