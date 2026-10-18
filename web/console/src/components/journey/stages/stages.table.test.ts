// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Pins the stage table itself: every ucp_state has a row, each row is
// coherent, and the stage the Path is told to render is the one JourneyBar
// marks as current.
//
// The table is keyed on the store's own activation lifecycle, not on the
// journey. That is what makes the Path end at `active` — a genuine
// completion — rather than at "live on Google", which a merchant who never
// enrolls with Google could never reach.
//
// This replaced a registry of six components. The properties worth pinning
// didn't change — every enum value covered, each state rendering as itself —
// but "maps each state to a distinct component" is gone with the components,
// since distinctness now lives in the rows rather than in six files.

import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { routes } from '@/router'
import { i18n } from '@/i18n'
import StageLayout from './StageLayout.vue'
import { buildStages } from './stages'
import type { components } from '@/api/schema'

const STAGES = buildStages(i18n.global.t)

type UCPState = components['schemas']['ucp_state']
type Store = components['schemas']['store']

// Progression order. `suspended` has a row in the table (the Path still has
// to say something coherent when a store is there) but is not a step on the
// way anywhere, so it is excluded from the ordered assertions and from the
// JourneyBar.
const STAGE_ORDER: UCPState[] = [
  'draft',
  'connector_linked',
  'profile_published',
  'verified',
  'active',
]

const ALL_STATES: UCPState[] = [...STAGE_ORDER, 'suspended']

const store: Store = { store_id: 's1', name: 'Acme', platform: 'woocommerce' } as Store

const ctx = { storeName: 'Acme', productCount: 340, readinessScore: 74 }

beforeEach(() => {
  setActivePinia(createPinia())
})

describe('STAGES table', () => {
  it('has a row for every ucp_state enum value', () => {
    expect(Object.keys(STAGES).sort()).toEqual([...ALL_STATES].sort())
  })

  // The whole point of the refactor: no stage of the merchant's own
  // onboarding may be about a distribution partner. If a Google step comes
  // back into the Path, it comes back through this table — so this is
  // where it is caught.
  it('mentions no agent surface anywhere in its copy', () => {
    const forbidden = [/google/i, /merchant center/i, /\bfeed\b/i, /sandbox/i, /waitlist/i]
    for (const state of ALL_STATES) {
      const def = STAGES[state]
      const copy = [def.headline(ctx), def.sub, def.nothingNeeded, def.statsLabel ?? ''].join(' ')
      for (const pattern of forbidden) {
        expect(copy, `stage ${state} copy: ${copy}`).not.toMatch(pattern)
      }
    }
  })

  // `active` is the end, and it reads like one.
  it('ends at active', () => {
    expect(STAGE_ORDER[STAGE_ORDER.length - 1]).toBe('active')
    expect(STAGES.active.stats.length).toBeGreaterThan(0)
  })

  it('gives every stage its own headline', () => {
    const headlines = ALL_STATES.map((s) => STAGES[s].headline(ctx))
    expect(new Set(headlines).size).toBe(headlines.length)
  })

  it('gives every stage its own subhead', () => {
    const subs = ALL_STATES.map((s) => STAGES[s].sub)
    expect(new Set(subs).size).toBe(subs.length)
  })

  // The empty state is the one line a merchant sees when the Path has
  // nothing for them, so it has to be true of the stage they are actually
  // in — it used to be one hardcoded sentence for all six.
  it('gives every stage its own empty-state line', () => {
    const notes = ALL_STATES.map((s) => STAGES[s].nothingNeeded)
    expect(new Set(notes).size).toBe(notes.length)
  })

  it('labels every stat strip it shows, and only those', () => {
    for (const state of ALL_STATES) {
      const def = STAGES[state]
      expect(Boolean(def.statsLabel)).toBe(def.stats.length > 0)
    }
  })

  // Only the two headlines that interpolate catalog data may ask for it —
  // loading it anywhere else is an API call for a number nothing shows.
  it('flags catalog loading exactly where the headline needs it', () => {
    const needing = ALL_STATES.filter((s) => STAGES[s].needsCatalog)
    expect(needing).toEqual(['connector_linked', 'profile_published'])
  })
})

describe('StageLayout', () => {
  it.each(STAGE_ORDER)('%s marks JourneyBar current at the right index', async (state) => {
    const router = createRouter({ history: createMemoryHistory(), routes })
    await router.push('/path')
    await router.isReady()

    const w = mount(StageLayout, {
      global: { plugins: [router, i18n] },
      props: {
        stage: state,
        store,
        tasks: [],
        hasMore: false,
        loadingMore: false,
        openTask: null,
      },
    })

    const expectedIndex = STAGE_ORDER.indexOf(state)
    const steps = w.findAll('.jstep')
    expect(steps[expectedIndex].classes()).toContain('current')
  })
})
