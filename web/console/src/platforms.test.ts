// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest'
import { DEFAULT_STORE_PLATFORM, STORE_PLATFORMS, platformLabelKey } from '@/platforms'

describe('platforms', () => {
  it('offers every commerce backend, WooCommerce first', () => {
    expect(Object.keys(STORE_PLATFORMS)).toEqual(['woocommerce', 'magento'])
    expect(DEFAULT_STORE_PLATFORM).toBe('woocommerce')
  })

  it('labels a store by its platform, and one with none or an unknown one by the default', () => {
    expect(platformLabelKey('magento')).toBe('settings.store.magento')
    expect(platformLabelKey(undefined)).toBe('settings.store.woocommerce')
    expect(platformLabelKey('unknown')).toBe('settings.store.woocommerce')
  })
})
