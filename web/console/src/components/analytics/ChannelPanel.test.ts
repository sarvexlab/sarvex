// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Channel Dashboard page. Covers:
//   - TierGate: locked on self_serve, unlocked on concierge/scale (R1/A1)
//   - Period defaults to this_month and round-trips through the URL;
//     a custom range over the 92-day cap is rejected client-side with a
//     clear message rather than being sent to the API (R2/A1)
//   - Surface filter is populated from the summary response, not a
//     hardcoded list — a fixture-only surface appears with no code change
//     (R3/A1)
//   - Multi-currency lost-revenue figures render as separate groups, never
//     summed (R5/A2)
//   - Progressive rendering: a failing comparison fetch doesn't blank the
//     KPI row or funnel (R15/A5)

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { routes } from '@/router'
import { i18n } from '@/i18n'
import ChannelPanel from './ChannelPanel.vue'
import type { ChannelSummary, ChannelFunnel, ChannelDailyEntry, ChannelComparison } from '@/api/channel'
import type { LedgerSummary } from '@/api/ledger'
import type { components } from '@/api/schema'

type Billing = components['schemas']['billing']

vi.mock('@/api/channel', () => ({
  getChannelSummary: vi.fn(),
  getChannelFunnel: vi.fn(),
  getChannelDaily: vi.fn(),
  getChannelComparison: vi.fn(),
}))
vi.mock('@/api/ledger', () => ({
  listLedgerSummaries: vi.fn(),
  listLedgerEntries: vi.fn(async () => ({ data: [], page: { cursor: null, has_more: false } })),
  patchLedgerEntryStatus: vi.fn(),
}))
vi.mock('@/api/billing', () => ({
  getBilling: vi.fn(),
  createPortalLink: vi.fn(),
}))
vi.mock('@/api/stores', () => ({ listStores: vi.fn(async () => []) }))

import { getChannelSummary, getChannelFunnel, getChannelDaily, getChannelComparison } from '@/api/channel'
import { listLedgerSummaries } from '@/api/ledger'
import { getBilling } from '@/api/billing'

const billing = (over: Partial<Billing> = {}): Billing => ({
  plan: 'concierge',
  status: 'active',
  sku_band: 'up_to_5000',
  usage: { sku_count: 10, sku_cap: 100, member_cap: 5 },
  ...over,
})

const summaryRow = (over: Partial<ChannelSummary> = {}): ChannelSummary => ({
  surface: 'all',
  month: '2026-08',
  sessions: 100,
  sessions_confirmed: 100,
  sessions_inferred: 0,
  checkouts_completed: 40,
  revenue_minor: 800000,
  currency: 'USD',
  currency_mixed: false,
  cart_rate: 0.6,
  checkout_rate: 0.7,
  completion_rate: 0.4,
  aov_minor: 20000,
  prior_period: {
    sessions_delta: 5,
    checkouts_completed_delta: 2,
    revenue_minor_delta: 10000,
    aov_minor_delta: 500,
    completion_rate_delta: 0.02,
  },
  ...over,
})

const funnel = (over: Partial<ChannelFunnel> = {}): ChannelFunnel => ({
  from: '2026-08-01',
  to: '2026-08-06',
  surface: 'all',
  sessions: 100,
  sessions_confirmed: 100,
  sessions_inferred: 0,
  sessions_reached_cart: 60,
  sessions_reached_checkout: 40,
  checkouts_initiated: 100,
  checkouts_completed: 40,
  currency: 'USD',
  currency_mixed: false,
  rates: { cart_rate: 0.6, checkout_rate: 0.67, completion_rate: 0.4, aov_minor: 20000 },
  ...over,
})

const comparison = (over: Partial<ChannelComparison> = {}): ChannelComparison => ({
  from: '2026-08-01',
  to: '2026-08-06',
  agentic: { revenue_minor: 800000, checkouts_completed: 40, aov_minor: 20000, currency: 'USD', currency_mixed: false },
  web_baseline: { orders: 0, revenue_minor: 0, aov_minor: 0, currency: null, currency_mixed: false, confidence: 'unavailable' },
  revenue: { available: false, confidence: 'unavailable', reason: 'no baseline data synced for this period' },
  aov: { available: false, confidence: 'unavailable', reason: 'no baseline data synced for this period' },
  agentic_share_of_total: { available: false, confidence: 'unavailable', reason: 'no baseline data synced for this period' },
  ...over,
})

const ledgerSummary = (over: Partial<LedgerSummary> = {}): LedgerSummary => ({
  month: '2026-08',
  currency: 'USD',
  causes: [],
  total_count: 0,
  total_lost_revenue_minor: 0,
  entries_with_unknown_revenue: 0,
  prior_month_delta_minor: 0,
  ...over,
})

let mountedWrappers: Array<ReturnType<typeof mount>> = []

