// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// What the Path says at each stage of store activation.
//
// One row per ucp_state, keyed off the OpenAPI enum so a new stage is a type
// error here rather than a silent gap.
//
// This table used to be keyed on journey_state, and its last three rows were
// Google's: "Checklist complete — Google is testing your checkout", "Final
// go-live review", "You're live on Google". That made the Path a funnel
// toward one distribution partner, and left a merchant who did not want
// Google — or had not been approved by them — permanently short of the end.
//
// It now ends at `active`, which is a genuine completion: the store's
// endpoints are live, verified and monitored, and it is transactable by any
// agent that reads its profile. Whether it is also live on Google is a
// separate, optional enrollment, and it lives on the Surfaces page.
//
// Everything actionable is a task and arrives through the feed. What is in
// this table is the frame around it: the headline, and the handful of facts
// that aren't work for anyone.

import type { Component } from 'vue'
import type { components } from '@/api/schema'
import OrdersStat from '../stats/OrdersStat.vue'
import ConsistencyStat from '../stats/ConsistencyStat.vue'

type UCPState = components['schemas']['ucp_state']

/** Minimal shape of vue-i18n's `t` — avoids importing vue-i18n's composer
 * type just for this module's signature. */
type Translate = (key: string, named?: Record<string, unknown>) => string

/** Runtime values a stage's headline may need. */
export type StageContext = {
  storeName: string
  /** GET /catalog/readiness product_count, or the placeholder before it lands. */
  productCount: number | string
  /** GET /catalog/readiness overall, or the placeholder before it lands. */
  readinessScore: number | string
}

/** Shown in place of a number the catalog hasn't returned yet. */
export const PENDING = '…'

export type StageDefinition = {
  /** A function because several stages interpolate live data into the headline. */
  headline: (c: StageContext) => string
  sub: string
  /**
   * The feed's "Needs you now" empty state. Per stage because the reassuring
   * thing to say depends on what is happening.
   */
  nothingNeeded: string
  /** Facts worth showing that aren't work for anyone. Usually none. */
  stats: Component[]
  statsLabel?: string
  /** Whether the headline needs GET /catalog/readiness loaded. */
  needsCatalog?: boolean
}

/** Builds the stage table from vue-i18n (journey.stages.* in src/locales).
 * Callers pass their component's own `t` so this stays a plain function
 * usable from both <script setup> and non-component code — same pattern as
 * lib/ledger.ts's causeLabel. */
export function buildStages(t: Translate): Record<UCPState, StageDefinition> {
  return {
    draft: {
      headline: () => t('journey.stages.draft.headline'),
      sub: t('journey.stages.draft.sub'),
      nothingNeeded: t('journey.stages.draft.nothingNeeded'),
      stats: [],
    },

    connector_linked: {
      headline: (c) =>
        t('journey.stages.connector_linked.headline', { productCount: c.productCount }),
      sub: t('journey.stages.connector_linked.sub'),
      nothingNeeded: t('journey.stages.connector_linked.nothingNeeded'),
      stats: [],
      needsCatalog: true,
    },

    profile_published: {
      headline: (c) =>
        t('journey.stages.profile_published.headline', { readinessScore: c.readinessScore }),
      sub: t('journey.stages.profile_published.sub'),
      nothingNeeded: t('journey.stages.profile_published.nothingNeeded'),
      stats: [],
      needsCatalog: true,
    },

    verified: {
      headline: () => t('journey.stages.verified.headline'),
      sub: t('journey.stages.verified.sub'),
      nothingNeeded: t('journey.stages.verified.nothingNeeded'),
      stats: [],
    },

    // The end of the Path. Not "live on Google" — live, full stop: the
    // store's endpoints work and agents can transact against them. Surfaces
    // are additive from here, and have their own page.
    active: {
      headline: (c) => t('journey.stages.active.headline', { storeName: c.storeName }),
      sub: t('journey.stages.active.sub'),
      nothingNeeded: t('journey.stages.active.nothingNeeded'),
      statsLabel: t('journey.stages.active.statsLabel'),
      stats: [OrdersStat, ConsistencyStat],
    },

    // Not a step on the way anywhere — a condition. It gets a row because
    // the Path still has to say something coherent when a store is here.
    suspended: {
      headline: (c) => t('journey.stages.suspended.headline', { storeName: c.storeName }),
      sub: t('journey.stages.suspended.sub'),
      nothingNeeded: t('journey.stages.suspended.nothingNeeded'),
      stats: [],
    },
  }
}
