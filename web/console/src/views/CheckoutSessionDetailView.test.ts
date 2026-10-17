// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Checkout Escalation Console — session detail. Covers:
//   - Diagnosis renders above the timeline (R6)
//   - Evidence seq highlighting + jump-to (R8), including paging further
//     back to find a not-yet-loaded evidence entry
//   - Timeline pagination against a mocked cursor sequence, including the
//     terminal (no more) page (R7)
//   - Escalation "never returned" state (R9)
//   - No retry/re-execute control anywhere (R10/A3)

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { routes } from '@/router'
import { i18n } from '@/i18n'
import CheckoutSessionDetailView from './CheckoutSessionDetailView.vue'
import type { CheckoutSession, CheckoutSessionTimelineEntry } from '@/api/checkoutSessions'

vi.mock('@/api/checkoutSessions', () => ({
  listCheckoutSessions: vi.fn(),
  listLiveCheckoutSessions: vi.fn(),
  getCheckoutSession: vi.fn(),
  getCheckoutSessionTimeline: vi.fn(),
}))
vi.mock('@/api/stores', () => ({ listStores: vi.fn(async () => []) }))

import { getCheckoutSession, getCheckoutSessionTimeline } from '@/api/checkoutSessions'

const STALLED_SESSION: CheckoutSession = {
  checkout_id: 'co-stalled-1',
  store_id: 'str-1',
  surface: 'ucp_checkout',
  agent_platform: 'gemini-app',
  state: 'escalated',
  current_stage: 'payment_handoff',
  started_at: '2026-07-16T08:00:00Z',
  last_activity_at: '2026-07-16T08:22:00Z',
  terminal_at: null,
  cart_summary: { currency: 'AUD', total_minor: 12800, item_count: 2 },
  escalation: {
    reason: 'Buyer handed off to hosted payment page.',
    escalated_at: '2026-07-16T08:22:00Z',
    continue_url_issued: true,
    returned_at: null,
    resolved: false,
  },
  failure: null,
  diagnosis: {
    stage_reached: 'payment_handoff',
    cause: 'escalation_stalled',
    plain_language_summary: 'The buyer never came back from the hosted payment page.',
    recommended_action: 'No action needed yet.',
    evidence: [1, 5],
  },
  event_count: 5,
  updated_at: '2026-07-16T08:22:00Z',
}

function entry(seq: number, over: Partial<CheckoutSessionTimelineEntry> = {}): CheckoutSessionTimelineEntry {
  return {
    seq,
    occurred_at: `2026-07-16T08:0${seq}:00Z`,
    event_type: `event.${seq}`,
    stage: 'payment_handoff',
    payload: { note: `entry ${seq}` },
    ...over,
  }
}

let mountedWrappers: Array<ReturnType<typeof mount>> = []

async function mountDetail(checkoutId = 'co-stalled-1'): Promise<{ wrapper: ReturnType<typeof mount>; router: Router }> {
  setActivePinia(createPinia())
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push(`/checkout-sessions/${checkoutId}`)
  await router.isReady()
  const wrapper = mount(CheckoutSessionDetailView, {
    global: { plugins: [router, i18n] },
    props: { checkoutId },
    // Attached to document.body: jumpToSeq (R8) scrolls via
    // document.getElementById, which can't see a detached mount tree.
    attachTo: document.body,
  })
  mountedWrappers.push(wrapper)
  await flushPromises()
  return { wrapper, router }
}

beforeEach(() => {
  mountedWrappers = []
  vi.mocked(getCheckoutSession).mockReset()
  vi.mocked(getCheckoutSessionTimeline).mockReset()
})

afterEach(() => {
  for (const w of mountedWrappers) {
    try {
      w.unmount()
    } catch {
      // already unmounted
    }
  }
})

describe('CheckoutSessionDetailView — diagnosis placement (R6)', () => {
  it('renders the diagnosis block above the timeline section', async () => {
    vi.mocked(getCheckoutSession).mockResolvedValue(STALLED_SESSION)
    vi.mocked(getCheckoutSessionTimeline).mockResolvedValue({
      data: [entry(1), entry(2)],
      page: { cursor: null, has_more: false },
    })
    const { wrapper } = await mountDetail()

    const html = wrapper.html()
    const diagIndex = html.indexOf('What happened')
    const timelineIndex = html.indexOf('Timeline')
    expect(diagIndex).toBeGreaterThan(-1)
    expect(timelineIndex).toBeGreaterThan(-1)
    expect(diagIndex).toBeLessThan(timelineIndex)
    expect(wrapper.text()).toContain('The buyer never came back from the hosted payment page.')
  })
})

