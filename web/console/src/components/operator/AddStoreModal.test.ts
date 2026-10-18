// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// The operator's add-store form is the only place a store's platform is
// chosen (ConnectSurface and Settings read whatever the server recorded).
// Confirms the platform picker defaults to WooCommerce but lets the
// operator provision a Magento store, and that the chosen platform reaches
// operator.createStore verbatim.

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { i18n } from '@/i18n'
import AddStoreModal from './AddStoreModal.vue'

vi.mock('@/api/operator', () => ({
  createMerchant: vi.fn(),
  createMerchantStore: vi.fn(async () => ({ store_id: 'store-1' })),
  inviteMember: vi.fn(),
  listAuditLog: vi.fn(async () => []),
  listMerchants: vi.fn(async () => []),
  listMerchantStores: vi.fn(async () => []),
  logManualStep: vi.fn(),
}))

import { createMerchantStore } from '@/api/operator'

async function flushPromises() {
  await new Promise((resolve) => setTimeout(resolve, 0))
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(createMerchantStore).mockClear()
})

describe('AddStoreModal', () => {
  it('defaults to woocommerce and creates the store on that platform', async () => {
    const w = mount(AddStoreModal, {
      global: { plugins: [i18n] },
      props: { merchantId: 'merchant-1', merchantName: 'Bayside Trail' },
    })

    const select = w.find('select')
    expect((select.element as HTMLSelectElement).value).toBe('woocommerce')

    await w.find('input[type="text"]').setValue('Bayside Trail')
    await w.find('button.btn-primary').trigger('click')

    expect(createMerchantStore).toHaveBeenCalledWith('merchant-1', {
      platform: 'woocommerce',
      name: 'Bayside Trail',
    })
  })

  it('creates a magento store when the operator switches the platform', async () => {
    const w = mount(AddStoreModal, {
      global: { plugins: [i18n] },
      props: { merchantId: 'merchant-1', merchantName: 'Bayside Trail' },
    })

    await w.find('input[type="text"]').setValue('Bayside Trail')
    await w.find('select').setValue('magento')
    await w.find('button.btn-primary').trigger('click')

    expect(createMerchantStore).toHaveBeenCalledWith('merchant-1', {
      platform: 'magento',
      name: 'Bayside Trail',
    })
  })

  it('emits created with the new store id', async () => {
    const w = mount(AddStoreModal, {
      global: { plugins: [i18n] },
      props: { merchantId: 'merchant-1', merchantName: 'Bayside Trail' },
    })

    await w.find('input[type="text"]').setValue('Bayside Trail')
    await w.find('button.btn-primary').trigger('click')
    await flushPromises()

    expect(w.emitted('created')).toEqual([['store-1']])
  })
})