async function mountView(path = '/analytics'): Promise<{ wrapper: ReturnType<typeof mount>; router: Router }> {
  setActivePinia(createPinia())
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push(path)
  await router.isReady()
  const wrapper = mount(ChannelPanel, { global: { plugins: [router, i18n] } })
  mountedWrappers.push(wrapper)
  await flushPromises()
  return { wrapper, router }
}

beforeEach(() => {
  mountedWrappers = []
  vi.mocked(getBilling).mockReset().mockResolvedValue(billing())
  vi.mocked(getChannelSummary).mockReset().mockResolvedValue([summaryRow()])
  vi.mocked(getChannelFunnel).mockReset().mockResolvedValue(funnel())
  vi.mocked(getChannelDaily).mockReset().mockResolvedValue([] as ChannelDailyEntry[])
  vi.mocked(getChannelComparison).mockReset().mockResolvedValue(comparison())
  vi.mocked(listLedgerSummaries).mockReset().mockResolvedValue([ledgerSummary()])
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

describe('ChannelPanel — TierGate (R1/A1)', () => {
  it('shows the locked panel on self_serve and never calls the channel API', async () => {
    vi.mocked(getBilling).mockResolvedValue(billing({ plan: 'self_serve' }))
    const { wrapper } = await mountView()
    expect(wrapper.find('a.locked-cta').exists()).toBe(true)
    expect(getChannelSummary).not.toHaveBeenCalled()
  })

  it('shows the real content on concierge', async () => {
    const { wrapper } = await mountView()
    expect(wrapper.find('.locked-cta').exists()).toBe(false)
    expect(wrapper.find('.kpi-card').exists()).toBe(true)
  })

  it('shows the real content on scale', async () => {
    vi.mocked(getBilling).mockResolvedValue(billing({ plan: 'scale' }))
    const { wrapper } = await mountView()
    expect(wrapper.find('.locked-cta').exists()).toBe(false)
  })
})

describe('ChannelPanel — period control (R2/A1)', () => {
  it('defaults to this_month and reflects it in the URL', async () => {
    const { router } = await mountView()
    expect(router.currentRoute.value.query.period).toBe('this_month')
    expect(getChannelSummary).toHaveBeenCalledWith(expect.objectContaining({ month: expect.any(String) }))
  })

  it('changing the preset updates the URL and refetches', async () => {
    const { wrapper, router } = await mountView()
    await wrapper.find('.period-field select').setValue('last_90_days')
    await flushPromises()
    expect(router.currentRoute.value.query.period).toBe('last_90_days')
    expect(getChannelFunnel).toHaveBeenLastCalledWith(expect.objectContaining({ from: expect.any(String), to: expect.any(String) }))
  })

  it('enforces the 92-day cap client-side with a clear message instead of sending the request', async () => {
    const { wrapper } = await mountView('/analytics?period=custom&from=2026-01-01&to=2026-04-05')
    // 2026-01-01..2026-04-05 is 95 days — over the cap.
    expect(wrapper.text()).toContain("more than 92 days")
    expect(getChannelFunnel).not.toHaveBeenCalled()
    expect(getChannelDaily).not.toHaveBeenCalled()
    expect(getChannelComparison).not.toHaveBeenCalled()
  })

  it('accepts a custom range within the cap', async () => {
    const { wrapper } = await mountView('/analytics?period=custom&from=2026-01-01&to=2026-01-10')
    expect(wrapper.text()).not.toContain('92 days')
    expect(getChannelFunnel).toHaveBeenCalledWith(
      expect.objectContaining({ from: '2026-01-01', to: '2026-01-10' }),
    )
  })
})

describe('ChannelPanel — surface filter driven by the API (R3/A1)', () => {
  it('populates surface options from the summary response, not a hardcoded list', async () => {
    vi.mocked(getChannelSummary).mockResolvedValue([
      summaryRow({ surface: 'all' }),
      summaryRow({ surface: 'google_ai_mode' }),
    ])
    const { wrapper } = await mountView()
    const options = wrapper.findAll('.surface-field option').map((o) => o.attributes('value'))
    expect(options).toContain('google_ai_mode')
  })

  it('a surface unknown to any frontend constant still appears — proves no hardcoded enum (A1)', async () => {
    vi.mocked(getChannelSummary).mockResolvedValue([
      summaryRow({ surface: 'all' }),
      summaryRow({ surface: 'brand_new_surface' as ChannelSummary['surface'] }),
    ])
    const { wrapper } = await mountView()
    const options = wrapper.findAll('.surface-field option').map((o) => o.attributes('value'))
    expect(options).toContain('brand_new_surface')
    expect(wrapper.text()).toContain('Brand New Surface')
  })

  it('selecting a surface updates the URL and refetches funnel/daily with it', async () => {
    vi.mocked(getChannelSummary).mockResolvedValue([
      summaryRow({ surface: 'all' }),
      summaryRow({ surface: 'google_ai_mode' }),
    ])
    const { wrapper, router } = await mountView()
    await wrapper.find('.surface-field select').setValue('google_ai_mode')
    await flushPromises()
    expect(router.currentRoute.value.query.surface).toBe('google_ai_mode')
    expect(getChannelFunnel).toHaveBeenLastCalledWith(expect.objectContaining({ surface: 'google_ai_mode' }))
  })
})

describe('ChannelPanel — multi-currency lost revenue (R5/A2)', () => {
  it('renders one figure per currency, never a combined total', async () => {
    vi.mocked(listLedgerSummaries).mockResolvedValue([
      ledgerSummary({ currency: 'USD', total_lost_revenue_minor: 500000 }),
      ledgerSummary({ currency: 'AUD', total_lost_revenue_minor: 300000 }),
    ])
    const { wrapper } = await mountView()
    const strip = wrapper.find('.lost-revenue-strip')
    const values = strip.findAll('.lr-value')
    expect(values).toHaveLength(2)
    expect(strip.text()).toContain('USD')
    expect(strip.text()).toContain('AUD')
    // 500000 (USD) + 300000 (AUD) = 800000 minor units ($8,000.00) would be
    // the wrong, cross-currency-summed total — assert it never appears
    // within the lost-revenue strip specifically (the page's KPI row
    // separately and legitimately renders its own $8,000.00 from unrelated
    // channel revenue data, so this must not assert against the whole page).
    expect(strip.text()).not.toMatch(/\$8,000\.00/)
  })

  it('renders one figure for a single-currency period', async () => {
    vi.mocked(listLedgerSummaries).mockResolvedValue([ledgerSummary({ currency: 'USD', total_lost_revenue_minor: 500000 })])
    const { wrapper } = await mountView()
    expect(wrapper.findAll('.lr-value')).toHaveLength(1)
  })

  it('does not render the strip when there is no ledger data for the period', async () => {
    vi.mocked(listLedgerSummaries).mockResolvedValue([])
    const { wrapper } = await mountView()
    expect(wrapper.find('.lost-revenue-strip').exists()).toBe(false)
  })
})

describe('ChannelPanel — progressive rendering (R15/A5)', () => {
  it('a failing comparison fetch leaves the KPI row and funnel rendered', async () => {
    vi.mocked(getChannelComparison).mockRejectedValue(new Error('comparison unavailable'))
    const { wrapper } = await mountView()

    expect(wrapper.find('.comparison .err').exists()).toBe(true)
    expect(wrapper.find('.kpi-card').exists()).toBe(true)
    expect(wrapper.findAll('.kpi-card')).toHaveLength(4)
    expect(wrapper.find('.funnel .stages').exists()).toBe(true)
  })

  it('a failing summary fetch leaves the funnel and comparison rendered', async () => {
    vi.mocked(getChannelSummary).mockRejectedValue(new Error('summary unavailable'))
    const { wrapper } = await mountView()

    expect(wrapper.findAll('.kpi-row .err')).toHaveLength(1)
    expect(wrapper.find('.funnel .stages').exists()).toBe(true)
    expect(wrapper.find('.comparison-title').exists()).toBe(true)
  })

  it('retrying a failed section only re-calls that section\'s endpoint', async () => {
    vi.mocked(getChannelComparison).mockRejectedValueOnce(new Error('comparison unavailable'))
    const { wrapper } = await mountView()
    expect(wrapper.find('.comparison .err').exists()).toBe(true)

    vi.mocked(getChannelComparison).mockResolvedValueOnce(comparison())
    const callsBefore = vi.mocked(getChannelFunnel).mock.calls.length
    await wrapper.find('.comparison .err .btn').trigger('click')
    await flushPromises()

    expect(wrapper.find('.comparison .err').exists()).toBe(false)
    expect(vi.mocked(getChannelFunnel).mock.calls.length).toBe(callsBefore)
  })
})

describe('ChannelPanel — resolved contract gaps wired end-to-end (R5/R11)', () => {
  it('a mixed-currency summary period shows the notice instead of a revenue/AOV figure', async () => {
    vi.mocked(getChannelSummary).mockResolvedValue([summaryRow({ currency: null, currency_mixed: true })])
    const { wrapper } = await mountView()
    expect(wrapper.find('.kpi-card .mixed-note').exists()).toBe(true)
  })

  it('a partial-confidence comparison shows real numbers with a confidence indicator, not an unavailable state', async () => {
    vi.mocked(getChannelComparison).mockResolvedValue(
      comparison({
        web_baseline: { orders: 20, revenue_minor: 300000, aov_minor: 15000, currency: 'USD', currency_mixed: false, confidence: 'partial' },
        revenue: {
          available: true,
          confidence: 'partial',
          reason: '2 of 6 days in this period have incomplete or missing web-baseline data — figures below are computed from what was reported, not estimated for the gap',
          agentic_minor: 800000,
          baseline_minor: 300000,
          delta_minor: 500000,
        },
      }),
    )
    const { wrapper } = await mountView()
    expect(wrapper.find('.confidence-badge').exists()).toBe(true)
    expect(wrapper.find('.comparison .unavailable').exists()).toBe(true) // aov/share are still unavailable in this fixture
    expect(wrapper.text()).toContain('$3,000.00')
  })
})
