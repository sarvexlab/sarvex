// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Lost Revenue Ledger page. Covers:
//   - TierGate: locked on self_serve, unlocked on concierge/scale (R1/A1)
//   - Month selector defaults to current month and accepts an incoming
//     ?period= query param (R2)
//   - Multi-currency renders separate cards, never a combined total (R4/A2)
//   - revenue_unknown renders "not priced", never $0.00 (R11/A2)
//   - entries_with_unknown_revenue disclosure (R5/A2)
//   - buyer_abandoned renders in its own group (R7/A3)
//   - Clicking a cause bar filters the entries table (R8/A1)
//   - Pagination against a mocked cursor sequence incl. terminal page (A5)
//   - Status PATCH: optimistic + rollback, forward-only, bulk partial
//     failure (A4)
//   - Empty states (R16)

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { routes } from '@/router'
import { i18n } from '@/i18n'
import { useStoresStore } from '@/stores/stores'
import LostRevenuePanel from './LostRevenuePanel.vue'
import type { LedgerEntry, LedgerSummary } from '@/api/ledger'
import type { components } from '@/api/schema'

type Billing = components['schemas']['billing']

vi.mock('@/api/ledger', () => ({
  listLedgerSummaries: vi.fn(),
  listLedgerEntries: vi.fn(),
  patchLedgerEntryStatus: vi.fn(),
}))
vi.mock('@/api/billing', () => ({
  getBilling: vi.fn(),
  createPortalLink: vi.fn(),
}))
vi.mock('@/api/stores', () => ({ listStores: vi.fn(async () => []) }))

import { listLedgerSummaries, listLedgerEntries, patchLedgerEntryStatus } from '@/api/ledger'
import { getBilling } from '@/api/billing'
import { listStores } from '@/api/stores'

const billing = (over: Partial<Billing> = {}): Billing => ({
  plan: 'concierge',
  status: 'active',
  sku_band: 'up_to_5000',
  usage: { sku_count: 10, sku_cap: 100, member_cap: 5 },
  ...over,
})

const entry = (over: Partial<LedgerEntry>): LedgerEntry => ({
  entry_id: 'led-1',
  occurred_at: '2026-08-05T10:00:00Z',
  checkout_id: 'chk-1',
  store_id: 'str-1',
  surface: 'ucp_checkout',
  stage: 'payment',
  failure_code: 'CONNECTOR_CREATE_FAILED',
  cause: 'payment_handler',
  recommended_action: 'Check your payment handler config',
  lost_revenue_minor: 1000,
  currency: 'USD',
  revenue_unknown: false,
  status: 'open',
  status_changed_at: '2026-08-05T10:00:00Z',
  created_at: '2026-08-05T10:00:00Z',
  ...over,
})

const summary = (over: Partial<LedgerSummary> = {}): LedgerSummary => ({
  month: '2026-08',
  currency: 'USD',
  causes: [],
  total_count: 0,
  total_lost_revenue_minor: 0,
  entries_with_unknown_revenue: 0,
  prior_month_delta_minor: 0,
  ...over,
})

const entriesPage = (data: LedgerEntry[], cursor: string | null = null, has_more = false) => ({
  data,
  page: { cursor, has_more },
})

let mountedWrappers: Array<ReturnType<typeof mount>> = []

async function mountView(path = '/analytics'): Promise<{ wrapper: ReturnType<typeof mount>; router: Router }> {
  setActivePinia(createPinia())
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push(path)
  await router.isReady()
  const wrapper = mount(LostRevenuePanel, { global: { plugins: [router, i18n] } })
  mountedWrappers.push(wrapper)
  await flushPromises()
  return { wrapper, router }
}

