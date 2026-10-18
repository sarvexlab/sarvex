// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Checkout Escalation Console — session list. Covers:
//   - Live polling starts on mount, pauses on tab-hidden, resumes on
//     visible, stops on unmount (R2)
//   - Live/History mode switching (R1)
//   - Filter state round-trips through the URL query (R4)
//   - The stalled-escalation badge count + its one-click filter (R5)
//   - No retry/re-execute control anywhere (R10/A3)

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { routes } from '@/router'
import { i18n } from '@/i18n'
import { useStoresStore } from '@/stores/stores'
import CheckoutsPanel from './CheckoutsPanel.vue'
import type { CheckoutSession } from '@/api/checkoutSessions'

vi.mock('@/api/checkoutSessions', () => ({
  listCheckoutSessions: vi.fn(),
  listLiveCheckoutSessions: vi.fn(),
  getCheckoutSession: vi.fn(),
  getCheckoutSessionTimeline: vi.fn(),
}))
vi.mock('@/api/stores', () => ({ listStores: vi.fn(async () => []) }))

import { listCheckoutSessions, listLiveCheckoutSessions } from '@/api/checkoutSessions'
import { listStores } from '@/api/stores'

const session = (over: Partial<CheckoutSession> = {}): CheckoutSession => ({
  checkout_id: 'co-1',
  store_id: 'str-1',
  surface: 'ucp_checkout',
  state: 'cart_active',
  current_stage: 'cart_review',
  started_at: '2026-07-16T10:00:00Z',
  last_activity_at: '2026-07-16T10:01:00Z',
  cart_summary: { currency: 'AUD', total_minor: 1000, item_count: 1 },
  event_count: 2,
  updated_at: '2026-07-16T10:01:00Z',
  ...over,
})

function setVisibility(state: DocumentVisibilityState) {
  Object.defineProperty(document, 'visibilityState', { value: state, configurable: true })
  document.dispatchEvent(new Event('visibilitychange'))
}

// Every mounted wrapper is tracked and unmounted in afterEach — otherwise a
// prior test's un-torn-down poll interval/visibilitychange listener keeps
// firing against the shared fake-timer clock and inflates later tests'
// call counts.
let mountedWrappers: Array<ReturnType<typeof mount>> = []

async function mountView(path = '/analytics'): Promise<{ wrapper: ReturnType<typeof mount>; router: Router }> {
  setActivePinia(createPinia())
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push(path)
  await router.isReady()
  const wrapper = mount(CheckoutsPanel, { global: { plugins: [router, i18n] } })
  mountedWrappers.push(wrapper)
  await flushPromises()
  return { wrapper, router }
}

beforeEach(() => {
  vi.useFakeTimers()
  mountedWrappers = []
  localStorage.clear()
  vi.mocked(listLiveCheckoutSessions).mockReset().mockResolvedValue({
    data: [session()],
    page: { cursor: null, has_more: false },
  })
  vi.mocked(listCheckoutSessions).mockReset().mockResolvedValue({
    data: [],
    page: { cursor: null, has_more: false },
  })
  vi.mocked(listStores).mockReset().mockResolvedValue([])
  setVisibility('visible')
})

afterEach(() => {
  for (const w of mountedWrappers) {
    try {
      w.unmount()
    } catch {
      // Already unmounted by the test itself — fine.
    }
  }
  vi.useRealTimers()
})

describe('CheckoutsPanel — Live polling', () => {
  it('Live is the default landing mode and polls on mount', async () => {
    const { wrapper } = await mountView()
    expect(wrapper.find('[role="tab"].active').text()).toBe('Live')
    expect(listLiveCheckoutSessions).toHaveBeenCalledTimes(1)
  })

  it('polls again after the 15s interval', async () => {
    await mountView()
    await vi.advanceTimersByTimeAsync(15_000)
    expect(listLiveCheckoutSessions).toHaveBeenCalledTimes(2)
  })

  it('pauses polling while the tab is hidden and resumes on visible', async () => {
    await mountView()
    expect(listLiveCheckoutSessions).toHaveBeenCalledTimes(1)

    setVisibility('hidden')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(60_000)
    // No further polling while hidden.
    expect(listLiveCheckoutSessions).toHaveBeenCalledTimes(1)

    setVisibility('visible')
    await flushPromises()
    // Resuming re-fetches immediately.
    expect(listLiveCheckoutSessions).toHaveBeenCalledTimes(2)

    await vi.advanceTimersByTimeAsync(15_000)
    expect(listLiveCheckoutSessions).toHaveBeenCalledTimes(3)
  })

  it('stops polling on unmount', async () => {
    const { wrapper } = await mountView()
    expect(listLiveCheckoutSessions).toHaveBeenCalledTimes(1)
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(60_000)
    expect(listLiveCheckoutSessions).toHaveBeenCalledTimes(1)
  })
})