describe('CheckoutSessionDetailView — evidence highlighting + jump-to (R8)', () => {
  it('highlights timeline entries whose seq is in diagnosis.evidence', async () => {
    vi.mocked(getCheckoutSession).mockResolvedValue(STALLED_SESSION)
    vi.mocked(getCheckoutSessionTimeline).mockResolvedValue({
      data: [entry(1), entry(2), entry(5)],
      page: { cursor: null, has_more: false },
    })
    const { wrapper } = await mountDetail()

    const evidenceRows = wrapper.findAll('.entry.evidence')
    expect(evidenceRows).toHaveLength(2)
    expect(wrapper.find('#timeline-entry-1').classes()).toContain('evidence')
    expect(wrapper.find('#timeline-entry-5').classes()).toContain('evidence')
    expect(wrapper.find('#timeline-entry-2').classes()).not.toContain('evidence')
  })

  it('clicking an evidence chip scrolls the matching entry into view, paging further back if needed', async () => {
    vi.mocked(getCheckoutSession).mockResolvedValue(STALLED_SESSION)
    // First page only has seq 5; seq 1 (also evidence) requires a further page.
    vi.mocked(getCheckoutSessionTimeline)
      .mockResolvedValueOnce({ data: [entry(5)], page: { cursor: '5', has_more: true } })
      .mockResolvedValueOnce({ data: [entry(1)], page: { cursor: null, has_more: false } })

    const { wrapper } = await mountDetail()
    expect(wrapper.find('#timeline-entry-1').exists()).toBe(false)

    const scrollSpy = vi.fn()
    // jsdom has no real layout; stub scrollIntoView on the element once it exists.
    const evidenceChips = wrapper.findAll('.evidence-chip')
    const chipForSeq1 = evidenceChips.find((c) => c.text().includes('seq 1'))
    expect(chipForSeq1).toBeTruthy()

    // Stub scrollIntoView on whatever element ends up with the target id.
    const originalGetById = document.getElementById.bind(document)
    vi.spyOn(document, 'getElementById').mockImplementation((id) => {
      const el = originalGetById(id)
      if (el) (el as unknown as { scrollIntoView: () => void }).scrollIntoView = scrollSpy
      return el
    })

    await chipForSeq1!.trigger('click')
    await flushPromises()

    expect(getCheckoutSessionTimeline).toHaveBeenCalledTimes(2)
    expect(wrapper.find('#timeline-entry-1').exists()).toBe(true)
    expect(scrollSpy).toHaveBeenCalled()
  })
})

describe('CheckoutSessionDetailView — timeline pagination (R7)', () => {
  it('paginates against a mocked cursor sequence, including the terminal page', async () => {
    vi.mocked(getCheckoutSession).mockResolvedValue({ ...STALLED_SESSION, diagnosis: null })
    vi.mocked(getCheckoutSessionTimeline)
      .mockResolvedValueOnce({ data: [entry(1), entry(2)], page: { cursor: '2', has_more: true } })
      .mockResolvedValueOnce({ data: [entry(3), entry(4)], page: { cursor: '4', has_more: true } })
      .mockResolvedValueOnce({ data: [entry(5)], page: { cursor: null, has_more: false } })

    const { wrapper } = await mountDetail()
    expect(wrapper.findAll('.timeline .entry')).toHaveLength(2)
    expect(wrapper.find('.load-more-btn').exists()).toBe(true)

    await wrapper.find('.load-more-btn').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('.timeline .entry')).toHaveLength(4)
    expect(getCheckoutSessionTimeline).toHaveBeenNthCalledWith(2, 'co-stalled-1', { cursor: '2' })

    // Terminal page: has_more false removes the Load more control.
    await wrapper.find('.load-more-btn').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('.timeline .entry')).toHaveLength(5)
    expect(getCheckoutSessionTimeline).toHaveBeenNthCalledWith(3, 'co-stalled-1', { cursor: '4' })
    expect(wrapper.find('.load-more-btn').exists()).toBe(false)
  })
})

describe('CheckoutSessionDetailView — escalation "never returned" (R9)', () => {
  it('renders never-returned with elapsed time and stalled warning styling', async () => {
    vi.mocked(getCheckoutSession).mockResolvedValue(STALLED_SESSION)
    vi.mocked(getCheckoutSessionTimeline).mockResolvedValue({ data: [], page: { cursor: null, has_more: false } })
    const { wrapper } = await mountDetail()

    expect(wrapper.text()).toContain('Never returned')
    expect(wrapper.find('.escalation.stalled').exists()).toBe(true)
    expect(wrapper.find('.escalation').text()).toContain('Stalled')
  })

  it('renders a returned timestamp instead when returned_at is set', async () => {
    vi.mocked(getCheckoutSession).mockResolvedValue({
      ...STALLED_SESSION,
      diagnosis: null,
      escalation: { ...STALLED_SESSION.escalation!, returned_at: '2026-07-16T08:40:00Z', resolved: true },
    })
    vi.mocked(getCheckoutSessionTimeline).mockResolvedValue({ data: [], page: { cursor: null, has_more: false } })
    const { wrapper } = await mountDetail()

    expect(wrapper.text()).not.toContain('Never returned')
    expect(wrapper.find('.escalation').text()).toContain('Resolved')
  })
})

describe('CheckoutSessionDetailView — no retry/re-execute control (R10)', () => {
  it('contains no retry/re-run/re-execute affordance', async () => {
    vi.mocked(getCheckoutSession).mockResolvedValue(STALLED_SESSION)
    vi.mocked(getCheckoutSessionTimeline).mockResolvedValue({
      data: [entry(1), entry(5)],
      page: { cursor: null, has_more: false },
    })
    const { wrapper } = await mountDetail()
    expect(wrapper.text()).not.toMatch(/retry|re-run|rerun|re-execute|reexecute/i)
  })
})
