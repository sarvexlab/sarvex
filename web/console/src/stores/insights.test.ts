// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useInsightsStore } from './insights'
import type { CapabilityGap, NegotiationInsights, ProductDemand } from '@/api/insights'

vi.mock('@/api/insights', () => ({
  listCapabilityGaps: vi.fn(),
  listProductDemand: vi.fn(),
  getNegotiationInsights: vi.fn(),
}))

import { listCapabilityGaps, listProductDemand, getNegotiationInsights } from '@/api/insights'

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(listCapabilityGaps).mockReset()
  vi.mocked(listProductDemand).mockReset()
  vi.mocked(getNegotiationInsights).mockReset()
})

describe('useInsightsStore — capability gaps section (R15/R17)', () => {
  it('loads independently and exposes its own loading/loaded/error state, preserving server order', async () => {
    const rows: CapabilityGap[] = [
      { capability_id: 'a', request_count: 500 },
      { capability_id: 'b', request_count: 5 },
    ]
    vi.mocked(listCapabilityGaps).mockResolvedValue(rows)
    const store = useInsightsStore()

    const promise = store.loadCapabilityGaps({ month: '2026-08' })
    expect(store.capabilityGapsLoading).toBe(true)
    await promise
    expect(store.capabilityGapsLoading).toBe(false)
    expect(store.capabilityGapsLoaded).toBe(true)
    expect(store.capabilityGaps).toEqual(rows)
    expect(store.capabilityGapsError).toBeNull()
  })

  it('sets capabilityGapsError without touching other sections on failure', async () => {
    vi.mocked(listCapabilityGaps).mockRejectedValue(new Error('boom'))
    const store = useInsightsStore()
    await store.loadCapabilityGaps({ month: '2026-08' })
    expect(store.capabilityGapsError).toBe('boom')
    expect(store.negotiationError).toBeNull()
    expect(store.productDemandError).toBeNull()
  })
})

describe('useInsightsStore — negotiation / product demand sections (R15)', () => {
  it('negotiation failure does not affect capability gaps or product demand state', async () => {
    vi.mocked(listCapabilityGaps).mockResolvedValue([{ capability_id: 'a', request_count: 10 }])
    vi.mocked(getNegotiationInsights).mockRejectedValue(new Error('negotiation down'))
    const store = useInsightsStore()
    await store.loadCapabilityGaps({ month: '2026-08' })
    await store.loadNegotiation({ from: '2026-08-01', to: '2026-08-06' })
    expect(store.capabilityGapsError).toBeNull()
    expect(store.capabilityGaps).toHaveLength(1)
    expect(store.negotiationError).toBe('negotiation down')
    expect(store.negotiation).toBeNull()
  })

  it('loads a negotiation payload on success', async () => {
    const negotiation: NegotiationInsights = { attempts: 10, successes: 8, failures: [], payment_handler_mismatch: { occurrences: 0, offered_handler_counts: {}, merchant_declared_handlers: [] } }
    vi.mocked(getNegotiationInsights).mockResolvedValue(negotiation)
    const store = useInsightsStore()
    await store.loadNegotiation({ from: '2026-08-01', to: '2026-08-06' })
    expect(store.negotiation).toEqual(negotiation)
    expect(store.negotiationLoaded).toBe(true)
  })

  it('product demand failure does not blank negotiation or capability gaps', async () => {
    vi.mocked(getNegotiationInsights).mockResolvedValue({ attempts: 1, successes: 1, failures: [], payment_handler_mismatch: { occurrences: 0, offered_handler_counts: {}, merchant_declared_handlers: [] } })
    vi.mocked(listProductDemand).mockRejectedValue(new Error('demand down'))
    const store = useInsightsStore()
    await Promise.all([
      store.loadNegotiation({ from: '2026-08-01', to: '2026-08-06' }),
      store.loadProductDemand({ from: '2026-08-01', to: '2026-08-06' }),
    ])
    expect(store.productDemandError).toBe('demand down')
    expect(store.productDemand).toEqual([])
    expect(store.negotiation).not.toBeNull()
  })

  it('accumulates surfaces seen across loads, driven by response data (R2)', async () => {
    const rows: ProductDemand[] = [{ product_id: 'a', request_count: 10, surface_breakdown: { google_ai_mode: 10 } }]
    vi.mocked(listProductDemand).mockResolvedValueOnce(rows)
    const store = useInsightsStore()
    await store.loadProductDemand({ from: '2026-08-01', to: '2026-08-06' })
    expect(store.productDemandSurfaces).toEqual(['google_ai_mode'])

    vi.mocked(listProductDemand).mockResolvedValueOnce([
      { product_id: 'b', request_count: 5, surface_breakdown: { brand_new_surface: 5 } },
    ])
    await store.loadProductDemand({ from: '2026-08-01', to: '2026-08-06', surface: 'brand_new_surface' })
    // Grows, never loses a previously seen surface even under a narrower filter.
    expect(store.productDemandSurfaces).toEqual(['brand_new_surface', 'google_ai_mode'])
  })
})
