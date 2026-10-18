// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Each named widget renders one milestone task, enriched with the domain
// data the spine has no field for. These pin that split: the task supplies
// owner/state (and usually the copy), the widget adds the number.

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { i18n } from '@/i18n'
import type { Component } from 'vue'

import { useStoresStore } from '@/stores/stores'
import { useConnectorsStore } from '@/stores/connectors'
import { useCatalogStore } from '@/stores/catalog'
import { useValidationStore } from '@/stores/validation'

import CatalogSyncTask from './CatalogSyncTask.vue'
import CatalogAuditTask from './CatalogAuditTask.vue'
import PreValidationTask from './PreValidationTask.vue'

import type { components } from '@/api/schema'

type Task = components['schemas']['task']
type Store = components['schemas']['store']

vi.mock('@/api/connectors', () => ({ listConnectors: vi.fn(async () => []), createConnector: vi.fn(), getConnector: vi.fn() }))
vi.mock('@/api/catalog', () => ({ getReadiness: vi.fn(async () => ({})), listProducts: vi.fn(async () => ({ products: [], cursor: null, hasMore: false })), getProduct: vi.fn() }))
vi.mock('@/api/validation', () => ({ getValidationStatus: vi.fn(async () => ({})), listValidationRuns: vi.fn(async () => []), triggerValidationRun: vi.fn() }))

import { getReadiness } from '@/api/catalog'

const STORE: Store = { store_id: 's1', name: 'Harbor & Pine', platform: 'woocommerce' } as Store

const task = (over: Partial<Task>): Task => ({
  task_id: 't',
  store_id: 's1',
  title: 'A milestone',
  owner: 'sarvex',
  resolution: 'automatic',
  state: 'in_progress',
  source: 'readiness',
  source_ref: 'initial_audit',
  ...over,
})

// The journey's milestones, verbatim from milestones.go.
const importTask = (over: Partial<Task> = {}) =>
  task({
    task_id: 'catalog_import:s1:initial_import',
    title: 'Importing your catalog',
    why_it_matters: "We're reading your products out of your store so agents can see them. Nothing needed from you.",
    source: 'catalog_import',
    source_ref: 'initial_import',
    ...over,
  })

const auditTask = (over: Partial<Task> = {}) =>
  task({
    task_id: 'readiness:s1:initial_audit',
    title: 'Auditing your catalog',
    why_it_matters: 'Checking completeness, identifiers, categories, and policy facts across every product.',
    ...over,
  })

const preValidationTask = (over: Partial<Task> = {}) =>
  task({
    task_id: 'validation:s1:run',
    title: 'Run pre-validation',
    why_it_matters: "Every store must pass Sarvex's internal pre-validation suite before attempting Google's sandbox.",
    resolution: 'one_click',
    source_ref: 'run',
    ...over,
  })

const preValidationFailureTask = (over: Partial<Task> = {}) =>
  task({
    task_id: 'validation:s1:pre_validation_failed',
    title: 'Pre-validation checks failed',
    why_it_matters: 'The store must pass pre-validation checks before going live.',
    owner: 'merchant',
    resolution: 'guided',
    state: 'open',
    source_ref: 'pre_validation_failed',
    ...over,
  })

