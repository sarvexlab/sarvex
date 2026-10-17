// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { i18n } from '@/i18n'
import ChannelComparison from './ChannelComparison.vue'
import type { ChannelComparison as ChannelComparisonType } from '@/api/channel'

function mountComparison(comparison: ChannelComparisonType | null, extra: Record<string, unknown> = {}) {
  return mount(ChannelComparison, {
    props: { comparison, loading: false, error: null, ...extra },
    global: { plugins: [i18n] },
  })
}

const agentic = (over: Partial<NonNullable<ChannelComparisonType['agentic']>> = {}) => ({
  revenue_minor: 500000,
  checkouts_completed: 40,
  aov_minor: 12500,
  currency: 'USD',
  currency_mixed: false,
  ...over,
})
const webBaseline = (over: Partial<NonNullable<ChannelComparisonType['web_baseline']>> = {}) => ({
  orders: 30,
  revenue_minor: 400000,
  aov_minor: 13333,
  currency: 'USD',
  currency_mixed: false,
  confidence: 'connector_reported' as const,
  ...over,
})

describe('ChannelComparison — unavailable metrics render a reason, never a placeholder (R11/A4)', () => {
  it('renders the server reason text instead of a number or dash when revenue is unavailable', () => {
    const w = mountComparison({
      from: '2026-08-01',
      to: '2026-08-06',
      agentic: agentic(),
      web_baseline: webBaseline({ confidence: 'unavailable' }),
      revenue: { available: false, confidence: 'unavailable', reason: 'no baseline data synced for this period' },
      aov: { available: false, confidence: 'unavailable', reason: 'no baseline data synced for this period' },
      agentic_share_of_total: { available: false, confidence: 'unavailable', reason: 'no baseline data synced for this period' },
    })
    const text = w.text()
    expect(text).toContain('no baseline data synced for this period')
    // No bare "$0.00"/"—" masquerading as a real figure for the unavailable metric.
    expect(w.findAll('.metric')[0].text()).not.toMatch(/\$0\.00/)
    expect(w.find('.unavailable-tag').exists()).toBe(true)
  })

  it('renders real numbers for a metric the server marks available at full confidence, with no confidence badge', () => {
    const w = mountComparison({
      from: '2026-08-01',
      to: '2026-08-06',
      agentic: agentic(),
      web_baseline: webBaseline(),
      revenue: { available: true, confidence: 'connector_reported', agentic_minor: 500000, baseline_minor: 400000, delta_minor: 100000 },
      aov: { available: false, confidence: 'unavailable', reason: 'no baseline data synced for this period' },
      agentic_share_of_total: { available: false, confidence: 'unavailable', reason: 'no baseline data synced for this period' },
    })
    const revenueMetric = w.findAll('.metric')[0]
    expect(revenueMetric.text()).toContain('$5,000.00')
    expect(revenueMetric.text()).toContain('$4,000.00')
    expect(revenueMetric.find('.unavailable').exists()).toBe(false)
    expect(revenueMetric.find('.confidence-badge').exists()).toBe(false)
  })

  it('renders real numbers WITH a confidence indicator at confidence=partial — never a substituted estimate (R11)', () => {
    const w = mountComparison({
      from: '2026-08-01',
      to: '2026-08-06',
      agentic: agentic(),
      web_baseline: webBaseline({ confidence: 'partial' }),
      revenue: {
        available: true,
        confidence: 'partial',
        reason: '3 of 6 days in this period have incomplete or missing web-baseline data — figures below are computed from what was reported, not estimated for the gap',
        agentic_minor: 500000,
        baseline_minor: 200000,
        delta_minor: 300000,
      },
      aov: { available: false, confidence: 'unavailable', reason: 'no baseline data synced for this period' },
      agentic_share_of_total: { available: false, confidence: 'unavailable', reason: 'no baseline data synced for this period' },
    })
    const revenueMetric = w.findAll('.metric')[0]
    expect(revenueMetric.find('.confidence-badge').exists()).toBe(true)
    expect(revenueMetric.text()).toContain('$5,000.00')
    expect(revenueMetric.text()).toContain('$2,000.00')
    expect(revenueMetric.text()).toContain('incomplete or missing web-baseline data')
    expect(revenueMetric.find('.unavailable').exists()).toBe(false)
  })

  it('renders the share exactly as the API computed it, never recomputed from agentic/baseline (R13)', () => {
    // agentic revenue is 500000 and baseline is 400000, which would make a
    // naive agentic/(agentic+baseline) = 0.5556 (55.6%) — but the server's
    // own share (which divides by its own connector-reported total, per
    // R13) is asserted here instead, to prove the component isn't
    // recomputing it.
    const w = mountComparison({
      from: '2026-08-01',
      to: '2026-08-06',
      agentic: agentic(),
      web_baseline: webBaseline(),
      revenue: { available: true, confidence: 'connector_reported', agentic_minor: 500000, baseline_minor: 400000, delta_minor: 100000 },
      aov: { available: true, confidence: 'connector_reported', agentic_minor: 12500, baseline_minor: 11000, delta_minor: 1500 },
      agentic_share_of_total: { available: true, confidence: 'connector_reported', share: 0.1234 },
    })
    expect(w.text()).toContain('12.3%')
    expect(w.text()).not.toContain('55.6%')
  })
})

describe('ChannelComparison — per-side currency (R5)', () => {
  it('shows a mixed-currency notice for whichever side is mixed, independently', () => {
    const w = mountComparison({
      from: '2026-08-01',
      to: '2026-08-06',
      agentic: agentic({ currency: null, currency_mixed: true }),
      web_baseline: webBaseline(),
      revenue: { available: true, confidence: 'connector_reported', agentic_minor: 500000, baseline_minor: 400000, delta_minor: 100000 },
      aov: { available: false, confidence: 'unavailable', reason: 'no baseline data synced for this period' },
      agentic_share_of_total: { available: false, confidence: 'unavailable', reason: 'no baseline data synced for this period' },
    })
    const revenueMetric = w.findAll('.metric')[0]
    expect(revenueMetric.text()).toContain('$4,000.00') // web side unaffected
    expect(revenueMetric.find('.mixed-note').exists()).toBe(true) // agentic side
    expect(revenueMetric.text()).not.toMatch(/\$5,000\.00/)
  })
})

describe('ChannelComparison — no session-count comparison anywhere (R12/A4)', () => {
  it('never renders a session count in the comparison tree', () => {
    const w = mountComparison({
      from: '2026-08-01',
      to: '2026-08-06',
      agentic: agentic(),
      web_baseline: webBaseline(),
      revenue: { available: true, confidence: 'connector_reported', agentic_minor: 500000, baseline_minor: 400000, delta_minor: 100000 },
      aov: { available: true, confidence: 'connector_reported', agentic_minor: 12500, baseline_minor: 11000, delta_minor: 1500 },
      agentic_share_of_total: { available: true, confidence: 'connector_reported', share: 0.55 },
    })
    expect(w.text().toLowerCase()).not.toContain('session')
  })
})

describe('ChannelComparison — loading/error (R15)', () => {
  it('shows a skeleton while loading', () => {
    const w = mountComparison(null, { loading: true })
    expect(w.find('.skel-block').exists()).toBe(true)
  })

  it('shows the error banner with a working retry', async () => {
    const w = mountComparison(null, { error: 'down' })
    await w.find('.err .btn').trigger('click')
    expect(w.emitted('retry')).toBeTruthy()
  })
})
