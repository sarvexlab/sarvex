// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// The attribute-mapping section of the product drawer.
//
// The resolve stage emits one entry per catalogue target attribute, so most
// entries on a sparse product are tier=unresolved with an empty source and
// zero confidence. The drawer rendered the tier chip as a binary — rule, or
// else "Learned" — so every one of those read "Learned · Confidence 0%",
// telling the merchant Sarvex had inferred a mapping it had in fact failed to
// find. That is worse than showing nothing: 0% confidence in a learned
// mapping and "no mapping exists" are opposite facts.

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

const detail = {
  sku: 'SIM-1',
  title: 'Sim Product 1',
  score: 84,
  open_issue_count: 0,
  has_blocking_issue: false,
  issues: [],
  attribute_mappings: [
    { source_attribute: '', catalog_attribute: 'age_group', tier: 'unresolved', confidence: 0 },
    { source_attribute: 'Marke', catalog_attribute: 'brand', tier: 'learned', confidence: 0.91 },
    { source_attribute: 'Weight', catalog_attribute: 'shipping_weight', tier: 'rule', confidence: 1 },
  ],
} as unknown as ProductDetail

async function mountDrawer() {
  const router = createRouter({ history: createMemoryHistory(), routes })
  router.push('/store')
  await router.isReady()

  const w = mount(ProductDrawer, {
    props: { sku: 'SIM-1' },
    global: { plugins: [router, i18n] },
  })
  await flushPromises()
  return w
}

beforeEach(() => {
  setActivePinia(createPinia())
  const catalog = useCatalogStore()
  catalog.detail = detail
  vi.spyOn(catalog, 'loadDetail').mockResolvedValue(undefined)
})

describe('attribute mappings', () => {
  it('never labels an unresolved attribute as learned', async () => {
    const w = await mountDrawer()
    const rows = w.findAll('.card.list')[0].findAll('.row')
    const unresolved = rows.find((r) => r.text().includes('age_group'))!

    expect(unresolved.text()).toContain('Not mapped')
    expect(unresolved.text()).not.toContain('Learned')
  })

  it('does not report a confidence for an attribute nothing resolved onto', async () => {
    const w = await mountDrawer()
    const rows = w.findAll('.card.list')[0].findAll('.row')
    const unresolved = rows.find((r) => r.text().includes('age_group'))!

    expect(unresolved.text()).not.toContain('Confidence')
    expect(unresolved.text()).not.toContain('0%')
  })

  it('says which side is missing rather than leaving it blank', async () => {
    const w = await mountDrawer()
    const unresolved = w
      .findAll('.card.list')[0]
      .findAll('.row')
      .find((r) => r.text().includes('age_group'))!

    expect(unresolved.find('.unmapped').text()).toBe('Missing attribute')
  })

  it('keeps tier and confidence for mappings that did resolve', async () => {
    const w = await mountDrawer()
    const rows = w.findAll('.card.list')[0].findAll('.row')

    const learned = rows.find((r) => r.text().includes('brand'))!
    expect(learned.text()).toContain('Learned')
    expect(learned.text()).toContain('91%')

    const rule = rows.find((r) => r.text().includes('shipping_weight'))!
    expect(rule.text()).toContain('Rule')
    expect(rule.text()).toContain('100%')
  })

  // Unresolved entries are the bulk of a sparse product; leading with them
  // would bury the mappings that worked.
  it('orders resolved mappings above unresolved ones', async () => {
    const w = await mountDrawer()
    const order = w
      .findAll('.card.list')[0]
      .findAll('.row')
      .map((r) => r.find('.mono').text())

    expect(order).toEqual(['shipping_weight', 'brand', 'age_group'])
  })
})
