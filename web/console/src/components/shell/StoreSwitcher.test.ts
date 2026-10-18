// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// StoreSwitcher, rebuilt to match the reference design ("Sarvex Console
// (standalone)-latest.html"): single store stays a plain label (no picker
// chrome for nothing to pick between); multiple stores get a real popover
// — an "All stores" link to the account home, plus each store's name,
// domain, activation stage, and a status badge (blocked / open-task count /
// checkmark).

import { describe, it, expect, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { mount, flushPromises } from '@vue/test-utils'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { routes } from '@/router'
import { i18n } from '@/i18n'
import StoreSwitcher from './StoreSwitcher.vue'
import { useStoresStore } from '@/stores/stores'
import { useAccountStore } from '@/stores/account'

type Store = ReturnType<typeof useStoresStore>['stores'][number]

const store = (over: Partial<Store>): Store =>
  ({
    store_id: 's1',
    name: 'Acme',
    platform: 'woocommerce',
    domain: 'acme.test',
    ucp_state: 'connector_linked',
    open_task_count: 0,
    blocked: false,
    ...over,
  }) as Store

// account-home is a lazy route (dynamic import); its chunk resolution takes
// an unpredictable number of extra ticks beyond flushPromises' microtask
// flush, so poll instead of guessing a fixed delay.
async function waitForRouteName(router: Router, name: string, timeoutMs = 1000) {
  const start = Date.now()
  while (router.currentRoute.value.name !== name) {
    if (Date.now() - start > timeoutMs) {
      throw new Error(`timed out waiting for route "${name}", still on "${String(router.currentRoute.value.name)}"`)
    }
    await new Promise((r) => setTimeout(r, 10))
  }
}

async function mountSwitcher(): Promise<{ wrapper: ReturnType<typeof mount>; router: Router }> {
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push('/path')
  await router.isReady()
  const wrapper = mount(StoreSwitcher, { global: { plugins: [router, i18n] } })
  return { wrapper, router }
}

beforeEach(() => {
  setActivePinia(createPinia())
})

describe('StoreSwitcher — single store', () => {
  it('renders a static label with no trigger button', async () => {
    const stores = useStoresStore()
    stores.stores = [store({})]
    stores.currentStoreId = 's1'

    const { wrapper } = await mountSwitcher()
    expect(wrapper.find('button.store-switch').exists()).toBe(false)
    expect(wrapper.find('.store-switch.static').exists()).toBe(true)
    expect(wrapper.text()).toContain('Acme')
  })
})

describe('StoreSwitcher — multiple stores', () => {
  function seed() {
    const stores = useStoresStore()
    stores.stores = [
      store({ store_id: 's1', name: 'Acme', domain: 'acme.test', ucp_state: 'connector_linked' }),
      store({ store_id: 's2', name: 'Acme NZ', domain: 'acme.co.nz', ucp_state: 'profile_published', open_task_count: 3 }),
      store({ store_id: 's3', name: 'Acme UK', domain: 'acme.co.uk', ucp_state: 'verified', blocked: true }),
    ]
    stores.currentStoreId = 's1'
    return stores
  }

  it('shows the current store name on a real trigger button, menu closed by default', async () => {
    seed()
    const { wrapper } = await mountSwitcher()
    const trigger = wrapper.find('button.store-switch')
    expect(trigger.exists()).toBe(true)
    expect(trigger.text()).toContain('Acme')
    expect(wrapper.find('.ss-menu').exists()).toBe(false)
  })

  it('opens the menu on click, listing every store with domain and stage', async () => {
    seed()
    const { wrapper } = await mountSwitcher()
    await wrapper.find('button.store-switch').trigger('click')

    const menu = wrapper.find('.ss-menu')
    expect(menu.exists()).toBe(true)
    const rows = wrapper.findAll('.ss-item.ss-store')
    expect(rows).toHaveLength(3)
    expect(rows[1].text()).toContain('Acme NZ')
    expect(rows[1].text()).toContain('acme.co.nz')
    expect(rows[1].text()).toContain('Published')
  })

  it('shows a Blocked chip, an open-task count, or a checkmark per store', async () => {
    seed()
    const { wrapper } = await mountSwitcher()
    await wrapper.find('button.store-switch').trigger('click')

    const rows = wrapper.findAll('.ss-item.ss-store')
    expect(rows[0].find('.ss-count.ok').exists()).toBe(true) // Acme: clear
    expect(rows[1].text()).toContain('3 for you') // Acme NZ: open tasks
    expect(rows[2].text()).toContain('Blocked') // Acme UK: blocked
    expect(rows[2].classes()).toContain('blocked')
  })

  it('picking a store selects it and closes the menu', async () => {
    const stores = seed()
    const { wrapper } = await mountSwitcher()
    await wrapper.find('button.store-switch').trigger('click')

    const rows = wrapper.findAll('.ss-item.ss-store')
    await rows[1].trigger('click')

    expect(stores.currentStoreId).toBe('s2')
    expect(wrapper.find('.ss-menu').exists()).toBe(false)
  })

  it('the "All stores" row navigates to the account home and closes the menu', async () => {
    seed()
    const { wrapper, router } = await mountSwitcher()
    await wrapper.find('button.store-switch').trigger('click')

    await wrapper.find('.ss-item.ss-home').trigger('click')
    await waitForRouteName(router, 'account-home')
    await flushPromises()

    expect(router.currentRoute.value.name).toBe('account-home')
    expect(wrapper.find('.ss-menu').exists()).toBe(false)
  })

  it('closes the menu on Escape', async () => {
    seed()
    const { wrapper } = await mountSwitcher()
    await wrapper.find('button.store-switch').trigger('click')
    expect(wrapper.find('.ss-menu').exists()).toBe(true)

    await window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await wrapper.vm.$nextTick()

    expect(wrapper.find('.ss-menu').exists()).toBe(false)
  })

  it('closes the menu when the backdrop is clicked', async () => {
    seed()
    const { wrapper } = await mountSwitcher()
    await wrapper.find('button.store-switch').trigger('click')

    await wrapper.find('.ss-backdrop').trigger('click')

    expect(wrapper.find('.ss-menu').exists()).toBe(false)
  })

  it('shows the merchant name and store count in the menu header', async () => {
    seed()
    const account = useAccountStore()
    account.account = { merchant_id: 'm1', name: 'Acme Group', timezone: 'UTC' } as never

    const { wrapper } = await mountSwitcher()
    await wrapper.find('button.store-switch').trigger('click')

    expect(wrapper.find('.ss-acct').text()).toContain('Acme Group')
    expect(wrapper.find('.ss-acct-n').text()).toBe('3 stores')
  })
})