describe('CheckoutsPanel — mode switching', () => {
  it('switching to History stops the Live poll and loads history', async () => {
    const { wrapper } = await mountView()
    await wrapper.findAll('[role="tab"]')[1].trigger('click')
    await flushPromises()

    expect(wrapper.find('[role="tab"].active').text()).toBe('History')
    expect(listCheckoutSessions).toHaveBeenCalled()

    const callsBefore = vi.mocked(listLiveCheckoutSessions).mock.calls.length
    await vi.advanceTimersByTimeAsync(60_000)
    expect(listLiveCheckoutSessions).toHaveBeenCalledTimes(callsBefore)
  })

  it('switching back to Live resumes polling', async () => {
    const { wrapper } = await mountView('/analytics?mode=history')
    await wrapper.findAll('[role="tab"]')[0].trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="tab"].active').text()).toBe('Live')
    expect(listLiveCheckoutSessions).toHaveBeenCalled()
  })
})

describe('CheckoutsPanel — filters round-trip through the URL', () => {
  it('changing the state filter updates the URL query', async () => {
    const { wrapper, router } = await mountView('/analytics?mode=history')
    await flushPromises()

    const select = wrapper.find('select')
    await select.setValue('failed')
    await flushPromises()

    expect(router.currentRoute.value.query.state).toBe('failed')
    expect(listCheckoutSessions).toHaveBeenLastCalledWith(
      expect.objectContaining({ state: 'failed' }),
    )
  })

  it('a URL with filters already set initializes the filter state and query', async () => {
    const { wrapper } = await mountView('/analytics?mode=history&state=escalated&escalated_only=1')
    await flushPromises()

    expect(listCheckoutSessions).toHaveBeenCalledWith(
      expect.objectContaining({ state: 'escalated', escalated_only: true }),
    )
    const stateSelect = wrapper.find('select')
    expect((stateSelect.element as HTMLSelectElement).value).toBe('escalated')
  })
})

describe('CheckoutsPanel — stalled-escalation badge', () => {
  it('shows a persistent count badge and is visible without constructing a filter', async () => {
    vi.mocked(listCheckoutSessions).mockResolvedValue({
      data: [session({ checkout_id: 'co-stalled' })],
      page: { cursor: null, has_more: false },
    })
    const { wrapper } = await mountView()
    await flushPromises()

    expect(listCheckoutSessions).toHaveBeenCalledWith({ cause: 'escalation_stalled', limit: 100 })
    const badge = wrapper.find('.stalled-badge')
    expect(badge.exists()).toBe(true)
    expect(badge.text()).toContain('1')
    expect(badge.text()).toContain('stalled')
  })

  it('clicking the badge is a one-click filter into History mode with cause=escalation_stalled', async () => {
    vi.mocked(listCheckoutSessions).mockResolvedValue({
      data: [session({ checkout_id: 'co-stalled', diagnosis: { cause: 'escalation_stalled' } })],
      page: { cursor: null, has_more: false },
    })
    const { wrapper, router } = await mountView()
    await flushPromises()

    await wrapper.find('.stalled-badge').trigger('click')
    await flushPromises()

    expect(router.currentRoute.value.query.mode).toBe('history')
    expect(router.currentRoute.value.query.cause).toBe('escalation_stalled')
    expect(wrapper.find('[role="tab"].active').text()).toBe('History')
  })

  it('no badge is shown when there are no stalled escalations', async () => {
    const { wrapper } = await mountView()
    await flushPromises()
    expect(wrapper.find('.stalled-badge').exists()).toBe(false)
  })
})

describe('CheckoutsPanel — no retry/re-execute control (R10)', () => {
  it('the happy-path render contains no retry/re-run/re-execute affordance', async () => {
    const { wrapper } = await mountView()
    await flushPromises()
    expect(wrapper.text()).not.toMatch(/retry|re-run|rerun|re-execute|reexecute/i)
  })
})

describe('CheckoutsPanel — scoped to the selected store', () => {
  beforeEach(() => {
    vi.mocked(listStores).mockResolvedValue([
      { store_id: 'str-1', name: 'Store 1' },
      { store_id: 'str-2', name: 'Store 2' },
    ])
  })

  it('History requests are scoped to the selected store and refetch on switch', async () => {
    await mountView('/analytics?mode=history')
    await flushPromises()
    expect(listCheckoutSessions).toHaveBeenLastCalledWith(expect.objectContaining({ store_id: 'str-1' }))

    useStoresStore().select('str-2')
    await flushPromises()
    expect(listCheckoutSessions).toHaveBeenLastCalledWith(expect.objectContaining({ store_id: 'str-2' }))
  })

  it('Live sessions are filtered client-side to the selected store and update on switch', async () => {
    vi.mocked(listLiveCheckoutSessions).mockResolvedValue({
      data: [session({ checkout_id: 'co-1', store_id: 'str-1' }), session({ checkout_id: 'co-2', store_id: 'str-2' })],
      page: { cursor: null, has_more: false },
    })
    const { wrapper } = await mountView()
    await flushPromises()
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)

    useStoresStore().select('str-2')
    await flushPromises()
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
  })

  it('has no per-panel store filter of its own', async () => {
    const { wrapper } = await mountView('/analytics?mode=history')
    await flushPromises()
    expect(wrapper.text()).not.toContain('All stores')
  })
})
