// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest'
import { i18n } from '@/i18n'
import { buildPlans, planName } from './plans'

const t = i18n.global.t

describe('planName', () => {
  it('resolves each known plan id to its display name', () => {
    expect(planName(t, 'self_serve')).toBe('Self-serve')
    expect(planName(t, 'concierge')).toBe('Concierge')
    expect(planName(t, 'scale')).toBe('Scale')
  })

  it('falls back to an em dash for unknown or missing ids', () => {
    expect(planName(t, undefined)).toBe('—')
    expect(planName(t, null)).toBe('—')
    expect(planName(t, 'enterprise' as never)).toBe('—')
  })

  it('has exactly one entry per plan, all with non-empty display fields', () => {
    const plans = buildPlans(t)
    expect(plans).toHaveLength(3)
    for (const p of plans) {
      expect(p.name).toBeTruthy()
      expect(p.price).toBeTruthy()
      expect(p.cadence).toBeTruthy()
      expect(p.blurb).toBeTruthy()
    }
  })
})