beforeEach(() => {
  mountedWrappers = []
  localStorage.clear()
  vi.mocked(getBilling).mockReset().mockResolvedValue(billing())
  vi.mocked(listLedgerSummaries).mockReset().mockResolvedValue([summary()])
  vi.mocked(listLedgerEntries).mockReset().mockResolvedValue(entriesPage([]))
  vi.mocked(patchLedgerEntryStatus).mockReset()
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

describe('LostRevenuePanel — TierGate (R1/A1)', () => {
  it('shows the locked panel with upgrade CTA on self_serve', async () => {
    vi.mocked(getBilling).mockResolvedValue(billing({ plan: 'self_serve' }))
    const { wrapper } = await mountView()
    expect(wrapper.text()).toContain('See exactly what checkout failures are costing you')
    expect(wrapper.find('a.locked-cta').exists()).toBe(true)
    expect(listLedgerSummaries).not.toHaveBeenCalled()
  })

  it('shows the real content on concierge', async () => {
    vi.mocked(getBilling).mockResolvedValue(billing({ plan: 'concierge' }))
    const { wrapper } = await mountView()
    expect(wrapper.find('.locked-cta').exists()).toBe(false)
    expect(wrapper.text()).toContain('No lost revenue recorded this month')
  })

  it('shows the real content on scale', async () => {
    vi.mocked(getBilling).mockResolvedValue(billing({ plan: 'scale' }))
    const { wrapper } = await mountView()
    expect(wrapper.find('.locked-cta').exists()).toBe(false)
  })
})

describe('LostRevenuePanel — month selector (R2)', () => {
  it('defaults to the current month and reflects it in the URL', async () => {
    const { router } = await mountView()
    const now = new Date()
    const expected = `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}`
    expect(router.currentRoute.value.query.period).toBe(expected)
  })

  it('accepts an incoming period query param', async () => {
    const { wrapper } = await mountView('/analytics?period=2026-03')
    expect(listLedgerSummaries).toHaveBeenCalledWith({ month: '2026-03' })
    const input = wrapper.find('input[type="month"]')
    expect((input.element as HTMLInputElement).value).toBe('2026-03')
  })
})

describe('LostRevenuePanel — multi-currency (R4/A2)', () => {
  it('renders one card per currency, never a combined total', async () => {
    vi.mocked(listLedgerSummaries).mockResolvedValue([
      summary({ currency: 'USD', total_lost_revenue_minor: 500000 }),
      summary({ currency: 'AUD', total_lost_revenue_minor: 300000 }),
    ])
    const { wrapper } = await mountView('/analytics?period=2026-08')

    const cards = wrapper.findAll('.summary-card')
    expect(cards).toHaveLength(2)
    expect(wrapper.text()).toContain('USD')
    expect(wrapper.text()).toContain('AUD')
    // No single number sums both currencies (e.g. 8000.00 = 5000+3000 must not appear).
    expect(wrapper.text()).not.toMatch(/\$8,000\.00/)
  })

  it('renders one card when the merchant has a single currency', async () => {
    vi.mocked(listLedgerSummaries).mockResolvedValue([summary({ currency: 'USD' })])
    const { wrapper } = await mountView('/analytics?period=2026-08')
    expect(wrapper.findAll('.summary-card')).toHaveLength(1)
  })

  it('renders no card when the merchant has no data this period', async () => {
    vi.mocked(listLedgerSummaries).mockResolvedValue([])
    const { wrapper } = await mountView('/analytics?period=2026-08')
    expect(wrapper.findAll('.summary-card')).toHaveLength(0)
  })
})

describe('LostRevenuePanel — unknown revenue (R5/R11/A2)', () => {
  it('discloses the unknown-revenue count as a plain line', async () => {
    vi.mocked(listLedgerSummaries).mockResolvedValue([
      summary({ currency: 'USD', entries_with_unknown_revenue: 12 }),
    ])
    const { wrapper } = await mountView('/analytics?period=2026-08')
    expect(wrapper.text()).toContain('12 entries could not be priced')
  })

  it('does not show the disclosure line when the count is zero', async () => {
    vi.mocked(listLedgerSummaries).mockResolvedValue([
      summary({ currency: 'USD', entries_with_unknown_revenue: 0 }),
    ])
    const { wrapper } = await mountView('/analytics?period=2026-08')
    expect(wrapper.text()).not.toContain('could not be priced')
  })

  it('renders "Not priced" for a revenue_unknown entry, never $0.00', async () => {
    vi.mocked(listLedgerEntries).mockResolvedValue(
      entriesPage([entry({ entry_id: 'led-unpriced', revenue_unknown: true, lost_revenue_minor: 0 })]),
    )
    const { wrapper } = await mountView('/analytics?period=2026-08')
    const row = wrapper.find('.not-priced')
    expect(row.exists()).toBe(true)
    expect(row.text()).toBe('Not priced')
    expect(wrapper.find('.ptable-wrap').text()).not.toContain('$0.00')
  })
})

describe('LostRevenuePanel — cause breakdown (R6/R7/R8/A1/A3)', () => {
  const causesFixture = [
    { cause: 'payment_handler' as const, count: 3, lost_revenue_minor: 5000 },
    { cause: 'buyer_abandoned' as const, count: 9, lost_revenue_minor: 20000 },
    { cause: 'timeout' as const, count: 1, lost_revenue_minor: 100 },
  ]

  it('separates buyer_abandoned from fixable causes even though it has more revenue', async () => {
    vi.mocked(listLedgerSummaries).mockResolvedValue([summary({ currency: 'USD', causes: causesFixture })])
    const { wrapper } = await mountView('/analytics?period=2026-08')

    expect(wrapper.find('.abandoned-card').exists()).toBe(true)
    expect(wrapper.find('.abandoned-card').text()).toContain('Buyer abandoned')
    expect(wrapper.find('.bars-card').text()).not.toContain('Buyer abandoned')
    expect(wrapper.text()).toContain('buyer')
  })

  it('clicking a cause bar filters the entries table by that cause', async () => {
    vi.mocked(listLedgerSummaries).mockResolvedValue([summary({ currency: 'USD', causes: causesFixture })])
    const { wrapper, router } = await mountView('/analytics?period=2026-08')

    await wrapper.find('.bars-card .bar-row').trigger('click')
    await flushPromises()

    expect(router.currentRoute.value.query.cause).toBe('payment_handler')
    expect(listLedgerEntries).toHaveBeenLastCalledWith(
      expect.objectContaining({ cause: 'payment_handler' }),
    )
  })
})

describe('LostRevenuePanel — entries table server order (R9/A1)', () => {
  it('renders entries in server order without imposing a client default sort', async () => {
    vi.mocked(listLedgerEntries).mockResolvedValue(
      entriesPage([entry({ entry_id: 'low', lost_revenue_minor: 100 }), entry({ entry_id: 'high', lost_revenue_minor: 900 })]),
    )
    await mountView('/analytics?period=2026-08')
    for (const call of vi.mocked(listLedgerEntries).mock.calls) {
      expect(call[0]).not.toHaveProperty('sort')
    }
  })
})

describe('LostRevenuePanel — pagination (A5)', () => {
  it('shows Load more while has_more is true and hides it on the terminal page', async () => {
    vi.mocked(listLedgerEntries).mockResolvedValueOnce(
      entriesPage([entry({ entry_id: 'a' })], 'cur-a', true),
    )
    const { wrapper } = await mountView('/analytics?period=2026-08')
    expect(wrapper.find('.load-more-btn').exists()).toBe(true)

    vi.mocked(listLedgerEntries).mockResolvedValueOnce(
      entriesPage([entry({ entry_id: 'b' })], null, false),
    )
    await wrapper.find('.load-more-btn').trigger('click')
    await flushPromises()

    expect(wrapper.find('.load-more-btn').exists()).toBe(false)
    expect(listLedgerEntries).toHaveBeenLastCalledWith(expect.objectContaining({ cursor: 'cur-a' }))
  })
})

describe('LostRevenuePanel — status actions (R14/R15/A4)', () => {
  it('acknowledges optimistically and rolls back with a visible error on failure', async () => {
    vi.mocked(listLedgerEntries).mockResolvedValue(entriesPage([entry({ entry_id: 'a', status: 'open' })]))
    const { wrapper } = await mountView('/analytics?period=2026-08')

    vi.mocked(patchLedgerEntryStatus).mockRejectedValue(new Error('boom'))
    const ackBtn = wrapper.find('.status-btn')
    await ackBtn.trigger('click')
    await flushPromises()

    expect(wrapper.find('.err').exists()).toBe(true)
    expect(wrapper.find('.chip-warn').exists()).toBe(true) // back to 'open'
  })

  it('only renders forward-transition buttons — resolved is terminal', async () => {
    vi.mocked(listLedgerEntries).mockResolvedValue(entriesPage([entry({ entry_id: 'a', status: 'resolved' })]))
    const { wrapper } = await mountView('/analytics?period=2026-08')
    expect(wrapper.find('.status-btn').exists()).toBe(false)
  })

  it('bulk status change reports partial failure (N succeeded, M failed)', async () => {
    vi.mocked(listLedgerEntries).mockResolvedValue(
      entriesPage([
        entry({ entry_id: 'a', status: 'open' }),
        entry({ entry_id: 'b', status: 'open' }),
      ]),
    )
    const { wrapper } = await mountView('/analytics?period=2026-08')

    vi.mocked(patchLedgerEntryStatus).mockImplementation(async (id, status) => {
      if (id === 'b') throw new Error('boom')
      return entry({ entry_id: id, status })
    })

    const checkboxes = wrapper.findAll('input[type="checkbox"]').filter((c) => c.attributes('aria-label')?.startsWith('Select entry'))
    for (const c of checkboxes) await c.setValue(true)
    await wrapper.find('.bulk-btn').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('1 succeeded, 1 failed')
  })
})

describe('LostRevenuePanel — empty states (R16)', () => {
  it('frames a totally empty period as good news', async () => {
    vi.mocked(listLedgerEntries).mockResolvedValue(entriesPage([]))
    const { wrapper } = await mountView('/analytics?period=2026-08')
    expect(wrapper.text()).toContain('No lost revenue recorded this month')
  })

  it('uses the standard filtered-empty pattern when filters narrow to nothing', async () => {
    const { wrapper } = await mountView('/analytics?period=2026-08&cause=timeout')
    await flushPromises()
    expect(wrapper.text()).toContain('No entries match these filters')
  })
})

describe('LostRevenuePanel — scoped to the selected store', () => {
  beforeEach(() => {
    vi.mocked(listStores).mockResolvedValue([
      { store_id: 'str-1', name: 'Store 1' },
      { store_id: 'str-2', name: 'Store 2' },
    ])
  })

  it('entries are scoped to the selected store and refetch on switch, summaries do not', async () => {
    await mountView('/analytics?period=2026-08')
    await flushPromises()
    expect(listLedgerEntries).toHaveBeenLastCalledWith(expect.objectContaining({ store_id: 'str-1' }))
    const summaryCallsBefore = vi.mocked(listLedgerSummaries).mock.calls.length

    useStoresStore().select('str-2')
    await flushPromises()
    expect(listLedgerEntries).toHaveBeenLastCalledWith(expect.objectContaining({ store_id: 'str-2' }))
    // GET /ledger/summary has no store_id param — switching stores must not
    // trigger a wasted re-fetch of a response that can't change.
    expect(vi.mocked(listLedgerSummaries).mock.calls.length).toBe(summaryCallsBefore)
  })

  it('has no per-panel store filter, and labels the summary as merchant-wide', async () => {
    const { wrapper } = await mountView('/analytics?period=2026-08')
    await flushPromises()
    expect(wrapper.text()).not.toContain('All stores')
    expect(wrapper.text()).toContain('Includes every store on this account')
  })
})
