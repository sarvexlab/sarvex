// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { defineStore } from 'pinia'
import { getActivation, verifyActivation } from '@/api/activation'
import { useStoresStore } from '@/stores/stores'
import type { Activation, VerificationReport } from '@/api/activation'

export const useActivationStore = defineStore('activation', {
  state: () => ({
    activation: null as Activation | null,
    report: null as VerificationReport | null,
    loaded: false,
    loading: false,
    verifying: false,
    error: null as string | null,
  }),
  getters: {
    /** The terminal success state. A store here is finished — with zero
     * surfaces enrolled, which is a supported, complete condition and not a
     * partial one. */
    isActive(s): boolean {
      return s.activation?.ucp_state === 'active'
    },
    isSuspended(s): boolean {
      return s.activation?.ucp_state === 'suspended'
    },
    /** The step the store is currently working toward, if any. */
    currentStep(s) {
      return s.activation?.steps?.find((step) => step.blocking)
    },
  },
  actions: {
    async load() {
      const storeId = useStoresStore().currentStoreId
      if (!storeId) return
      this.loading = true
      this.error = null
      try {
        this.activation = await getActivation(storeId)
        this.loaded = true
      } catch (e: unknown) {
        this.error = e instanceof Error ? e.message : 'Failed to load activation state'
      } finally {
        this.loading = false
      }
    },

    /** A verification run that finds failures is a normal outcome, not an
     * error: the report is kept either way and the activation state is
     * re-read to reflect wherever the run left the store. */
    async verify() {
      const storeId = useStoresStore().currentStoreId
      if (!storeId) return
      this.verifying = true
      this.error = null
      try {
        this.report = await verifyActivation(storeId)
        await this.load()
      } catch (e: unknown) {
        this.error = e instanceof Error ? e.message : 'Verification could not be run'
      } finally {
        this.verifying = false
      }
    },
  },
})
