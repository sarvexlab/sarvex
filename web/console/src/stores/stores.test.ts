// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// useStoresStore.load() shares its in-flight promise across concurrent
// callers — the original `if (this.loading) return` bailed early and let
// the second caller observe stale state. This is what bit us in step 4.

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useStoresStore } from './stores'

vi.mock('@/api/stores', () => ({
  listStores: vi.fn(),
}))

import { listStores } from '@/api/stores'

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(listStores).mockReset()
  localStorage.clear()
})

describe('useStoresStore.load', () => {
  it('shares the in-flight promise across concurrent callers', async () => {
    let resolve!: (v: unknown[]) => void
    const apiPromise = new Promise<unknown[]>((r) => (resolve = r))
    vi.mocked(listStores).mockReturnValue(apiPromise as unknown as ReturnType<typeof listStores>)

    const stores = useStoresStore()

    const first = stores.load()
    const second = stores.load()
    // Both calls must wait for the same fetch — listStores must only be
    // called once.
    expect(vi.mocked(listStores)).toHaveBeenCalledTimes(1)

    resolve([
      { store_id: 'str-acme-au', name: 'Acme', platform: 'woocommerce', domain: 'acme.test', journey_state: 'connected' },
    ])
    await Promise.all([first, second])

    // Both callers see the loaded state — neither got a stale early-return.
    expect(stores.loaded).toBe(true)
    expect(stores.stores).toHaveLength(1)
    expect(stores.currentStoreId).toBe('str-acme-au')
  })

  it('select() ignores ids that aren\'t in the loaded list', async () => {
    vi.mocked(listStores).mockResolvedValue([
      { store_id: 'str-acme-au', name: 'Acme', platform: 'woocommerce', domain: 'acme.test', journey_state: 'connected' },
    ] as unknown as Awaited<ReturnType<typeof listStores>>)
    const stores = useStoresStore()
    await stores.load()
    stores.select('str-does-not-exist')
    expect(stores.currentStoreId).toBe('str-acme-au')
    stores.select('str-acme-au')
    expect(stores.currentStoreId).toBe('str-acme-au')
  })

  it('currentStore falls back to the first store when currentStoreId is null', async () => {
    vi.mocked(listStores).mockResolvedValue([
      { store_id: 's1', name: 'First',  platform: 'woocommerce', domain: 'a.test', journey_state: 'connected' },
      { store_id: 's2', name: 'Second', platform: 'woocommerce', domain: 'b.test', journey_state: 'connected' },
    ] as unknown as Awaited<ReturnType<typeof listStores>>)
    const stores = useStoresStore()
    await stores.load()
    expect(stores.currentStore?.store_id).toBe('s1')
  })

  // Regression: a refresh reset Pinia's in-memory currentStoreId to null,
  // so load() always fell back to stores[0] — even if the second store was
  // the one actually selected before the reload.
  it('select() persists the choice so a later load() (simulating a reload) restores it', async () => {
    vi.mocked(listStores).mockResolvedValue([
      { store_id: 's1', name: 'First',  platform: 'woocommerce', domain: 'a.test', journey_state: 'connected' },
      { store_id: 's2', name: 'Second', platform: 'woocommerce', domain: 'b.test', journey_state: 'connected' },
    ] as unknown as Awaited<ReturnType<typeof listStores>>)

    const stores = useStoresStore()
    await stores.load()
    stores.select('s2')

    // Fresh Pinia instance, same localStorage — models a page reload.
    setActivePinia(createPinia())
    const reloaded = useStoresStore()
    await reloaded.load()

    expect(reloaded.currentStoreId).toBe('s2')
  })

  it('load() falls back to the first store when the remembered id no longer exists', async () => {
    localStorage.setItem('sarvex.currentStoreId', 'str-deleted-store')
    vi.mocked(listStores).mockResolvedValue([
      { store_id: 's1', name: 'First', platform: 'woocommerce', domain: 'a.test', journey_state: 'connected' },
    ] as unknown as Awaited<ReturnType<typeof listStores>>)

    const stores = useStoresStore()
    await stores.load()

    expect(stores.currentStoreId).toBe('s1')
  })

  it('setDomain() patches the matching store in place so "Domain & profile" reflects a saved profile without a reload', async () => {
    vi.mocked(listStores).mockResolvedValue([
      { store_id: 's1', name: 'First',  platform: 'woocommerce', domain: 'old.test', journey_state: 'connected' },
      { store_id: 's2', name: 'Second', platform: 'woocommerce', domain: 'b.test', journey_state: 'connected' },
    ] as unknown as Awaited<ReturnType<typeof listStores>>)
    const stores = useStoresStore()
    await stores.load()

    stores.setDomain('s1', 'new.test')

    expect(stores.stores.find((s) => s.store_id === 's1')?.domain).toBe('new.test')
    expect(stores.stores.find((s) => s.store_id === 's2')?.domain).toBe('b.test')
  })

  it('setDomain() is a no-op for a store id that is not loaded', async () => {
    vi.mocked(listStores).mockResolvedValue([
      { store_id: 's1', name: 'First', platform: 'woocommerce', domain: 'old.test', journey_state: 'connected' },
    ] as unknown as Awaited<ReturnType<typeof listStores>>)
    const stores = useStoresStore()
    await stores.load()

    stores.setDomain('does-not-exist', 'new.test')

    expect(stores.stores).toHaveLength(1)
    expect(stores.stores[0].domain).toBe('old.test')
  })
})
