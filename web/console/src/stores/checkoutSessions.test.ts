// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useCheckoutSessionsStore } from './checkoutSessions'
import type { CheckoutSession, CheckoutSessionTimelineEntry } from '@/api/checkoutSessions'

vi.mock('@/api/checkoutSessions', () => ({
  listCheckoutSessions: vi.fn(),
  listLiveCheckoutSessions: vi.fn(),
  getCheckoutSession: vi.fn(),
  getCheckoutSessionTimeline: vi.fn(),
}))

import {
  listCheckoutSessions,
  listLiveCheckoutSessions,
  getCheckoutSession,
  getCheckoutSessionTimeline,
} from '@/api/checkoutSessions'

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

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(listCheckoutSessions).mockReset()
  vi.mocked(listLiveCheckoutSessions).mockReset()
  vi.mocked(getCheckoutSession).mockReset()
  vi.mocked(getCheckoutSessionTimeline).mockReset()
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('useCheckoutSessionsStore — live poll', () => {
  it('loadLive populates sessions and records lastPolledAt', async () => {
    vi.mocked(listLiveCheckoutSessions).mockResolvedValue({
      data: [session()],
      page: { cursor: null, has_more: false },
    })
    const store = useCheckoutSessionsStore()
    await store.loadLive()
    expect(store.live).toHaveLength(1)
    expect(store.liveLoaded).toBe(true)
    expect(store.lastPolledAt).not.toBeNull()
  })

  it('startPolling polls on the documented 15s interval', async () => {
    vi.mocked(listLiveCheckoutSessions).mockResolvedValue({
      data: [],
      page: { cursor: null, has_more: false },
    })
    const store = useCheckoutSessionsStore()
    store.startPolling()
    expect(store.polling).toBe(true)

    await vi.advanceTimersByTimeAsync(15_000)
    expect(listLiveCheckoutSessions).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(15_000)
    expect(listLiveCheckoutSessions).toHaveBeenCalledTimes(2)
  })

  it('stopPolling halts further polls', async () => {
    vi.mocked(listLiveCheckoutSessions).mockResolvedValue({
      data: [],
      page: { cursor: null, has_more: false },
    })
    const store = useCheckoutSessionsStore()
    store.startPolling()
    store.stopPolling()
    expect(store.polling).toBe(false)

    await vi.advanceTimersByTimeAsync(60_000)
    expect(listLiveCheckoutSessions).not.toHaveBeenCalled()
  })

  it('captures loadLive errors as a recoverable error string', async () => {
    vi.mocked(listLiveCheckoutSessions).mockRejectedValue(new Error('network down'))
    const store = useCheckoutSessionsStore()
    await store.loadLive()
    expect(store.liveError).toBe('network down')
  })
})

describe('useCheckoutSessionsStore — history', () => {
  it('loadHistory replaces the list and captures the cursor', async () => {
    vi.mocked(listCheckoutSessions).mockResolvedValue({
      data: [session({ checkout_id: 'co-1' })],
      page: { cursor: 'cur-1', has_more: true },
    })
    const store = useCheckoutSessionsStore()
    await store.loadHistory({ state: 'cart_active' })
    expect(store.history).toHaveLength(1)
    expect(store.historyCursor).toBe('cur-1')
    expect(store.historyHasMore).toBe(true)
  })

  it('loadMoreHistory appends using the stored filter + cursor', async () => {
    vi.mocked(listCheckoutSessions)
      .mockResolvedValueOnce({ data: [session({ checkout_id: 'co-1' })], page: { cursor: 'cur-1', has_more: true } })
      .mockResolvedValueOnce({ data: [session({ checkout_id: 'co-2' })], page: { cursor: null, has_more: false } })
    const store = useCheckoutSessionsStore()
    await store.loadHistory({ cause: 'unknown' })
    await store.loadMoreHistory()

    expect(store.history.map((s) => s.checkout_id)).toEqual(['co-1', 'co-2'])
    expect(listCheckoutSessions).toHaveBeenLastCalledWith({ cause: 'unknown', cursor: 'cur-1' })
    expect(store.historyHasMore).toBe(false)
  })

  it('loadMoreHistory is a no-op when there is no more to fetch', async () => {
    const store = useCheckoutSessionsStore()
    await store.loadMoreHistory()
    expect(listCheckoutSessions).not.toHaveBeenCalled()
  })
})

describe('useCheckoutSessionsStore — stalled-escalation badge', () => {
  it('loadStalledCount counts returned rows and flags truncation via has_more', async () => {
    vi.mocked(listCheckoutSessions).mockResolvedValue({
      data: [session({ checkout_id: 'co-1' }), session({ checkout_id: 'co-2' })],
      page: { cursor: null, has_more: true },
    })
    const store = useCheckoutSessionsStore()
    await store.loadStalledCount()
    expect(listCheckoutSessions).toHaveBeenCalledWith({ cause: 'escalation_stalled', limit: 100 })
    expect(store.stalledCount).toBe(2)
    expect(store.stalledBadge).toBe('100+')
  })

  it('stalledBadge is the exact count when the result was not truncated', async () => {
    vi.mocked(listCheckoutSessions).mockResolvedValue({
      data: [session()],
      page: { cursor: null, has_more: false },
    })
    const store = useCheckoutSessionsStore()
    await store.loadStalledCount()
    expect(store.stalledBadge).toBe('1')
  })
})

describe('useCheckoutSessionsStore — detail + timeline', () => {
  it('loadDetail then loadTimeline populate the detail page state', async () => {
    vi.mocked(getCheckoutSession).mockResolvedValue(session({ checkout_id: 'co-1' }))
    vi.mocked(getCheckoutSessionTimeline).mockResolvedValue({
      data: [{ seq: 1, occurred_at: '2026-07-16T10:00:00Z', event_type: 'session.created', stage: 'initiated', payload: {} }],
      page: { cursor: null, has_more: false },
    })
    const store = useCheckoutSessionsStore()
    await store.loadDetail('co-1')
    await store.loadTimeline('co-1')
    expect(store.detail?.checkout_id).toBe('co-1')
    expect(store.timeline).toHaveLength(1)
  })

  it('loadMoreTimeline appends subsequent pages by seq cursor', async () => {
    const page1: CheckoutSessionTimelineEntry[] = [
      { seq: 1, occurred_at: '2026-07-16T10:00:00Z', event_type: 'a', stage: 's1', payload: {} },
    ]
    const page2: CheckoutSessionTimelineEntry[] = [
      { seq: 2, occurred_at: '2026-07-16T10:01:00Z', event_type: 'b', stage: 's2', payload: {} },
    ]
    vi.mocked(getCheckoutSessionTimeline)
      .mockResolvedValueOnce({ data: page1, page: { cursor: '1', has_more: true } })
      .mockResolvedValueOnce({ data: page2, page: { cursor: null, has_more: false } })

    const store = useCheckoutSessionsStore()
    await store.loadTimeline('co-1')
    await store.loadMoreTimeline('co-1')

    expect(store.timeline.map((e) => e.seq)).toEqual([1, 2])
    expect(getCheckoutSessionTimeline).toHaveBeenLastCalledWith('co-1', { cursor: '1' })
    expect(store.timelineHasMore).toBe(false)
  })

  it('clearDetail resets detail and timeline state', async () => {
    vi.mocked(getCheckoutSession).mockResolvedValue(session({ checkout_id: 'co-1' }))
    const store = useCheckoutSessionsStore()
    await store.loadDetail('co-1')
    store.clearDetail()
    expect(store.detail).toBeNull()
    expect(store.timeline).toEqual([])
    expect(store.timelineLoaded).toBe(false)
  })
})
