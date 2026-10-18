// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// The Path's body is one component now (StageLayout), driven by a row per
// ucp_state. These assertions — load-more, TaskFeed forwarding,
// TaskDrawer forwarding — were always structurally identical across the six
// stages, which is what made collapsing them safe. They stay parameterized
// over every stage so that a row which somehow renders differently is
// caught.

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { routes } from '@/router'
import { i18n } from '@/i18n'
import StageLayout from './StageLayout.vue'
import type { components } from '@/api/schema'

type Task = components['schemas']['task']
type Store = components['schemas']['store']
type UCPState = components['schemas']['ucp_state']

vi.mock('@/api/tasks', () => ({ getTask: vi.fn() }))
vi.mock('@/api/remediations', () => ({
  applyRemediations: vi.fn(),
  revertWriteBack: vi.fn(),
}))
import { getTask } from '@/api/tasks'

const STAGE_ORDER: UCPState[] = [
  'draft',
  'connector_linked',
  'profile_published',
  'verified',
  'active',
]

const store: Store = { store_id: 's1', name: 'Acme', platform: 'woocommerce' } as Store

const task = (over: Partial<Task> = {}): Task => ({
  task_id: 'task-1',
  store_id: 's1',
  title: 'Fix product color',
  why_it_matters: 'Blocks checkout.',
  owner: 'merchant',
  resolution: 'one_click',
  state: 'open',
  blocks_checkout: true,
  source: 'readiness',
  source_ref: 'sku-1',
  ...over,
})

type Props = {
  store: Store | undefined
  tasks: Task[]
  hasMore: boolean
  loadingMore: boolean
  openTask: Task | null
}

async function mountStage(state: UCPState, props: Partial<Props> = {}) {
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push('/path')
  await router.isReady()
  return mount(StageLayout, {
    global: { plugins: [router, i18n] },
    props: {
      stage: state,
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
  vi.mocked(getTask).mockReset()
})

describe.each(STAGE_ORDER)('%s stage', (state) => {
  it('hides load-more when hasMore is false, shows and emits when true', async () => {
    const hidden = await mountStage(state, { hasMore: false })
    expect(hidden.find('.load-more-btn').exists()).toBe(false)

    const shown = await mountStage(state, { hasMore: true })
    const btn = shown.find('.load-more-btn')
    expect(btn.exists()).toBe(true)
    await btn.trigger('click')
    expect(shown.emitted('loadMore')).toHaveLength(1)
  })

  it('disables load-more while loadingMore', async () => {
    const w = await mountStage(state, { hasMore: true, loadingMore: true })
    const btn = w.find('.load-more-btn')
    expect(btn.attributes('disabled')).toBeDefined()
    expect(btn.text()).toBe('Loading…')
  })

  it('forwards TaskFeed opens as its own open emit', async () => {
    const t = task()
    const w = await mountStage(state, { tasks: [t] })
    await w.find('.feed-root .task-card').trigger('click')
    expect(w.emitted('open')?.[0]).toEqual([t])
  })

  it('renders TaskDrawer when openTask is set, and forwards its close emit', async () => {
    const t = task()
    vi.mocked(getTask).mockResolvedValue(t)
    const w = await mountStage(state, { openTask: t })
    expect(w.find('.drawer').exists()).toBe(true)

    await w.find('.icon-btn[aria-label="Close"]').trigger('click')
    expect(w.emitted('close')).toHaveLength(1)
  })

  it('does not render TaskDrawer when openTask is null', async () => {
    const w = await mountStage(state, { openTask: null })
    expect(w.find('.drawer').exists()).toBe(false)
  })
})
