// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { defineStore } from 'pinia'
import {
  enrollSurface,
  evaluatePrerequisites,
  listStoreSurfaces,
  setSurfaceConfig,
  submitSurface,
  withdrawSurface,
} from '@/api/surfaces'
import { useStoresStore } from '@/stores/stores'
import type { SurfaceEnrollment } from '@/api/surfaces'

// One store for every surface. There is deliberately no per-surface state
// here and no branch on a surface id anywhere in this file: a new surface
// appears in `enrollments` because the registry returned it, and every
// action below works on it unchanged.

export const useSurfacesStore = defineStore('surfaces', {
  state: () => ({
    enrollments: [] as SurfaceEnrollment[],
    loaded: false,
    loading: false,
    error: null as string | null,
    /** Surface ids with an action in flight, so each card can show its own
     * pending state without a single global spinner. */
    busy: [] as string[],
  }),
  getters: {
    /** Surfaces the store is actually live on. */
    enrolled(s): SurfaceEnrollment[] {
      return s.enrollments.filter((e) => e.state === 'enrolled')
    },
    /** True while the store is not yet UCP active — the one thing that
     * holds every enrollment at `available`. */
    blocked(s): boolean {
      return s.enrollments.some((e) => e.blocked)
    },
    byId(s): (surfaceId: string) => SurfaceEnrollment | undefined {
      return (surfaceId: string) => s.enrollments.find((e) => e.surface_id === surfaceId)
    },
    isBusy(s): (surfaceId: string) => boolean {
      return (surfaceId: string) => s.busy.includes(surfaceId)
    },
  },
  actions: {
    async load() {
      const storeId = useStoresStore().currentStoreId
      if (!storeId) return
      this.loading = true
      this.error = null
      try {
        this.enrollments = await listStoreSurfaces(storeId)
        this.loaded = true
      } catch (e: unknown) {
        this.error = e instanceof Error ? e.message : 'Failed to load surfaces'
      } finally {
        this.loading = false
      }
    },

    /** Runs one surface action and folds the returned enrollment back into
     * the list. Every action below shares this, so they all report errors
     * and clear busy state the same way. */
    async run(
      surfaceId: string,
      action: (storeId: string) => Promise<SurfaceEnrollment>,
    ): Promise<void> {
      const storeId = useStoresStore().currentStoreId
      if (!storeId) return
      this.busy = [...this.busy, surfaceId]
      this.error = null
      try {
        const updated = await action(storeId)
        this.merge(updated)
      } catch (e: unknown) {
        this.error = e instanceof Error ? e.message : 'Action failed'
      } finally {
        this.busy = this.busy.filter((id) => id !== surfaceId)
      }
    },

    merge(updated: SurfaceEnrollment) {
      const i = this.enrollments.findIndex((e) => e.surface_id === updated.surface_id)
      if (i === -1) this.enrollments = [...this.enrollments, updated]
      else this.enrollments = this.enrollments.map((e, idx) => (idx === i ? updated : e))
    },

    enroll(surfaceId: string) {
      return this.run(surfaceId, (storeId) => enrollSurface(storeId, surfaceId))
    },

    evaluate(surfaceId: string) {
      return this.run(surfaceId, (storeId) => evaluatePrerequisites(storeId, surfaceId))
    },

    submit(surfaceId: string) {
      return this.run(surfaceId, (storeId) => submitSurface(storeId, surfaceId))
    },

    saveConfig(surfaceId: string, config: Record<string, string>) {
      return this.run(surfaceId, (storeId) => setSurfaceConfig(storeId, surfaceId, config))
    },

    /** Withdrawal returns no body, so the list is reloaded rather than
     * merged — and reloading also re-reads the store's other surfaces,
     * which withdrawal must have left untouched. */
    async withdraw(surfaceId: string) {
      const storeId = useStoresStore().currentStoreId
      if (!storeId) return
      this.busy = [...this.busy, surfaceId]
      this.error = null
      try {
        await withdrawSurface(storeId, surfaceId)
        await this.load()
      } catch (e: unknown) {
        this.error = e instanceof Error ? e.message : 'Failed to withdraw'
      } finally {
        this.busy = this.busy.filter((id) => id !== surfaceId)
      }
    },
  },
})
