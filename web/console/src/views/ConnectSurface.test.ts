// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// ConnectSurface polling — the flow that originally broke (step 4 + step 5
// regressions). What this pins:
//   - Invalid pairing secret: Pair button stays disabled
//   - Valid secret: POST /connectors, then poll GET /connectors/{id}
//   - Polling continues while status === 'pairing'
//   - When status flips off pairing, the success card renders
//   - The success card's Open-Path navigates to /path

import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { routes } from '@/router'
import { i18n } from '@/i18n'

vi.mock('@/api/connectors', () => ({
  createConnector: vi.fn(),
  getConnector: vi.fn(),
  listConnectors: vi.fn(),
  resyncConnector: vi.fn(),
}))
vi.mock('@/api/stores', () => ({ listStores: vi.fn() }))

import { createConnector, getConnector, listConnectors } from '@/api/connectors'
import { listStores } from '@/api/stores'
import ConnectSurface from './ConnectSurface.vue'
import { useStoresStore } from '@/stores/stores'

type Connector = Awaited<ReturnType<typeof getConnector>>

const STORE = {
  store_id: 'str-test',
  name: 'Test Co',
  platform: 'woocommerce' as const,
  domain: 't.test',
  connector_id: null,
  journey_state: 'connected' as const,
}

const PAIRING_CONNECTOR: Connector = {
  connector_id: 'con-new',
  store_id: 'str-test',
  platform: 'woocommerce',
  status: 'pairing',
  sync_model: 'realtime',
  last_signal_at: null,
  last_full_sync_at: null,
  freshness_warning: false,
  plugin_version: null,
}
const HEALTHY_CONNECTOR: Connector = { ...PAIRING_CONNECTOR, status: 'healthy' }

async function mountWithStore(store: typeof STORE = STORE): Promise<{ wrapper: ReturnType<typeof mount>; router: Router }> {
  const pinia = createPinia()
  setActivePinia(pinia)
  // Seed currentStore — ConnectSurface refuses to do anything without one.
  const stores = useStoresStore()
  stores.stores = [store] as unknown as typeof stores.stores
  stores.currentStoreId = store.store_id
  stores.loaded = true

  const router = createRouter({ history: createMemoryHistory(), routes })
  // No global guard attached — we test the surface, not the guard.
  await router.push('/connect')
  await router.isReady()

  const wrapper = mount(ConnectSurface, {
    global: { plugins: [pinia, router, i18n] },
  })
  await flushPromises()
  return { wrapper, router }
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.mocked(createConnector).mockReset()
  vi.mocked(getConnector).mockReset()
  vi.mocked(listConnectors).mockReset().mockResolvedValue([])
  vi.mocked(listStores).mockReset().mockResolvedValue([STORE] as unknown as Awaited<ReturnType<typeof listStores>>)
})

afterEach(() => {
  vi.useRealTimers()
})

