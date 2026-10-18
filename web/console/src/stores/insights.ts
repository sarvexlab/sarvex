// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Agent Behavior Insights state (R17). Three independent sections —
// capability gaps, negotiation, product demand — each with its own
// loading/loaded/error triple and its own load action, mirroring the
// Channel Dashboard's store shape exactly (R15: a failing section must
// never blank another). Attribute gaps has no backend endpoint at all (see
// api/insights.ts's top-of-file note) so it has no state here —
// AttributeGapsSection.vue renders a static explanatory panel, not a
// store-backed one.

import { defineStore } from 'pinia'
import {
  listCapabilityGaps,
  listProductDemand,
  getNegotiationInsights,
  type CapabilityGap,
  type ProductDemand,
  type NegotiationInsights,
  type InsightsRangeFilter,
} from '@/api/insights'

export const useInsightsStore = defineStore('insights', {
  state: () => ({
    capabilityGaps: [] as CapabilityGap[],
    capabilityGapsLoading: false,
    capabilityGapsLoaded: false,
    capabilityGapsError: null as string | null,

    negotiation: null as NegotiationInsights | null,
    negotiationLoading: false,
    negotiationLoaded: false,
    negotiationError: null as string | null,

    productDemand: [] as ProductDemand[],
    productDemandLoading: false,
    productDemandLoaded: false,
    productDemandError: null as string | null,
    // Accumulated across every product-demand load, filtered or not (R2 —
    // the surface filter's option list must come from real API data, never
    // a hardcoded enum). A load scoped to one surface returns
    // surface_breakdown containing only that surface, so this only ever
    // grows as more surfaces are observed rather than being derived from
    // the current (possibly filtered) response alone.
    productDemandSurfacesSeen: new Set<string>() as Set<string>,
  }),

  getters: {
    productDemandSurfaces: (s): string[] => [...s.productDemandSurfacesSeen].sort(),
  },

  actions: {
    async loadCapabilityGaps(args: { month?: string; store_id?: string }) {
      this.capabilityGapsLoading = true
      this.capabilityGapsError = null
      try {
        // Server order (impact_minor desc, request_count desc tiebreak) is
        // the product invariant (R4/A2) — no client-side sort applied here
        // or anywhere downstream.
        this.capabilityGaps = await listCapabilityGaps(args)
        this.capabilityGapsLoaded = true
      } catch (e: unknown) {
        this.capabilityGapsError = e instanceof Error ? e.message : 'Failed to load capability gaps'
      } finally {
        this.capabilityGapsLoading = false
      }
    },

    async loadNegotiation(args: { from?: string; to?: string }) {
      this.negotiationLoading = true
      this.negotiationError = null
      try {
        this.negotiation = await getNegotiationInsights(args)
        this.negotiationLoaded = true
      } catch (e: unknown) {
        this.negotiationError = e instanceof Error ? e.message : 'Failed to load negotiation insights'
      } finally {
        this.negotiationLoading = false
      }
    },

    async loadProductDemand(args: InsightsRangeFilter) {
      this.productDemandLoading = true
      this.productDemandError = null
      try {
        const rows = await listProductDemand(args)
        this.productDemand = rows
        this.productDemandLoaded = true
        const seen = new Set(this.productDemandSurfacesSeen)
        for (const row of rows) {
          for (const surface of Object.keys(row.surface_breakdown ?? {})) seen.add(surface)
        }
        this.productDemandSurfacesSeen = seen
      } catch (e: unknown) {
        this.productDemandError = e instanceof Error ? e.message : 'Failed to load product demand'
      } finally {
        this.productDemandLoading = false
      }
    },
  },
})