async function mountWidget(component: Component, t: Task) {
  const w = mount(component, { global: { plugins: [i18n] }, props: { task: t } })
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

describe('CatalogSyncTask', () => {
  it('fills the product count into the spine copy while importing', async () => {
    useCatalogStore().readiness = { product_count: 1840 }
    useCatalogStore().storeId = 's1'

    const w = await mountWidget(CatalogSyncTask, importTask())
    expect(w.find('.task-title').text()).toBe('Importing your catalog')
    expect(w.find('.task-why').text()).toBe('Reading all 1,840 products from woocommerce.')
    // No import-progress endpoint exists, so the bar must be indeterminate.
    expect(w.find('.pbar').exists()).toBe(true)
    expect(w.find('.pbar > i').attributes('style')).toBeUndefined()
  })

  it('falls back to the spine copy before the catalog has loaded', async () => {
    const w = await mountWidget(CatalogSyncTask, importTask())
    expect(w.find('.task-why').text()).toContain("We're reading your products")
  })

  // Regression: readiness == null used to be the only "already loaded, skip
  // the fetch" guard. True the first time a store's Path is opened, false
  // forever after — including once the merchant switches to a *different*
  // store, so a brand-new store with zero products rendered the previous
  // store's leftover product count ("Reading all 200 products…") instead of
  // refetching for itself.
  it('refetches instead of showing a previous store\'s leftover product count', async () => {
    useCatalogStore().readiness = { product_count: 200 }
    useCatalogStore().storeId = 'store-old'
    vi.mocked(getReadiness).mockResolvedValueOnce({})

    const w = await mountWidget(CatalogSyncTask, importTask())

    expect(getReadiness).toHaveBeenCalledWith('s1')
    expect(w.find('.task-why').text()).not.toContain('200')
    expect(w.find('.task-why').text()).toContain("We're reading your products")
  })

  it('retitles the resolved milestone with the count the design asks for', async () => {
    useCatalogStore().readiness = { product_count: 1840 }
    useCatalogStore().storeId = 's1'
    useConnectorsStore().connectors = [
      { connector_id: 'c1', store_id: 's1', status: 'healthy', last_full_sync_at: daysAgo(1) },
    ]

    const w = await mountWidget(CatalogSyncTask, importTask({ state: 'done' }))
    expect(w.find('.task-title').text()).toBe('First catalog sync — 1,840 products')
    expect(w.find('.task-card').classes()).toContain('is-done')
  })

  it('renders an import failure from its own task, keyed on owner not state', async () => {
    const w = await mountWidget(
      CatalogSyncTask,
      importTask({
        title: "We couldn't reach your store",
        why_it_matters: 'Your store credentials stopped working.',
        owner: 'merchant',
        state: 'open',
        source_ref: 'initial',
      }),
    )
    expect(w.find('.task-title').text()).toBe("We couldn't reach your store")
    expect(w.text()).toContain('Import failed')
  })
})

describe('CatalogAuditTask', () => {
  it('shows the spine copy and an indeterminate bar while auditing', async () => {
    const w = await mountWidget(CatalogAuditTask, auditTask())
    expect(w.find('.task-title').text()).toBe('Auditing your catalog')
    expect(w.find('.task-why').text()).toContain('Checking completeness, identifiers')
    expect(w.find('.pbar').exists()).toBe(true)
  })

  it('adds the score once the milestone resolves', async () => {
    useCatalogStore().readiness = { overall: 74, last_audit_at: daysAgo(1) }
    useCatalogStore().storeId = 's1'

    const w = await mountWidget(CatalogAuditTask, auditTask({ state: 'done' }))
    expect(w.find('.task-title').text()).toBe('Readiness audit complete — scored 74')
    expect(w.text()).toContain('74 / 100')
  })

  it('still resolves cleanly when readiness has not loaded', async () => {
    const w = await mountWidget(CatalogAuditTask, auditTask({ state: 'done' }))
    expect(w.find('.task-title').text()).toBe('Readiness audit complete')
  })
})

describe('PreValidationTask', () => {
  it('falls back to the spine copy before any run has happened', async () => {
    const w = await mountWidget(PreValidationTask, preValidationTask())
    expect(w.find('.task-title').text()).toBe('Run pre-validation')
    expect(w.text()).toContain('Not run yet')
  })

  it('reads as passed once a run comes back clean', async () => {
    const validation = useValidationStore()
    validation.status = { pre_validation: 'passing', last_run_at: daysAgo(0) }
    validation.loaded = true

    const w = await mountWidget(PreValidationTask, preValidationTask({ state: 'done' }))
    expect(w.find('.task-title').text()).toBe('Pre-validation passed')
    expect(w.text()).toContain('Passed')
  })

  it('shows the failure task with its own spine copy, still failed', async () => {
    const validation = useValidationStore()
    validation.status = { pre_validation: 'failing', last_run_at: daysAgo(0) }
    validation.loaded = true

    const w = await mountWidget(PreValidationTask, preValidationFailureTask())
    expect(w.find('.task-title').text()).toBe('Pre-validation checks failed')
    expect(w.text()).toContain('Failed')
  })
})

describe('widget resilience', () => {
  it('renders from the task alone when the enrichment endpoint fails', async () => {
    const { getReadiness } = await import('@/api/catalog')
    vi.mocked(getReadiness).mockRejectedValueOnce(new Error('boom'))

    // No throw, no unhandled rejection — the Path keeps working and the
    // spine's own copy carries the card.
    const w = await mountWidget(CatalogAuditTask, auditTask())
    expect(w.find('.task-title').text()).toBe('Auditing your catalog')
  })
})
