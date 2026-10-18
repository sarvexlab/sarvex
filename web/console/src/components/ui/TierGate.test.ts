// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import TierGate from './TierGate.vue'

async function mountGate(props: { unlocked: boolean; loading?: boolean }) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div/>' } },
      { path: '/settings', component: { template: '<div/>' } },
    ],
  })
  await router.push('/')
  await router.isReady()
  return mount(TierGate, {
    props: {
      lockedTitle: 'Locked title',
      lockedDescription: 'Locked description',
      upgradeCta: 'Upgrade',
      ...props,
    },
    slots: { default: '<div class="secret">Real content</div>' },
    global: { plugins: [router] },
  })
}

describe('TierGate', () => {
  it('shows a skeleton and nothing else while loading, regardless of unlocked', async () => {
    const w = await mountGate({ unlocked: true, loading: true })
    expect(w.find('.secret').exists()).toBe(false)
    expect(w.find('.locked').exists()).toBe(false)
    expect(w.find('.tier-gate-skel').exists()).toBe(true)
  })

  it('renders the default slot when unlocked', async () => {
    const w = await mountGate({ unlocked: true, loading: false })
    expect(w.find('.secret').exists()).toBe(true)
    expect(w.find('.locked').exists()).toBe(false)
  })

  it('renders the locked panel with the upgrade CTA when not unlocked', async () => {
    const w = await mountGate({ unlocked: false, loading: false })
    expect(w.find('.secret').exists()).toBe(false)
    expect(w.text()).toContain('Locked title')
    expect(w.text()).toContain('Locked description')
    const cta = w.find('a.locked-cta')
    expect(cta.exists()).toBe(true)
    expect(cta.text()).toContain('Upgrade')
    expect(cta.attributes('href')).toBe('/settings')
  })
})
