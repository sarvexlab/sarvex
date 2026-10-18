// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { i18n } from '@/i18n'
import ChannelFunnel from './ChannelFunnel.vue'
import type { ChannelFunnel as ChannelFunnelType } from '@/api/channel'

function mountFunnel(funnel: ChannelFunnelType | null, extra: Record<string, unknown> = {}) {
  return mount(ChannelFunnel, {
    props: { funnel, loading: false, error: null, ...extra },
    global: { plugins: [i18n] },
  })
}

const baseFunnel: ChannelFunnelType = {
  from: '2026-08-01',
  to: '2026-08-06',
  surface: 'all',
  sessions: 100,
  sessions_inferred: 0,
  sessions_reached_cart: 60,
  sessions_reached_checkout: 40,
  checkouts_initiated: 100,
  checkouts_completed: 30,
  rates: { cart_rate: 0.6, checkout_rate: 0.67, completion_rate: 0.3, aov_minor: 5000 },
}

describe('ChannelFunnel — stages and rates (R6)', () => {
  it('renders the four stage counts from the API', () => {
    const w = mountFunnel(baseFunnel)
    const stages = w.findAll('.stage')
    expect(stages).toHaveLength(4)
    expect(stages.map((s) => s.find('.stage-count').text())).toEqual(['100', '60', '40', '30'])
  })

  it('derives stage-to-stage rates from the raw counts rather than reusing completion_rate for the last segment', () => {
    const w = mountFunnel(baseFunnel)
    const rateValues = w.findAll('.rate-value').map((n) => n.text())
    // sessions->cart 60/100=60.0%, cart->checkout 40/60=66.7%,
    // checkout->completed 30/40=75.0% — the last is NOT completion_rate
    // (30/100=30.0%), which is the whole-funnel figure, not this segment.
    expect(rateValues).toEqual(['60.0%', '66.7%', '75.0%'])
  })
})

describe('ChannelFunnel — inferred-attribution qualifier (R7/A3)', () => {
  it('shows the qualifier when sessions_inferred > 0', () => {
    const w = mountFunnel({ ...baseFunnel, sessions_inferred: 12 })
    const q = w.find('.qualifier')
    expect(q.exists()).toBe(true)
    expect(q.attributes('title')).toBeTruthy()
  })

  it('tooltip cites the real sessions_confirmed/sessions_inferred split from the API, not a generic caveat', () => {
    const w = mountFunnel({ ...baseFunnel, sessions_confirmed: 88, sessions_inferred: 12 })
    const title = w.find('.qualifier').attributes('title')
    expect(title).toContain('88')
    expect(title).toContain('12')
  })

  it('shows no qualifier when sessions_inferred is 0 (fully confirmed)', () => {
    const w = mountFunnel({ ...baseFunnel, sessions_inferred: 0 })
    expect(w.find('.qualifier').exists()).toBe(false)
  })
})

describe('ChannelFunnel — no-sessionization-data degradation (R8/A3)', () => {
  it('renders a short explanation instead of an empty/zeroed funnel when sessions is 0', () => {
    const w = mountFunnel({ ...baseFunnel, sessions: 0, sessions_reached_cart: 0, sessions_reached_checkout: 0, checkouts_completed: 0 })
    expect(w.find('.stages').exists()).toBe(false)
    expect(w.find('.no-data-note').exists()).toBe(true)
    expect(w.find('.qualifier').exists()).toBe(false)
  })

  it('renders normal stages when there is data', () => {
    const w = mountFunnel(baseFunnel)
    expect(w.find('.no-data-note').exists()).toBe(false)
    expect(w.find('.stages').exists()).toBe(true)
  })
})

describe('ChannelFunnel — loading/error (R15)', () => {
  it('shows a skeleton while loading and no stages', () => {
    const w = mountFunnel(null, { loading: true })
    expect(w.find('.skel-block').exists()).toBe(true)
    expect(w.find('.stages').exists()).toBe(false)
  })

  it('shows the error banner with a retry that re-emits', async () => {
    const w = mountFunnel(null, { error: 'network down' })
    const banner = w.find('.err')
    expect(banner.exists()).toBe(true)
    await banner.find('.btn').trigger('click')
    expect(w.emitted('retry')).toBeTruthy()
  })
})
