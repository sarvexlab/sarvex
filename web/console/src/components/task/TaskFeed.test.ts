// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// The feed's dispatch rule: named tasks render through their widget,
// everything else through the generic TaskCard. Either way the feed shows
// every task and nothing but tasks — the spine is the whole content.

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { routes } from '@/router'
import { i18n } from '@/i18n'
import TaskFeed from './TaskFeed.vue'
import TaskCard from './TaskCard.vue'
import CatalogAuditTask from './widgets/CatalogAuditTask.vue'
import { useStoresStore } from '@/stores/stores'
import { useCatalogStore } from '@/stores/catalog'
import type { components } from '@/api/schema'

type Task = components['schemas']['task']

vi.mock('@/api/catalog', () => ({
  getReadiness: vi.fn(async () => ({})),
  listProducts: vi.fn(async () => ({ products: [], cursor: null, hasMore: false })),
  getProduct: vi.fn(),
}))

const namedWidgetTask = (): Task => ({
  task_id: 'readiness:s1:initial_audit',
  store_id: 's1',
  title: 'Auditing your catalog',
  owner: 'sarvex',
  resolution: 'automatic',
  state: 'in_progress',
  source: 'readiness',
  source_ref: 'initial_audit',
})

const dynamic = (over: Partial<Task> = {}): Task => ({
  task_id: 'readiness:s1:HP-1042',
  store_id: 's1',
  title: 'Add a size to Channel-knit fisherman sweater',
  why_it_matters: "Google won't show a Buy button until it has a size.",
  owner: 'merchant',
  resolution: 'guided',
  state: 'open',
  source: 'readiness',
  source_ref: 'HP-1042',
  ...over,
})

type FeedProps = InstanceType<typeof TaskFeed>['$props']

async function mountFeed(props: FeedProps) {
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push('/path')
  await router.isReady()
  const w = mount(TaskFeed, { global: { plugins: [router, i18n] }, props })
  await flushPromises()
  return w
}

beforeEach(() => {
  setActivePinia(createPinia())
  const stores = useStoresStore()
  stores.stores = [{ store_id: 's1', name: 'Harbor & Pine', platform: 'woocommerce' } as never]
  stores.currentStoreId = 's1'
  stores.loaded = true

  // Preset rather than let the widget's self-load hit the (mocked) API —
  // matches the pattern in widgets.test.ts.
  const catalog = useCatalogStore()
  catalog.readiness = { overall: 74 }
  catalog.storeId = 's1'
})

describe('TaskFeed dispatch', () => {
  it('renders a named task through its widget', async () => {
    const w = await mountFeed({ tasks: [namedWidgetTask()] })
    expect(w.findComponent(CatalogAuditTask).exists()).toBe(true)
  })

  it('renders a dynamically-created task through the generic card', async () => {
    const w = await mountFeed({ tasks: [dynamic()] })
    expect(w.findComponent(CatalogAuditTask).exists()).toBe(false)
    expect(w.findComponent(TaskCard).exists()).toBe(true)
    expect(w.find('.task-title').text()).toBe('Add a size to Channel-knit fisherman sweater')
  })

  it('shows every task it is given — nothing is filtered out', async () => {
    const w = await mountFeed({ tasks: [namedWidgetTask(), dynamic()] })
    const titles = w.findAll('.task-title').map((n) => n.text())
    expect(titles).toEqual([
      'Add a size to Channel-knit fisherman sweater',
      'Auditing your catalog',
    ])
  })

  it('forwards a widget open the same way it forwards a card open', async () => {
    const t = namedWidgetTask()
    const w = await mountFeed({ tasks: [t] })
    await w.find('.task-card').trigger('click')
    expect(w.emitted('open')?.[0]).toEqual([t])
  })

  it('falls back to the generic card in multi-store contexts', async () => {
    // Widgets self-load against the *current* store, so a board row that
    // belongs to another store must not be specialised.
    const w = await mountFeed({ tasks: [namedWidgetTask()], storeName: 'Bayside Trail' })
    expect(w.findComponent(CatalogAuditTask).exists()).toBe(false)
    expect(w.find('.store-tag').text()).toContain('Bayside Trail')
  })

  it('still groups by owner and collapses done', async () => {
    const w = await mountFeed({
      tasks: [
        dynamic(),
        dynamic({ task_id: 'r2', owner: 'sarvex', state: 'in_progress' }),
        dynamic({ task_id: 'r3', owner: 'external', state: 'blocked' }),
        dynamic({ task_id: 'r4', state: 'done' }),
      ],
    })
    const labels = w.findAll('.section-label').map((n) => n.text())
    expect(labels[0]).toContain('Needs you now · 1')
    expect(labels[1]).toContain("What we're doing for you · 1")
    expect(labels[2]).toContain('Waiting on an external party · 1')
    expect(labels[3]).toContain('Done · 1')
    // Done stays collapsed until toggled.
    expect(w.findAll('.task-card')).toHaveLength(3)
  })
})
