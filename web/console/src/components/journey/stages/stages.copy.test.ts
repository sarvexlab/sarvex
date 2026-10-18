// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Pins each stage's headline and subhead. This content is genuinely
// different per stage, so it's asserted per stage rather than parameterized.
//
// Worth pinning literally: when the copy lived in six separate components,
// checklist_complete shipped a headline copied from the Ready stage and a
// subhead that was placeholder text from the design mockup, and no test
// noticed because each file was asserted against itself.

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { routes } from '@/router'
import { i18n } from '@/i18n'
import StageLayout from './StageLayout.vue'
import { useCatalogStore } from '@/stores/catalog'
import { useStoresStore } from '@/stores/stores'
import type { components } from '@/api/schema'

vi.mock('@/api/catalog', () => ({
  getReadiness: vi.fn(),
  listProducts: vi.fn(),
}))
import { getReadiness, listProducts } from '@/api/catalog'

type Task = components['schemas']['task']
type Store = components['schemas']['store']
type UCPState = components['schemas']['ucp_state']

const store: Store = { store_id: 's1', name: 'Acme', platform: 'woocommerce' } as Store

type Props = {
  store: Store | undefined
  tasks: Task[]
  hasMore: boolean
  loadingMore: boolean
  openTask: Task | null
}

const READINESS = {
  overall: 74,
  product_count: 340,
  dimensions: { completeness: 80, consistency: 70, attribute_quality: 75, delivery_data: 72 },
  last_audit_at: '2026-07-01T00:00:00Z',
}

async function mountStage(stage: UCPState, props: Partial<Props> = {}) {
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push('/path')
  await router.isReady()
  return mount(StageLayout, {
    global: { plugins: [router, i18n] },
    props: {
      stage,
      store,
      tasks: [],
      hasMore: false,
      loadingMore: false,
      openTask: null,
      ...props,
    },
  })
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(getReadiness).mockReset()
  vi.mocked(listProducts).mockReset()
})

describe('draft copy', () => {
  it('asks for the one thing that unblocks everything else', async () => {
    const w = await mountStage('draft')
    expect(w.find('h1').text()).toBe("Let's get your store connected.")
    expect(w.find('p.page-sub').text()).toContain('Connect your commerce backend')
  })
})

describe('connector_linked copy', () => {
  it('shows a placeholder product count before the catalog store loads', async () => {
    const w = await mountStage('connector_linked')
    expect(w.find('h1').text()).toBe("You're connected. … products are coming in.")
  })

  it('shows the loaded product count from the catalog store', async () => {
    useCatalogStore().readiness = READINESS
    const w = await mountStage('connector_linked')
    expect(w.find('h1').text()).toBe("You're connected. 340 products are coming in.")
    expect(w.find('p.page-sub').text()).toContain('scoring every product')
  })
})

describe('profile_published copy', () => {
  it('shows a placeholder score before the catalog store loads', async () => {
    const w = await mountStage('profile_published')
    expect(w.find('h1').text()).toBe(
      "You're visible to agents — you scored …. Here's what's left.",
    )
  })

  it('shows the loaded readiness score from the catalog store', async () => {
    useCatalogStore().readiness = READINESS
    const w = await mountStage('profile_published')
    expect(w.find('h1').text()).toBe(
      "You're visible to agents — you scored 74. Here's what's left.",
    )
  })

  it('loads the catalog store once mounted for the current store', async () => {
    const stores = useStoresStore()
    stores.stores = [store]
    stores.currentStoreId = store.store_id!
    vi.mocked(getReadiness).mockResolvedValue({ ...READINESS, overall: 91, product_count: 12 })
    vi.mocked(listProducts).mockResolvedValue({ products: [], cursor: null, hasMore: false })

    const w = await mountStage('profile_published')
    await flushPromises()

    expect(getReadiness).toHaveBeenCalledWith(store.store_id)
    expect(w.find('h1').text()).toBe(
      "You're visible to agents — you scored 91. Here's what's left.",
    )
  })
})

describe('verified copy', () => {
  // This stage used to be "checklist_complete", and said Google was testing
  // the merchant's checkout. It is now about the merchant's own endpoints:
  // Sarvex ran the conformance suite against them and they passed.
  it('describes the store\'s own endpoints, not a third party\'s verdict', async () => {
    const w = await mountStage('verified')
    expect(w.find('h1').text()).toBe('Your endpoints check out. Turning on monitoring.')

    const sub = w.find('p.page-sub').text()
    expect(sub).toContain('against your own endpoints')
    expect(sub).not.toMatch(/google/i)
  })
})

describe('active copy', () => {
  // The end of the Path, and the assertion that matters most: a store is
  // fully active on its own endpoints, with no distribution partner named.
  it('reads as a completion and names no surface', async () => {
    const w = await mountStage('active')
    expect(w.find('h1').text()).toBe('Acme is agent-ready. Agents can buy from you.')

    const sub = w.find('p.page-sub').text()
    expect(sub).toContain('live, verified and monitored')
    expect(sub).not.toMatch(/google/i)
  })
})

describe('suspended copy', () => {
  // Says why the store is suspended and points at the tasks, without
  // mentioning surfaces: enrollment is a later feature, and copy that
  // reassures a merchant about something they have never seen only raises
  // the question of what it is.
  it('says the store is suspended and points at the tasks', async () => {
    const w = await mountStage('suspended')
    expect(w.find('h1').text()).toBe('Acme is suspended.')
    const sub = w.find('p.page-sub').text()
    expect(sub).toContain('The tasks below say which')
    expect(sub).not.toMatch(/surface/i)
  })
})

describe("the feed's empty state", () => {
  // One hardcoded line used to tell every merchant the next milestone was
  // sandbox validation — including merchants already live.
  it('speaks to the stage the merchant is in', async () => {
    const connected = await mountStage('connector_linked')
    expect(connected.find('.empty-note').text()).toContain('Your import is running')

    const active = await mountStage('active')
    const activeNote = active.find('.empty-note').text()
    expect(activeNote).toContain("You're active and nothing needs you")
    expect(activeNote).not.toContain('sandbox validation')
  })
})
