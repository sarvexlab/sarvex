// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Analytics surface — tab shell over Checkouts / Lost Revenue / Channel
// Dashboard / Agent Behavior Insights. Covers:
//   - Default tab is Checkouts when no ?tab= is present
//   - Clicking a tab switches the rendered panel and updates the URL
//   - Deep-linking via ?tab= renders the right panel on mount

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { routes } from '@/router'
import { i18n } from '@/i18n'
import AnalyticsSurface from './AnalyticsSurface.vue'
import type { components } from '@/api/schema'

type Billing = components['schemas']['billing']

vi.mock('@/api/checkoutSessions', () => ({
  listCheckoutSessions: vi.fn(async () => ({ data: [], page: { cursor: null, has_more: false } })),
  listLiveCheckoutSessions: vi.fn(async () => ({ data: [], page: { cursor: null, has_more: false } })),
  getCheckoutSession: vi.fn(),
  getCheckoutSessionTimeline: vi.fn(),
}))
vi.mock('@/api/stores', () => ({ listStores: vi.fn(async () => []) }))
vi.mock('@/api/billing', () => ({
  getBilling: vi.fn(async () => billing()),
  createPortalLink: vi.fn(),
}))
vi.mock('@/api/ledger', () => ({
  listLedgerSummaries: vi.fn(async () => ({})),
  listLedgerEntries: vi.fn(async () => ({ data: [], page: { cursor: null, has_more: false } })),
  patchLedgerEntryStatus: vi.fn(),
}))
vi.mock('@/api/channel', () => ({
  getChannelSummary: vi.fn(async () => ({ data: [] })),
  getChannelFunnel: vi.fn(),
  getChannelDaily: vi.fn(),
  getChannelComparison: vi.fn(),
}))
vi.mock('@/api/insights', () => ({
  listCapabilityGaps: vi.fn(),
  listProductDemand: vi.fn(),
  getNegotiationInsights: vi.fn(),
}))

function billing(over: Partial<Billing> = {}): Billing {
  return {
    plan: 'self_serve',
    status: 'active',
    sku_band: 'up_to_5000',
    usage: { sku_count: 10, sku_cap: 100, member_cap: 5 },
    ...over,
  }
}

let mountedWrappers: Array<ReturnType<typeof mount>> = []

async function mountSurface(path = '/analytics'): Promise<{ wrapper: ReturnType<typeof mount>; router: Router }> {
  setActivePinia(createPinia())
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push(path)
  await router.isReady()
  const wrapper = mount(AnalyticsSurface, { global: { plugins: [router, i18n] } })
  mountedWrappers.push(wrapper)
  await flushPromises()
  return { wrapper, router }
}

beforeEach(() => {
  mountedWrappers = []
})

afterEach(() => {
  for (const w of mountedWrappers) w.unmount()
})

describe('AnalyticsSurface', () => {
  it('defaults to the Checkouts tab', async () => {
    const { wrapper } = await mountSurface()
    const tabs = wrapper.findAll('.tabs button')
    expect(tabs[0].classes()).toContain('active')
    expect(wrapper.findAll('.tabs button.active')).toHaveLength(1)
  })

  it('switches panels on tab click and updates the URL', async () => {
    const { wrapper, router } = await mountSurface()
    const tabs = wrapper.findAll('.tabs button')
    await tabs[1].trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.tab).toBe('lost-revenue')
    expect(tabs[1].classes()).toContain('active')
  })

  it('deep-links to the requested tab via ?tab=', async () => {
    const { wrapper } = await mountSurface('/analytics?tab=channel')
    const tabs = wrapper.findAll('.tabs button')
    expect(tabs[2].classes()).toContain('active')
  })

  it('falls back to Checkouts for an unknown ?tab= value', async () => {
    const { wrapper } = await mountSurface('/analytics?tab=nonsense')
    const tabs = wrapper.findAll('.tabs button')
    expect(tabs[0].classes()).toContain('active')
  })
})
