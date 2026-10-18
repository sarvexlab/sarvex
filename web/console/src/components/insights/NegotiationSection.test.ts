// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { i18n } from '@/i18n'
import NegotiationSection from './NegotiationSection.vue'
import type { NegotiationInsights, NegotiationFailure } from '@/api/insights'

function mountSection(negotiation: NegotiationInsights | null, extra: Record<string, unknown> = {}) {
  return mount(NegotiationSection, {
    props: { negotiation, loading: false, error: null, ...extra },
    global: { plugins: [i18n] },
  })
}

const base: NegotiationInsights = {
  attempts: 100,
  successes: 70,
  failures: [
    { reason: 'capabilities_incompatible', count: 20, missing_capability_counts: { 'dev.ucp.shopping.fulfillment': 20 } },
    { reason: 'version_unsupported', count: 10 },
  ],
  payment_handler_mismatch: {
    occurrences: 15,
    offered_handler_counts: { paypal: 12, venmo: 3 },
    merchant_declared_handlers: ['stripe'],
  },
}

describe('NegotiationSection — attempts/successes/failures (R9)', () => {
  it('renders attempts and successes', () => {
    const w = mountSection(base)
    expect(w.text()).toContain('100')
    expect(w.text()).toContain('70')
  })

  it('renders each failure reason from the API, via its own label', () => {
    const w = mountSection(base)
    expect(w.text()).toContain('Capabilities incompatible')
    expect(w.text()).toContain('Unsupported protocol version')
  })

  it('a fixture reason unknown at build time still renders, humanized (A4)', () => {
    // The enum is closed on the wire today, but the API's own doc comment
    // derives it from a Go function this frontend must not assume it can
    // enumerate forever — a reason string outside the current enum still
    // has to render (A4), not be dropped.
    const unknownFailure = { reason: 'brand_new_failure_reason', count: 7 } as unknown as NegotiationFailure
    const w = mountSection({ ...base, failures: [unknownFailure] })
    expect(w.text()).toContain('Brand New Failure Reason')
  })

  it('renders missing_capability_counts only for the capability-delta reason', () => {
    const w = mountSection(base)
    expect(w.find('.missing-caps').exists()).toBe(true)
    expect(w.text()).toContain('dev.ucp.shopping.fulfillment: 20')
  })
})

describe('NegotiationSection — payment-handler mismatch (R10/A4)', () => {
  it('renders its own dedicated card, not folded into the failures list', () => {
    const w = mountSection(base)
    const card = w.find('.handler-mismatch')
    expect(card.exists()).toBe(true)
    expect(card.text()).toContain('15')
    expect(card.text()).toContain('paypal')
    expect(card.text()).toContain('stripe')
  })
})

describe('NegotiationSection — empty/loading/error (R15)', () => {
  it('shows an empty note when there are no attempts', () => {
    const w = mountSection({ attempts: 0, successes: 0, failures: [], payment_handler_mismatch: { occurrences: 0, offered_handler_counts: {}, merchant_declared_handlers: [] } })
    expect(w.text()).toContain('No negotiation attempts')
  })

  it('shows a skeleton while loading', () => {
    const w = mountSection(null, { loading: true })
    expect(w.find('.skel-block').exists()).toBe(true)
  })

  it('shows the error banner with a retry that re-emits', async () => {
    const w = mountSection(null, { error: 'network down' })
    const banner = w.find('.err')
    expect(banner.exists()).toBe(true)
    await banner.find('.btn').trigger('click')
    expect(w.emitted('retry')).toBeTruthy()
  })
})
