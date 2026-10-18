// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// The score breakdown in the product drawer: the four areas the overall score
// is built from, so "84" is explainable from the page rather than a bare number.

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { routes } from '@/router'
import { i18n } from '@/i18n'
import ProductDrawer from './ProductDrawer.vue'
import { useCatalogStore } from '@/stores/catalog'
import type { components } from '@/api/schema'

type ProductDetail = components['schemas']['product_detail']

function detailWith(dimensions: ProductDetail['dimensions']): ProductDetail {
  return {
    sku: 'SIM-1',
    title: 'Sim Product 1',
    score: 84,
    open_issue_count: 3,
    has_blocking_issue: false,
    dimensions,
    issues: [],
    attribute_mappings: [],
  } as unknown as ProductDetail
}

async function mountDrawer(detail: ProductDetail) {
  setActivePinia(createPinia())
  const catalog = useCatalogStore()
  catalog.detail = detail
  vi.spyOn(catalog, 'loadDetail').mockResolvedValue(undefined)

  const router = createRouter({ history: createMemoryHistory(), routes })
  router.push('/store')
  await router.isReady()
  const w = mount(ProductDrawer, { props: { sku: 'SIM-1' }, global: { plugins: [router, i18n] } })
  await flushPromises()
  return w
}

describe('score breakdown', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('lists each readiness area with its score, in plain English', async () => {
    const w = await mountDrawer(
      detailWith({ completeness: 98, consistency: 100, attribute_quality: 100, delivery_data: 0 }),
    )
    const rows = w.findAll('.card.breakdown .dim')
    expect(rows.map((r) => r.find('.dim-label').text())).toEqual([
      'Complete listings',
      'Consistent data',
      'Attribute quality',
      'Delivery info',
    ])
    expect(rows.map((r) => r.find('.dim-val').text())).toEqual(['98', '100', '100', '0'])
    expect(rows[3].find('.dim-bar i').attributes('style')).toContain('width: 0%')
  })

  it('shows nothing when the product carries no breakdown', async () => {
    const w = await mountDrawer(detailWith(undefined))
    expect(w.find('.card.breakdown').exists()).toBe(false)
  })
})