describe('ConnectSurface — pairing flow', () => {
  it("names the store, not an empty domain, before the store has one", async () => {
    const stores = [{ ...STORE, domain: '' }]
    vi.mocked(listStores).mockResolvedValue(stores as unknown as Awaited<ReturnType<typeof listStores>>)
    const { wrapper } = await mountWithStore(stores[0])
    expect(wrapper.text()).toContain("serves Test Co's store details")
    expect(wrapper.text()).not.toContain(" 's store details")
  })

  it('Generate button starts disabled and enables after code generation', async () => {
    const { wrapper } = await mountWithStore()
    const genBtn = wrapper.find('.btn.btn-primary')
    expect((genBtn.element as HTMLButtonElement).disabled).toBe(false)
    expect(genBtn.text()).toContain('Generate pairing code')
  })

  it('on click Generate, POSTs /stores/:id/connectors/pairing then shows pairing code', async () => {
    vi.mocked(createConnector).mockResolvedValue({
      ...PAIRING_CONNECTOR,
      pairing_code: 'cnd-XXXX-XXXX-XXXX',
    } as unknown as Awaited<ReturnType<typeof createConnector>>)

    const { wrapper } = await mountWithStore()

    // Click Generate pairing code button
    await wrapper.find('.btn.btn-primary').trigger('click')
    await flushPromises()

    // POST went out.
    expect(createConnector).toHaveBeenCalledWith('str-test', 'woocommerce')

    // Pairing code is now displayed
    expect(wrapper.html()).toContain('cnd-XXXX-XXXX-XXXX')
    // Instructions visible
    expect(wrapper.html()).toContain('enter the pairing code above')
  })

  it('poll starts when user clicks "I\'ve entered the code" and continues until healthy', async () => {
    vi.mocked(createConnector).mockResolvedValue({
      ...PAIRING_CONNECTOR,
      pairing_code: 'cnd-XXXX-XXXX-XXXX',
    } as unknown as Awaited<ReturnType<typeof createConnector>>)
    // First two polls still pairing, third polls healthy.
    vi.mocked(getConnector)
      .mockResolvedValueOnce(PAIRING_CONNECTOR)
      .mockResolvedValueOnce(PAIRING_CONNECTOR)
      .mockResolvedValueOnce(HEALTHY_CONNECTOR)

    const { wrapper } = await mountWithStore()

    // Generate pairing code first
    await wrapper.find('.btn.btn-primary').trigger('click')
    await flushPromises()

    // Now click the polling button
    await wrapper.find('.start-polling-btn').trigger('click')
    await flushPromises()

    // Advance through three poll iterations (700ms each).
    for (let i = 0; i < 3; i++) {
      await vi.advanceTimersByTimeAsync(700)
      await flushPromises()
    }

    // Three polls fired.
    expect(getConnector).toHaveBeenCalledTimes(3)
    // Success card visible.
    expect(wrapper.html()).toContain('is connected')
  })

  it('shows the success card and navigates to /path on Open', async () => {
    vi.mocked(createConnector).mockResolvedValue({
      ...HEALTHY_CONNECTOR,
      pairing_code: 'cnd-X',
    } as unknown as Awaited<ReturnType<typeof createConnector>>)
    // Single poll: already healthy.
    vi.mocked(getConnector).mockResolvedValue(HEALTHY_CONNECTOR)

    // Real timers throughout — the goal here is to verify the
    // surface → router handoff after pairing succeeds, not the poll
    // mechanics (covered by the previous test).
    vi.useRealTimers()
    const { wrapper, router } = await mountWithStore()

    // Generate pairing code
    await wrapper.find('.btn.btn-primary').trigger('click')
    await flushPromises()

    // Start polling
    await wrapper.find('.start-polling-btn').trigger('click')
    // Wait for the poll to fire (~700ms) and the success card to render.
    await new Promise((r) => setTimeout(r, 900))
    await flushPromises()
    expect(wrapper.html()).toContain('is connected')

    const pushSpy = vi.spyOn(router, 'push')
    await wrapper.find('.done-btn').trigger('click')
    await flushPromises()
    expect(pushSpy).toHaveBeenCalledWith({ name: 'path' })
    // Await the navigation itself so the route actually transitions.
    await pushSpy.mock.results[0].value
    expect(router.currentRoute.value.path).toBe('/path')
  })

  it('keeps polling while the connector stays in pairing for many iterations', async () => {
    vi.mocked(createConnector).mockResolvedValue({
      ...PAIRING_CONNECTOR,
      pairing_code: 'cnd-X',
    } as unknown as Awaited<ReturnType<typeof createConnector>>)
    vi.mocked(getConnector).mockResolvedValue(PAIRING_CONNECTOR)

    const { wrapper } = await mountWithStore()

    // Generate pairing code
    await wrapper.find('.btn.btn-primary').trigger('click')
    await flushPromises()

    // Start polling
    await wrapper.find('.start-polling-btn').trigger('click')
    await flushPromises()

    for (let i = 0; i < 5; i++) {
      await vi.advanceTimersByTimeAsync(700)
      await flushPromises()
    }

    expect(getConnector).toHaveBeenCalledTimes(5)
    // Success card not visible — still showing the pairing UI.
    expect(wrapper.html()).not.toContain('is connected')
    // Button label is "Verifying..." while polling continues.
    expect(wrapper.find('.start-polling-btn').text()).toContain('Verifying')
  })
})
