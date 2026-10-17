// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useChannelStore } from './channel'
import type { ChannelSummary, ChannelFunnel, ChannelDailyEntry, ChannelComparison } from '@/api/channel'

vi.mock('@/api/channel', () => ({
  getChannelSummary: vi.fn(),
  getChannelFunnel: vi.fn(),
  getChannelDaily: vi.fn(),
  getChannelComparison: vi.fn(),
}))

import { getChannelSummary, getChannelFunnel, getChannelDaily, getChannelComparison } from '@/api/channel'

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(getChannelSummary).mockReset()
  vi.mocked(getChannelFunnel).mockReset()
  vi.mocked(getChannelDaily).mockReset()
  vi.mocked(getChannelComparison).mockReset()
})

describe('useChannelStore — summary section (R15)', () => {
  it('loads independently and exposes its own loading/loaded/error state', async () => {
    const rows: ChannelSummary[] = [{ surface: 'all', month: '2026-08' }]
    vi.mocked(getChannelSummary).mockResolvedValue(rows)
    const store = useChannelStore()

    const promise = store.loadSummary({ month: '2026-08' })
    expect(store.summaryLoading).toBe(true)
    await promise
    expect(store.summaryLoading).toBe(false)
    expect(store.summaryLoaded).toBe(true)
    expect(store.summaries).toEqual(rows)
    expect(store.summaryError).toBeNull()
  })

  it('sets summaryError without touching other sections on failure', async () => {
    vi.mocked(getChannelSummary).mockRejectedValue(new Error('boom'))
    const store = useChannelStore()
    await store.loadSummary({ month: '2026-08' })
    expect(store.summaryError).toBe('boom')
    expect(store.funnelError).toBeNull()
    expect(store.comparisonError).toBeNull()
  })

  it('availableSurfaces (R3) is driven by response data, not a hardcoded list', async () => {
    vi.mocked(getChannelSummary).mockResolvedValue([
      { surface: 'all', month: '2026-08' },
      { surface: 'google_ai_mode', month: '2026-08' },
      { surface: 'google_gemini', month: '2026-08' },
    ])
    const store = useChannelStore()
    await store.loadSummary({})
    expect(store.availableSurfaces).toEqual(['google_ai_mode', 'google_gemini'])
  })

  it('a fixture surface unknown to any hardcoded enum still appears (R3/A1)', async () => {
    vi.mocked(getChannelSummary).mockResolvedValue([
      { surface: 'all', month: '2026-08' },
      // A surface value that doesn't exist in any frontend constant today —
      // proves the list comes from the response, not from code.
      { surface: 'brand_new_surface' as ChannelSummary['surface'], month: '2026-08' },
    ])
    const store = useChannelStore()
    await store.loadSummary({})
    expect(store.availableSurfaces).toContain('brand_new_surface')
  })
})

describe('useChannelStore — funnel/daily/comparison sections (R15)', () => {
  it('funnel failure does not affect summary state', async () => {
    vi.mocked(getChannelSummary).mockResolvedValue([{ surface: 'all', month: '2026-08' }])
    vi.mocked(getChannelFunnel).mockRejectedValue(new Error('funnel down'))
    const store = useChannelStore()
    await store.loadSummary({ month: '2026-08' })
    await store.loadFunnel({ from: '2026-08-01', to: '2026-08-06' })
    expect(store.summaryError).toBeNull()
    expect(store.summaries).toHaveLength(1)
    expect(store.funnelError).toBe('funnel down')
    expect(store.funnel).toBeNull()
  })

  it('loads daily rows independently', async () => {
    const rows: ChannelDailyEntry[] = [{ date: '2026-08-01', surface: 'google_ai_mode' }]
    vi.mocked(getChannelDaily).mockResolvedValue(rows)
    const store = useChannelStore()
    await store.loadDaily({ from: '2026-08-01', to: '2026-08-06' })
    expect(store.daily).toEqual(rows)
    expect(store.dailyLoaded).toBe(true)
  })

  it('comparison failure does not blank funnel/summary/daily (R15/A5)', async () => {
    vi.mocked(getChannelSummary).mockResolvedValue([{ surface: 'all', month: '2026-08' }])
    const funnel: ChannelFunnel = { from: '2026-08-01', to: '2026-08-06', sessions: 10 }
    vi.mocked(getChannelFunnel).mockResolvedValue(funnel)
    vi.mocked(getChannelComparison).mockRejectedValue(new Error('comparison down'))

    const store = useChannelStore()
    await Promise.all([
      store.loadSummary({ month: '2026-08' }),
      store.loadFunnel({ from: '2026-08-01', to: '2026-08-06' }),
      store.loadComparison({ from: '2026-08-01', to: '2026-08-06' }),
    ])

    expect(store.comparisonError).toBe('comparison down')
    expect(store.comparison).toBeNull()
    expect(store.summaries).toHaveLength(1)
    expect(store.funnel).toEqual(funnel)
  })

  it('loads a comparison payload on success', async () => {
    const comparison: ChannelComparison = {
      from: '2026-08-01',
      to: '2026-08-06',
      revenue: { available: false, reason: 'no baseline data synced for this period' },
    }
    vi.mocked(getChannelComparison).mockResolvedValue(comparison)
    const store = useChannelStore()
    await store.loadComparison({ from: '2026-08-01', to: '2026-08-06' })
    expect(store.comparison).toEqual(comparison)
    expect(store.comparisonLoaded).toBe(true)
  })
})
