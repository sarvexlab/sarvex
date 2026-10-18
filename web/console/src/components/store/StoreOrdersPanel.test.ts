// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// The empty state must not claim a live store "isn't live for agents yet".

import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { i18n } from '@/i18n'
import StoreOrdersPanel from './StoreOrdersPanel.vue'
import { useStoresStore } from '@/stores/stores'
import { useOrdersStore } from '@/stores/orders'
import type { components } from '@/api/schema'

type Store = components['schemas']['store']

function mountPanel(ucpState: string) {
  setActivePinia(createPinia())
  const storesStore = useStoresStore()
  const store = {
    store_id: 'store-1',
    name: 'Acme Tea',
    platform: 'woocommerce',
    ucp_state: ucpState,
  } as unknown as Store
  storesStore.stores = [store]
  storesStore.currentStoreId = 'store-1'

  const orders = useOrdersStore()
  orders.orders = []
  orders.loaded = true

  return mount(StoreOrdersPanel, { global: { plugins: [i18n] } })
}

describe('StoreOrdersPanel — empty state', () => {
  it('does not say a live store is not live', () => {
    const text = mountPanel('active').text()
    expect(text).toContain('No agent has checked out from Acme Tea yet')
    expect(text).not.toContain("isn't live")
  })

  it('says the store is not live while it is not UCP-active', () => {
    const text = mountPanel('draft').text()
    expect(text).toContain("Acme Tea isn't live for agents yet")
  })
})
