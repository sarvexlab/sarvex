// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Sidebar: marks the current page, keeps collapsed icons labelled, and only
// the mobile drawer has a close button (the desktop rail has the collapse
// toggle instead).

import { describe, it, expect } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { routes } from '@/router'
import { i18n } from '@/i18n'
import Sidebar from './Sidebar.vue'

async function mountAt(path: string, props: { variant: 'desktop' | 'mobile'; collapsed?: boolean }) {
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push(path)
  await router.isReady()
  const w = mount(Sidebar, { props, global: { plugins: [router, i18n] } })
  await flushPromises()
  return w
}

describe('Sidebar', () => {
  it('marks only the current page as active', async () => {
    const w = await mountAt('/store?tab=orders', { variant: 'desktop' })
    const current = w.findAll('[aria-current="page"]')
    expect(current).toHaveLength(1)
    expect(current[0].text()).toBe('Orders')
  })

  it('keeps an accessible name on every nav item when collapsed', async () => {
    const w = await mountAt('/analytics', { variant: 'desktop', collapsed: true })
    const items = w.findAll('.sb-list .sb-item')
    expect(items.length).toBeGreaterThan(0)
    for (const i of items) {
      expect(i.text()).toBe('')
      expect(i.attributes('aria-label')).toBeTruthy()
    }
    expect(w.find('.sb-collapse').attributes('aria-label')).toBe('Expand sidebar')
  })

  it('gives the mobile drawer a close button and no collapse toggle', async () => {
    const w = await mountAt('/analytics', { variant: 'mobile', collapsed: true })
    expect(w.find('.sb-collapse').exists()).toBe(false)
    // The drawer never collapses, whatever the desktop preference is.
    expect(w.findAll('.sb-label').length).toBeGreaterThan(0)
    await w.get('button[aria-label="Close menu"]').trigger('click')
    expect(w.emitted('close')).toHaveLength(1)
  })

  it('renders the collapser icon-only, with its action as the accessible name', async () => {
    const w = await mountAt('/analytics', { variant: 'desktop' })
    const btn = w.get('.sb-collapse')
    expect(btn.text()).toBe('')
    expect(btn.attributes('aria-label')).toBe('Collapse sidebar')
  })

  it('hides the collapser when the rail cannot be toggled (tablet widths)', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes })
    await router.push('/analytics')
    await router.isReady()
    const w = mount(Sidebar, {
      props: { variant: 'desktop', collapsed: true, collapsible: false },
      global: { plugins: [router, i18n] },
    })
    expect(w.find('.sb-collapse').exists()).toBe(false)
  })

  // Health left the nav before its fold into Home existed, which made the
  // only place to acknowledge/resolve alerts unreachable. Keep it until then.
  it('links to Health and marks it active there', async () => {
    const w = await mountAt('/health', { variant: 'desktop' })
    const current = w.findAll('[aria-current="page"]')
    expect(current).toHaveLength(1)
    expect(current[0].text()).toBe('Health')
  })
})
