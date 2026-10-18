// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { i18n } from '@/i18n'
import ProductDemandSection from './ProductDemandSection.vue'
import type { ProductDemand } from '@/api/insights'

function mountSection(rows: ProductDemand[], extra: Record<string, unknown> = {}) {
  return mount(ProductDemandSection, {
    props: { rows, loading: false, error: null, limit: 100, ...extra },
    global: { plugins: [i18n] },
  })
}

describe('ProductDemandSection — counts only (R12)', () => {
  it('renders product_id, request_count, and a per-surface breakdown', () => {
    const rows: ProductDemand[] = [
      { product_id: 'SKU-1', request_count: 40, surface_breakdown: { google_ai_mode: 30, google_gemini: 10 } },
    ]
    const w = mountSection(rows)
    expect(w.text()).toContain('SKU-1')
    expect(w.text()).toContain('40')
    expect(w.text()).toContain('30')
    expect(w.text()).toContain('10')
  })

  it('explains that reached-cart/completed counts are not available, rather than showing a fabricated rate', () => {
    const rows: ProductDemand[] = [{ product_id: 'SKU-1', request_count: 40, surface_breakdown: {} }]
    const w = mountSection(rows)
    expect(w.text()).toMatch(/cart\/completion rates aren.t available|correlate a catalog request/i)
    expect(w.text()).not.toMatch(/%/)
  })
})

describe('ProductDemandSection — top-N selector', () => {
  it('emits update:limit when the selector changes', async () => {
    const w = mountSection([{ product_id: 'SKU-1', request_count: 40, surface_breakdown: {} }])
    await w.find('.limit-field select').setValue('25')
    expect(w.emitted('update:limit')?.[0]).toEqual([25])
  })
})

describe('ProductDemandSection — empty/loading/error (R15)', () => {
  it('shows an empty state when there are no rows', () => {
    const w = mountSection([])
    expect(w.text()).toContain('No product demand recorded')
  })

  it('shows a skeleton while loading', () => {
    const w = mountSection([], { loading: true })
    expect(w.find('.skel-block').exists()).toBe(true)
  })

  it('shows the error banner with a retry that re-emits', async () => {
    const w = mountSection([], { error: 'network down' })
    const banner = w.find('.err')
    expect(banner.exists()).toBe(true)
    await banner.find('.btn').trigger('click')
    expect(w.emitted('retry')).toBeTruthy()
  })
})
