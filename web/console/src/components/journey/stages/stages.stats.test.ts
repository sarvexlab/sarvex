// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Pins what each stage renders *besides* the task feed.
//
// The answer should be: almost nothing. Everything the design draws as a
// card is a spine task now, arriving through TaskFeed, so the only extra is
// the stat strip — facts that aren't work for anyone, deliberately not
// wearing the task card's clothes.

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { routes } from '@/router'
import { i18n } from '@/i18n'
import StageLayout from './StageLayout.vue'
import { useStoresStore } from '@/stores/stores'
import { useOrdersStore } from '@/stores/orders'
import { useHealthStore } from '@/stores/health'
import type { components } from '@/api/schema'

type UCPState = components['schemas']['ucp_state']
type Store = components['schemas']['store']

vi.mock('@/api/tasks', () => ({ getTask: vi.fn() }))
vi.mock('@/api/connectors', () => ({ listConnectors: vi.fn(async () => []), createConnector: vi.fn(), getConnector: vi.fn() }))
vi.mock('@/api/catalog', () => ({ getReadiness: vi.fn(async () => ({})), listProducts: vi.fn(async () => ({ products: [], cursor: null, hasMore: false })), getProduct: vi.fn() }))
vi.mock('@/api/orders', () => ({ listOrders: vi.fn(async () => []) }))
vi.mock('@/api/health', () => ({ getHealth: vi.fn(async () => ({})), listConsistency: vi.fn(async () => []) }))

const STORE: Store = { store_id: 's1', name: 'Harbor & Pine', platform: 'woocommerce' } as Store

const ALL: UCPState[] = [
  'draft', 'connector_linked', 'profile_published', 'verified', 'active',
]

async function mountStage(state: UCPState) {
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push('/path')
  await router.isReady()
  const w = mount(StageLayout, {
    global: { plugins: [router, i18n] },
    props: { stage: state, store: STORE, tasks: [], hasMore: false, loadingMore: false, openTask: null },
  })
  await flushPromises()
  return w
}

function daysAgo(n: number): string {
  return new Date(Date.now() - n * 24 * 60 * 60 * 1000).toISOString()
}

beforeEach(() => {
  setActivePinia(createPinia())
  const stores = useStoresStore()
  stores.stores = [STORE]
  stores.currentStoreId = 's1'
  stores.loaded = true
})

describe('stages render only the spine plus stat tiles', () => {
  it.each(ALL)('%s renders no task cards of its own outside the feed', async (state) => {
    const w = await mountStage(state)
    // Every .task-card on the page belongs to TaskFeed. With no tasks
    // passed in, there should be none at all.
    expect(w.findAll('.task-card')).toHaveLength(0)
    expect(w.find('.feed-root').exists()).toBe(true)
  })

  it.each([
    ['connector_linked', 0],
    ['connector_linked', 0],
    ['profile_published', 0],
    ['verified', 0],
    ['verified', 0],
    ['active', 2],
  ] as Array<[UCPState, number]>)('%s shows %i stat tile(s)', async (state, count) => {
    const w = await mountStage(state)
    expect(w.findAll('.stat')).toHaveLength(count)
  })
})

describe('Live stage stats', () => {
  it('counts the week\'s orders and totals their value', async () => {
    const orders = useOrdersStore()
    orders.orders = [
      { order_id: 'o1', placed_at: daysAgo(1), value: { amount_minor: 120000, currency: 'USD' } },
      { order_id: 'o2', placed_at: daysAgo(3), value: { amount_minor: 94000, currency: 'USD' } },
      // Outside the window — must not be counted.
      { order_id: 'o3', placed_at: daysAgo(30), value: { amount_minor: 500000, currency: 'USD' } },
    ]
    orders.loaded = true

    const w = await mountStage('active')
    const tiles = w.findAll('.stat')
    expect(tiles[0].find('.stat-value').text()).toBe('2')
    expect(tiles[0].find('.stat-note').text()).toContain('$2,140.00 in agent-driven sales')
  })

  it('gives the all-clear on consistency with the last check time', async () => {
    const health = useHealthStore()
    health.health = {
      statements: [{ kind: 'consistency', ok: true, message: 'Prices match.', checked_at: daysAgo(0) }],
    }
    health.findings = []
    health.loaded = true

    const w = await mountStage('active')
    const tile = w.findAll('.stat')[1]
    expect(tile.find('.stat-value').text()).toBe('In sync')
    expect(tile.find('.stat-note').text()).toContain('Last checked')
  })

  // Same bug as the pre-fix Health page (see HealthSurface.test.ts): a
  // relative-time note computed once from a fixed input never moves again,
  // so "Prices & inventory · Last checked 5 minutes ago" would say that
  // forever, however long the Path stayed open.
  it('advances "Last checked" on the Prices & inventory tile as the page stays open', async () => {
    vi.useFakeTimers()
    try {
      vi.setSystemTime(new Date('2026-09-06T11:20:00.000Z'))

      const health = useHealthStore()
      health.health = {
        statements: [
          { kind: 'consistency', ok: true, message: 'Prices match.', checked_at: '2026-09-06T11:14:41.553Z' },
        ],
      }
      health.findings = []
      health.loaded = true

      const router = createRouter({ history: createMemoryHistory(), routes })
      await router.push('/path')
      await router.isReady()
      const w = mount(StageLayout, {
        global: { plugins: [router, i18n] },
        props: { stage: 'active', store: STORE, tasks: [], hasMore: false, loadingMore: false, openTask: null },
      })
      await vi.advanceTimersByTimeAsync(0)

      const note = () => w.findAll('.stat')[1].find('.stat-note').text()
      expect(note()).toContain('5 minutes ago')

      // Well past useNow's 15s tick, no page interaction, no refetch.
      await vi.advanceTimersByTimeAsync(5 * 60 * 1000)

      expect(note()).toContain('10 minutes ago')
    } finally {
      vi.useRealTimers()
    }
  })

  it('reports drift as a count, pointing at the tasks below', async () => {
    const health = useHealthStore()
    health.health = { statements: [{ kind: 'consistency', ok: false, message: 'Prices differ.' }] }
    health.findings = [
      { finding_id: 'f1', sku: 'HP-1042', field: 'price' },
      { finding_id: 'f2', sku: 'HP-0871', field: 'stock' },
    ]
    health.loaded = true

    const w = await mountStage('active')
    const tile = w.findAll('.stat')[1]
    expect(tile.find('.stat-value').text()).toBe('2')
    expect(tile.find('.stat-value').classes()).toContain('warn')
    expect(tile.find('.stat-label').text()).toBe('Products drifted across sources')
  })

  it('shows zero orders without inventing a number', async () => {
    const w = await mountStage('active')
    expect(w.findAll('.stat')[0].find('.stat-value').text()).toBe('0')
  })
})
