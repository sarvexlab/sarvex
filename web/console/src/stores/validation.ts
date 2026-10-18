// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { defineStore } from 'pinia'
import { getValidationStatus, listValidationRuns, triggerValidationRun } from '@/api/validation'
import { useStoresStore } from '@/stores/stores'
import { useActivationStore } from '@/stores/activation'
import type { components } from '@/api/schema'

type Status = components['schemas']['validation_status']
type Record = components['schemas']['conformance_record']

export const useValidationStore = defineStore('validation', {
  state: () => ({
    status: null as Status | null,
    runs: [] as Record[],
    loading: false,
    loaded: false,
    triggering: false,
    error: null as string | null,
  }),
  actions: {
    async load() {
      const storeId = useStoresStore().currentStoreId
      if (!storeId) return
      this.loading = true
      this.error = null
      try {
        const [s, r] = await Promise.all([
          getValidationStatus(storeId),
          listValidationRuns(storeId),
        ])
        this.status = s
        this.runs = r
        this.loaded = true
      } catch (e: unknown) {
        this.error = e instanceof Error ? e.message : 'Failed to load validation'
      } finally {
        this.loading = false
      }
    },
    // A passing run also verifies the store's own UCP endpoints server-side
    // (validation.Orchestrator.RunValidation), which advances activation and
    // writes the check results onto the activation record — so the activation
    // store is re-read here rather than left to the websocket push, which the
    // panel showing those checks should not have to depend on.
    async triggerRun() {
      const storeId = useStoresStore().currentStoreId
      if (!storeId) return
      this.triggering = true
      try {
        await triggerValidationRun(storeId)
        await Promise.all([this.load(), useActivationStore().load()])
      } finally {
        this.triggering = false
      }
    },
  },
})
