// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { i18n } from '@/i18n'
import CapabilityGapsSection from './CapabilityGapsSection.vue'
import type { CapabilityGap } from '@/api/insights'

function mountSection(gaps: CapabilityGap[], extra: Record<string, unknown> = {}) {
  return mount(CapabilityGapsSection, {
    props: { gaps, loading: false, error: null, ...extra },
    global: { plugins: [i18n] },
  })
}

function gap(overrides: Partial<CapabilityGap> = {}): CapabilityGap {
  return {
    capability_id: 'dev.ucp.shopping.fulfillment',
    request_count: 40,
    distinct_agent_count: 5,
    surface_breakdown: { google_ai_mode: 40 },
    estimated_impact_minor: 0,
    impact_confidence: 'unmeasurable',
    merchant_supported: false,
    remediation_available: false,
    recommended_action: 'Declare shipping options in your UCP profile.',
    ...overrides,
  }
}

describe('CapabilityGapsSection — server order (R4/A2)', () => {
  it('renders gaps in the exact order given, applying no client sort', () => {
    const gaps = [
      gap({ capability_id: 'dev.ucp.shopping.discount', request_count: 5 }),
      gap({ capability_id: 'dev.ucp.shopping.fulfillment', request_count: 500 }),
      gap({ capability_id: 'dev.ucp.shopping.cart', request_count: 50 }),
    ]
    const w = mountSection(gaps)
    const ids = w.findAll('.gap-id').map((n) => n.text())
    expect(ids).toEqual(['dev.ucp.shopping.discount', 'dev.ucp.shopping.fulfillment', 'dev.ucp.shopping.cart'])
  })
})

describe('CapabilityGapsSection — confidence on every impact figure (R6/A2)', () => {
  it('unmeasurable renders an explanation and never a money figure', () => {
    const w = mountSection([gap({ impact_confidence: 'unmeasurable', estimated_impact_minor: 0 })])
    expect(w.text()).not.toMatch(/\$0(\.00)?\b/)
    expect(w.find('.impact-unmeasurable').exists()).toBe(true)
    expect(w.find('.confidence-note').text().length).toBeGreaterThan(0)
    expect(w.text()).toContain('Not measurable')
  })

  it('partial renders its own indicator, distinct from unmeasurable', () => {
    const w = mountSection([gap({ impact_confidence: 'partial', estimated_impact_minor: 12000 })])
    expect(w.find('.impact-unmeasurable').exists()).toBe(false)
    expect(w.find('.impact-value').exists()).toBe(true)
    const chip = w.find('.confidence-chip')
    expect(chip.text()).toContain('Partial')
  })

  it('measured renders plainly with a money figure and a measured indicator', () => {
    const w = mountSection([gap({ impact_confidence: 'measured', estimated_impact_minor: 5000 })], { currency: 'USD' })
    expect(w.find('.impact-value').text()).toContain('$50.00')
    expect(w.find('.confidence-chip').text()).toContain('Measured')
  })

  it('never renders $0 for any unmeasurable row, regardless of how many rows are shown', () => {
    const gaps = [
      gap({ capability_id: 'a', impact_confidence: 'unmeasurable', estimated_impact_minor: 0 }),
      gap({ capability_id: 'b', impact_confidence: 'unmeasurable', estimated_impact_minor: 0 }),
    ]
    const w = mountSection(gaps)
    expect(w.text()).not.toMatch(/\$0(\.00)?\b/)
  })
})

describe('CapabilityGapsSection — remediation split (R7/A2)', () => {
  it('renders a distinct treatment and "we can fix this" copy when remediation_available', () => {
    const w = mountSection([gap({ remediation_available: true })])
    expect(w.find('.gap-card').classes()).toContain('remediable')
    expect(w.find('.remediation-chip').exists()).toBe(true)
    expect(w.text()).toContain('We can fix this for you')
  })

  it('renders merchant-action copy, no badge, when remediation is not available', () => {
    const w = mountSection([gap({ remediation_available: false })])
    expect(w.find('.gap-card').classes()).not.toContain('remediable')
    expect(w.find('.remediation-chip').exists()).toBe(false)
    expect(w.text()).toContain('Requires your action')
  })
})

describe('CapabilityGapsSection — capability label mapping (R5/R16/A3)', () => {
  it('renders a human label for a mapped capability id, with the raw id as secondary detail', () => {
    const w = mountSection([gap({ capability_id: 'dev.ucp.shopping.fulfillment' })])
    expect(w.find('.gap-title').text()).toBe('Shipping & fulfillment options')
    expect(w.find('.gap-id').text()).toBe('dev.ucp.shopping.fulfillment')
  })

  it('falls back to the raw id, without error, for an unmapped capability id', () => {
    const w = mountSection([gap({ capability_id: 'dev.ucp.shopping.brand_new_thing' })])
    expect(w.find('.gap-title').text()).toBe('dev.ucp.shopping.brand_new_thing')
  })
})

describe('CapabilityGapsSection — empty state (R8)', () => {
  it('frames an empty result positively', () => {
    const w = mountSection([])
    expect(w.text()).toContain("aren't requesting capabilities you lack")
  })
})

describe('CapabilityGapsSection — loading/error (R3/R15)', () => {
  it('shows a skeleton while loading and no gap cards', () => {
    const w = mountSection([], { loading: true })
    expect(w.find('.skel-block').exists()).toBe(true)
    expect(w.find('.gap-card').exists()).toBe(false)
  })

  it('shows the error banner with a retry that re-emits', async () => {
    const w = mountSection([], { error: 'network down' })
    const banner = w.find('.err')
    expect(banner.exists()).toBe(true)
    await banner.find('.btn').trigger('click')
    expect(w.emitted('retry')).toBeTruthy()
  })
})
