// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Surfaces — being listed on a particular shopping surface, Google included —
// are a later, additive feature. Until they ship, no merchant-facing copy
// should mention one: a merchant who has never seen a Surfaces page cannot
// act on a sentence about it, and naming Google in the shell or on the store
// page implies Sarvex is a Google integration rather than a UCP one.
//
// The Path already guards this per-stage (stages.table.test.ts,
// stages.copy.test.ts). This covers the two surfaces those tests don't reach:
// the shell's own chrome and the store page.
//
// Deliberately not covered here, because each is about something real and
// present rather than a distribution surface:
//   - auth.continueWithGoogle — Kinde's social login button
//   - analytics agent-platform labels (google_ai_mode, google_gemini) — the
//     names of agents actually observed in telemetry
//   - "All surfaces" analytics filters — a data dimension, not a feature page
//   - the Google Pay handler's setup note — the merchant's own console

import { describe, it, expect } from 'vitest'
import en from './en'

const GOOGLE = /google/i
const SURFACE = /surface/i

/** Every string value in a nested copy object, ignoring keys. */
function strings(node: unknown): string[] {
  if (typeof node === 'string') return [node]
  if (node && typeof node === 'object') return Object.values(node).flatMap(strings)
  return []
}

describe('shell chrome', () => {
  it('names the catalog page neutrally', () => {
    expect(en.shell.nav.catalog).toBe('Catalog')
  })

  it('mentions no distribution surface anywhere in the nav', () => {
    for (const s of strings(en.shell.nav)) {
      expect(s).not.toMatch(GOOGLE)
      expect(s).not.toMatch(SURFACE)
    }
  })
})

describe('the store page', () => {
  it('describes what agents see without naming a surface', () => {
    for (const s of strings(en.store)) {
      expect(s, `store copy must not name Google: ${s}`).not.toMatch(GOOGLE)
    }
  })

  it('titles the product drawer mapping section neutrally', () => {
    expect(en.store.drawer.mappingTitle).toBe('How your attributes map')
  })

  // The targets are merchandising attributes (brand, gtin, size...), not
  // fields of the UCP protocol — the spec has no brand/mpn/condition, carries
  // gtin as a barcode type and size/color/material as product options. Calling
  // them protocol fields, as the API's old `ucp_field` name did, asserts a
  // correspondence that does not exist.
  it('does not claim the mapping targets are protocol fields', () => {
    expect(en.store.drawer.mappingNote).not.toMatch(/\bucp\b/i)
    expect(en.store.drawer.mappingNote).not.toMatch(/protocol|spec\b/i)
    expect(en.store.drawer.mappingNote).toMatch(/attribute/i)
  })
})

describe('the Path', () => {
  it('does not send an active store to a Surfaces page that does not exist yet', () => {
    expect(en.journey.stages.active.sub).not.toMatch(SURFACE)
    expect(en.journey.stages.active.sub).not.toMatch(GOOGLE)
  })

  it('does not reassure a suspended store about surface enrollments', () => {
    expect(en.journey.stages.suspended.sub).not.toMatch(SURFACE)
  })
})

describe('the product score explanation', () => {
  it('explains agent-ready versus the score without naming a surface', () => {
    for (const s of [en.store.products.readyNote, en.store.drawer.breakdownTitle, en.store.drawer.breakdownNote]) {
      expect(s).not.toMatch(GOOGLE)
      expect(s).not.toMatch(SURFACE)
    }
    expect(en.store.products.readyNote).toMatch(/blocks? checkout/i)
  })
})
