// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { i18n } from '@/i18n'
import ChannelKpiRow from './ChannelKpiRow.vue'
import type { ChannelSummary } from '@/api/channel'

function mountKpi(row: ChannelSummary | null, extra: Record<string, unknown> = {}) {
  return mount(ChannelKpiRow, {
    props: { row, loading: false, error: null, ...extra },
    global: { plugins: [i18n] },
  })
}

const baseRow: ChannelSummary = {
  surface: 'all',
  month: '2026-08',
  sessions: 200,
  sessions_confirmed: 200,
  sessions_inferred: 0,
  checkouts_completed: 80,
  revenue_minor: 1_600_000,
  currency: 'USD',
  currency_mixed: false,
  cart_rate: 0.6,
  checkout_rate: 0.7,
  completion_rate: 0.4,
  aov_minor: 20000,
  prior_period: {
    sessions_delta: 10,
    checkouts_completed_delta: -5,
    revenue_minor_delta: 50000,
    aov_minor_delta: 1500,
    completion_rate_delta: 0.05,
  },
}

describe('ChannelKpiRow — four KPIs from GET /channel/summary (R4)', () => {
  it('renders checkouts completed, revenue, AOV, and completion rate', () => {
    const w = mountKpi(baseRow)
    const cards = w.findAll('.kpi-card')
    expect(cards).toHaveLength(4)
    expect(cards[0].find('.kpi-value').text()).toBe('80')
    expect(cards[1].find('.kpi-value').text()).toBe('$16,000.00')
    expect(cards[2].find('.kpi-value').text()).toBe('$200.00')
    expect(cards[3].find('.kpi-value').text()).toBe('40.0%')
  })

  it('renders the server-provided delta verbatim for all four KPIs, never computing its own', () => {
    const w = mountKpi(baseRow)
    const cards = w.findAll('.kpi-card')
    // checkouts_completed_delta is -5 → "down"
    expect(cards[0].find('.delta').classes()).toContain('down')
    expect(cards[0].find('.delta').text()).toContain('5')
    // revenue_minor_delta is +50000 → "up"
    expect(cards[1].find('.delta').classes()).toContain('up')
    // aov_minor_delta is +1500 → "up", rendered as money
    expect(cards[2].find('.delta').classes()).toContain('up')
    expect(cards[2].find('.delta').text()).toContain('$15.00')
    // completion_rate_delta is +0.05 → "up", rendered as percentage points
    expect(cards[3].find('.delta').classes()).toContain('up')
    expect(cards[3].find('.delta').text()).toContain('5.0pp')
  })

  it('omits a delta only when the server omits prior_period entirely', () => {
    const w = mountKpi({ ...baseRow, prior_period: undefined })
    const cards = w.findAll('.kpi-card')
    for (const card of cards) {
      expect(card.find('.delta').exists()).toBe(false)
    }
  })
})

describe('ChannelKpiRow — currency handling (R5)', () => {
  it('falls back to the shared formatter\'s placeholder when the row has no revenue-bearing day yet', () => {
    const w = mountKpi({ ...baseRow, currency: null, currency_mixed: false, revenue_minor: 0, aov_minor: 0 })
    const cards = w.findAll('.kpi-card')
    expect(cards[1].find('.kpi-value').text()).toBe('—')
    expect(cards[2].find('.kpi-value').text()).toBe('—')
  })

  it('shows a mixed-currency notice instead of a number when currency_mixed is true, never picking one currency silently', () => {
    const w = mountKpi({ ...baseRow, currency: null, currency_mixed: true })
    const cards = w.findAll('.kpi-card')
    expect(cards[1].find('.mixed-note').exists()).toBe(true)
    expect(cards[2].find('.mixed-note').exists()).toBe(true)
    expect(cards[1].text()).not.toMatch(/\$16,000\.00/)
    // No delta shown alongside a mixed-currency figure either.
    expect(cards[1].find('.delta').exists()).toBe(false)
    expect(cards[2].find('.delta').exists()).toBe(false)
  })
})

describe('ChannelKpiRow — loading/error/empty (R15)', () => {
  it('shows four skeletons while loading', () => {
    const w = mountKpi(null, { loading: true })
    expect(w.findAll('.skel-grid > *')).toHaveLength(4)
  })

  it('shows an empty note when there is no row for the selected surface', () => {
    const w = mountKpi(null)
    expect(w.find('.empty-note').exists()).toBe(true)
  })

  it('shows the error banner with a working retry', async () => {
    const w = mountKpi(null, { error: 'down' })
    await w.find('.err .btn').trigger('click')
    expect(w.emitted('retry')).toBeTruthy()
  })
})
