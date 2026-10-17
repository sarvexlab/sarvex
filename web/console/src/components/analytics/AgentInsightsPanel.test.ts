// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Agent Behavior Insights page. Covers:
//   - TierGate: locked on self_serve, unlocked on concierge/scale (R1/A1)
//   - month (capability gaps) and period/from/to (negotiation, product
//     demand) round-trip through the URL independently (R2/A1)
//   - Surface filter (product demand) is populated from response data, not
//     a hardcoded list — a fixture-only surface still appears (R2/A1)
//   - Section-independent loading and error retry (R3/R15/A1)
//   - Four sections render in the required order (R3)

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { routes } from '@/router'
import { i18n } from '@/i18n'
import { useStoresStore } from '@/stores/stores'
import AgentInsightsPanel from './AgentInsightsPanel.vue'
import type { CapabilityGap, NegotiationInsights, ProductDemand } from '@/api/insights'
import type { components } from '@/api/schema'

type Billing = components['schemas']['billing']

vi.mock('@/api/insights', () => ({
  listCapabilityGaps: vi.fn(),
  listProductDemand: vi.fn(),
  getNegotiationInsights: vi.fn(),
}))
vi.mock('@/api/billing', () => ({
  getBilling: vi.fn(),
  createPortalLink: vi.fn(),
}))
vi.mock('@/api/stores', () => ({ listStores: vi.fn(async () => []) }))

import { listCapabilityGaps, listProductDemand, getNegotiationInsights } from '@/api/insights'
import { getBilling } from '@/api/billing'
import { listStores } from '@/api/stores'

const billing = (over: Partial<Billing> = {}): Billing => ({
  plan: 'concierge',
  status: 'active',
  sku_band: 'up_to_5000',
  usage: { sku_count: 10, sku_cap: 100, member_cap: 5 },
  ...over,
})

const negotiation = (over: Partial<NegotiationInsights> = {}): NegotiationInsights => ({
  attempts: 10,
  successes: 8,
  failures: [],
  payment_handler_mismatch: { occurrences: 0, offered_handler_counts: {}, merchant_declared_handlers: [] },
  ...over,
})

let mountedWrappers: Array<ReturnType<typeof mount>> = []

async function mountView(path = '/analytics'): Promise<{ wrapper: ReturnType<typeof mount>; router: Router }> {
  setActivePinia(createPinia())
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push(path)
  await router.isReady()
  const wrapper = mount(AgentInsightsPanel, { global: { plugins: [router, i18n] } })
  mountedWrappers.push(wrapper)
  await flushPromises()
  return { wrapper, router }
}

