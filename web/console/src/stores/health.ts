// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

import { defineStore } from 'pinia'
import { getHealth, listConsistency } from '@/api/health'
import { acknowledgeAlert, resolveAlert } from '@/api/alerts'
import { useStoresStore } from '@/stores/stores'
import type { components } from '@/api/schema'

type Health = components['schemas']['health']
type ConsistencyFinding = components['schemas']['consistency_finding']

export const useHealthStore = defineStore('health', {
  state: () => ({
    health: null as Health | null,
    findings: [] as ConsistencyFinding[],
    loading: false,
    reloadQueued: false,
    loaded: false,
    error: null as string | null,
    // Per-alert-id in-flight state for the acknowledge/resolve buttons —
    // keyed rather than a single flag so acting on one alert doesn't
    // disable the buttons on every other alert in the list.
    alertActionPending: {} as Record<string, boolean>,
  }),
  actions: {
    // A probe cycle publishes one event per check, so health.updated arrives
    // in bursts of four or five. Queue a single re-fetch rather than firing a
    // pair of requests per frame; the last one is the one that matters.
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
        const [h, c] = await Promise.all([getHealth(storeId), listConsistency(storeId)])
        this.health = h
        this.findings = c
        this.loaded = true
      } catch (e: unknown) {
        this.error = e instanceof Error ? e.message : 'Failed to load health'
      } finally {
        this.loading = false
      }
      if (this.reloadQueued) {
        this.reloadQueued = false
        await this.load()
      }
    },

    async acknowledge(alertId: string) {
      await this.mutateAlert(alertId, () => acknowledgeAlert(alertId))
    },

    async resolve(alertId: string) {
      await this.mutateAlert(alertId, () => resolveAlert(alertId))
    },

    async mutateAlert(alertId: string, action: () => Promise<unknown>) {
      this.alertActionPending = { ...this.alertActionPending, [alertId]: true }
      try {
        await action()
        // Re-fetch rather than patching client-side state — the alert's
        // effect on endpoint_status.checks (and possibly on the check's
        // flap-suppression state) is derived server-side, not something
        // the client should try to reconstruct.
        await this.load()
      } finally {
        const next = { ...this.alertActionPending }
        delete next[alertId]
        this.alertActionPending = next
      }
    },
  },
})
