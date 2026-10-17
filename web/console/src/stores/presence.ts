// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

import { defineStore } from 'pinia'
import { getManifest, getPresence } from '@/api/presence'
import { useStoresStore } from '@/stores/stores'
import type { components } from '@/api/schema'

type Presence = components['schemas']['presence']
type ManifestView = components['schemas']['manifest_view']

export const usePresenceStore = defineStore('presence', {
  state: () => ({
    presence: null as Presence | null,
    manifest: null as ManifestView | null,
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
        const [p, m] = await Promise.all([getPresence(storeId), getManifest(storeId)])
        this.presence = p
        this.manifest = m
        this.loaded = true
      } catch (e: unknown) {
        this.error = e instanceof Error ? e.message : 'Failed to load presence'
      } finally {
        this.loading = false
      }
    },
  },
})