beforeEach(() => {
  mountedWrappers = []
  localStorage.clear()
  vi.mocked(getBilling).mockReset().mockResolvedValue(billing())
  vi.mocked(listCapabilityGaps).mockReset().mockResolvedValue([] as CapabilityGap[])
  vi.mocked(listProductDemand).mockReset().mockResolvedValue([] as ProductDemand[])
  vi.mocked(getNegotiationInsights).mockReset().mockResolvedValue(negotiation())
  vi.mocked(listStores).mockReset().mockResolvedValue([])
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

describe('AgentInsightsPanel — TierGate (R1/A1)', () => {
  it('shows the locked panel on self_serve and never calls any insights endpoint', async () => {
    vi.mocked(getBilling).mockResolvedValue(billing({ plan: 'self_serve' }))
    const { wrapper } = await mountView()
    expect(wrapper.find('a.locked-cta').exists()).toBe(true)
    expect(listCapabilityGaps).not.toHaveBeenCalled()
    expect(getNegotiationInsights).not.toHaveBeenCalled()
    expect(listProductDemand).not.toHaveBeenCalled()
  })

  it('shows the real content on concierge', async () => {
    const { wrapper } = await mountView()
    expect(wrapper.find('.locked-cta').exists()).toBe(false)
    expect(listCapabilityGaps).toHaveBeenCalled()
  })

  it('shows the real content on scale', async () => {
    vi.mocked(getBilling).mockResolvedValue(billing({ plan: 'scale' }))
    const { wrapper } = await mountView()
    expect(wrapper.find('.locked-cta').exists()).toBe(false)
  })
})

describe('AgentInsightsPanel — section order (R3)', () => {
  it('renders the four sections in the required order: gaps, negotiation, product demand, attribute gaps', async () => {
    const { wrapper } = await mountView()
    const titles = wrapper.findAll('.section-title').map((t) => t.text())
    expect(titles).toEqual(['Capability gaps', 'Negotiation', 'Product demand', 'Attribute gaps'])
  })
})

describe('AgentInsightsPanel — period controls (R2/A1)', () => {
  it('defaults month and period, reflecting both in the URL', async () => {
    const { router } = await mountView()
    expect(router.currentRoute.value.query.month).toMatch(/^\d{4}-\d{2}$/)
    expect(router.currentRoute.value.query.period).toBe('this_month')
  })

  it('changing the gaps month updates the URL and refetches only capability gaps', async () => {
    const { wrapper, router } = await mountView()
    const callsBefore = vi.mocked(getNegotiationInsights).mock.calls.length
    await wrapper.find('.month-field input').setValue('2026-05')
    await flushPromises()
    expect(router.currentRoute.value.query.month).toBe('2026-05')
    expect(listCapabilityGaps).toHaveBeenLastCalledWith(expect.objectContaining({ month: '2026-05' }))
    expect(vi.mocked(getNegotiationInsights).mock.calls.length).toBe(callsBefore)
  })

  it('changing the range preset updates the URL and refetches negotiation/product demand', async () => {
    const { wrapper, router } = await mountView()
    await wrapper.find('.period-field select').setValue('last_90_days')
    await flushPromises()
    expect(router.currentRoute.value.query.period).toBe('last_90_days')
    expect(getNegotiationInsights).toHaveBeenLastCalledWith(expect.objectContaining({ from: expect.any(String), to: expect.any(String) }))
  })

  it('enforces the 92-day cap client-side instead of sending the request', async () => {
    const { wrapper } = await mountView('/analytics?period=custom&from=2026-01-01&to=2026-04-05')
    expect(wrapper.text()).toContain('more than 92 days')
    expect(getNegotiationInsights).not.toHaveBeenCalled()
    expect(listProductDemand).not.toHaveBeenCalled()
  })
})

describe('AgentInsightsPanel — surface filter driven by the API (R2/A1)', () => {
  it('populates surface options from product-demand response data, not a hardcoded list', async () => {
    vi.mocked(listProductDemand).mockResolvedValue([
      { product_id: 'SKU-1', request_count: 10, surface_breakdown: { google_ai_mode: 10 } },
    ])
    const { wrapper } = await mountView()
    const options = wrapper.findAll('.surface-field option').map((o) => o.attributes('value'))
    expect(options).toContain('google_ai_mode')
  })

  it('a surface unknown to any frontend constant still appears', async () => {
    vi.mocked(listProductDemand).mockResolvedValue([
      { product_id: 'SKU-1', request_count: 10, surface_breakdown: { brand_new_surface: 10 } },
    ])
    const { wrapper } = await mountView()
    const options = wrapper.findAll('.surface-field option').map((o) => o.attributes('value'))
    expect(options).toContain('brand_new_surface')
  })

  it('selecting a surface updates the URL and refetches product demand with it', async () => {
    vi.mocked(listProductDemand).mockResolvedValue([
      { product_id: 'SKU-1', request_count: 10, surface_breakdown: { google_ai_mode: 10 } },
    ])
    const { wrapper, router } = await mountView()
    await wrapper.find('.surface-field select').setValue('google_ai_mode')
    await flushPromises()
    expect(router.currentRoute.value.query.surface).toBe('google_ai_mode')
    expect(listProductDemand).toHaveBeenLastCalledWith(expect.objectContaining({ surface: 'google_ai_mode' }))
  })
})

describe('AgentInsightsPanel — section-independent loading/error retry (R3/R15/A1)', () => {
  it('a failing negotiation fetch leaves capability gaps and product demand rendered', async () => {
    vi.mocked(getNegotiationInsights).mockRejectedValue(new Error('negotiation down'))
    const { wrapper } = await mountView()
    expect(wrapper.find('.err').exists()).toBe(true)
    // Capability gaps (empty-array fixture) still renders its own positive
    // empty state rather than being blanked by the negotiation failure.
    expect(wrapper.text()).toContain("aren't requesting capabilities you lack")
    // Product demand (empty-array fixture) still renders its own empty state too.
    expect(wrapper.text()).toContain('No product demand recorded')
  })

  it('retrying a failed section only re-calls that section\'s endpoint', async () => {
    vi.mocked(getNegotiationInsights).mockRejectedValueOnce(new Error('negotiation down'))
    const { wrapper } = await mountView()
    expect(wrapper.find('.err').exists()).toBe(true)

    vi.mocked(getNegotiationInsights).mockResolvedValueOnce(negotiation())
    const capabilityCallsBefore = vi.mocked(listCapabilityGaps).mock.calls.length
    await wrapper.find('.err .btn').trigger('click')
    await flushPromises()

    expect(wrapper.find('.err').exists()).toBe(false)
    expect(vi.mocked(listCapabilityGaps).mock.calls.length).toBe(capabilityCallsBefore)
  })
})

describe('AgentInsightsPanel — scoped to the selected store', () => {
  beforeEach(() => {
    vi.mocked(listStores).mockResolvedValue([
      { store_id: 'str-1', name: 'Store 1' },
      { store_id: 'str-2', name: 'Store 2' },
    ])
  })

  it('capability gaps are scoped to the selected store and refetch on switch; negotiation/product demand do not', async () => {
    await mountView()
    await flushPromises()
    expect(listCapabilityGaps).toHaveBeenLastCalledWith(expect.objectContaining({ store_id: 'str-1' }))
    const negotiationCallsBefore = vi.mocked(getNegotiationInsights).mock.calls.length
    const demandCallsBefore = vi.mocked(listProductDemand).mock.calls.length

    useStoresStore().select('str-2')
    await flushPromises()
    expect(listCapabilityGaps).toHaveBeenLastCalledWith(expect.objectContaining({ store_id: 'str-2' }))
    // Neither endpoint accepts store_id (sarvex-v1-control-plane.yaml) —
    // switching stores must not trigger a wasted, identical re-fetch.
    expect(vi.mocked(getNegotiationInsights).mock.calls.length).toBe(negotiationCallsBefore)
    expect(vi.mocked(listProductDemand).mock.calls.length).toBe(demandCallsBefore)
  })

  it('labels negotiation and product demand as merchant-wide', async () => {
    const { wrapper } = await mountView()
    await flushPromises()
    expect(wrapper.text().match(/Includes every store on this account/g)?.length).toBe(2)
  })
})
