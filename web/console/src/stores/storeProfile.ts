// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

import { defineStore } from 'pinia'
import { isAxiosError } from 'axios'
import { getStoreProfile, updateStoreProfile } from '@/api/storeProfile'
import type { components } from '@/api/schema'

type StoreProfile = components['schemas']['store_profile']
type StoreProfileUpdate = components['schemas']['store_profile_update']

export const useStoreProfileStore = defineStore('storeProfile', {
  state: () => ({
    profile: null as StoreProfile | null,
    loading: false,
  }),
  actions: {
    async load(storeId: string) {
      this.loading = true
      try {
        this.profile = await getStoreProfile(storeId)
      } catch (err) {
        // A store created before a profile was ever saved for it has no
        // profile document yet — the backend 404s by design. Treat that as
        // an empty profile rather than a load failure.
        if (isAxiosError(err) && err.response?.status === 404) {
          this.profile = null
        } else {
          throw err
        }
      } finally {
        this.loading = false
      }
    },
    async save(storeId: string, update: StoreProfileUpdate) {
      this.profile = await updateStoreProfile(storeId, update)
    },
  },
})
