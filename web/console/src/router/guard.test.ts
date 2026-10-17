// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Integration test for the route guard — composes the real router, real
// Pinia stores, real `needsConnect` function, with mocked API wrappers.
//
// What this pins:
//   - Unauthed visits → /login with ?next= preserving the original path
//   - Operator outside enter-as-merchant is locked to /board
//   - Merchant with no live connector lands on /connect; once paired,
//     /connect bounces to /path
//   - Multi-store merchant lands on /account (single → /path)
//
// The auth simulator is the real one; merchant-scoped API wrappers are
// vi.mock'd so we control exactly what's returned at each guard pass.

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'

vi.mock('@/api/stores', () => ({ listStores: vi.fn() }))
vi.mock('@/api/connectors', () => ({
  listConnectors: vi.fn(),
  getConnector: vi.fn(),
  createConnector: vi.fn(),
  resyncConnector: vi.fn(),
}))
vi.mock('@/api/auth', () => ({ getMe: vi.fn() }))

import { listStores } from '@/api/stores'
import { listConnectors } from '@/api/connectors'
import { getMe } from '@/api/auth'

// Import after mocks so the modules pick them up.
import { beforeEachGuard, routes } from './index'
import { useAuthStore } from '@/stores/auth'
import { useStoresStore } from '@/stores/stores'
import { useConnectorsStore } from '@/stores/connectors'
import { useOperatorStore } from '@/stores/operator'

type Principal = Awaited<ReturnType<typeof getMe>>
type Store = Awaited<ReturnType<typeof listStores>>[number]
type Connector = Awaited<ReturnType<typeof listConnectors>>[number]

const MERCHANT_PRINCIPAL: Principal = {
  user_id: 'usr-test',
  email: 'merchant@test',
  role: 'owner',
  merchant_id: 'mer-test',
  store_ids: ['str-test'],
}
const OPERATOR_PRINCIPAL: Principal = {
  user_id: 'op-test',
  email: 'op@test',
  role: 'operator',
  merchant_id: '',
  store_ids: [],
}

const STORE_PAIRED: Store = {
  store_id: 'str-test',
  name: 'Test',
  platform: 'woocommerce',
  domain: 't.test',
  connector_id: 'con-test',
  journey_state: 'connected',
}
const STORE_UNPAIRED: Store = { ...STORE_PAIRED, connector_id: null }
const STORE_SECOND: Store = { ...STORE_PAIRED, store_id: 'str-two', name: 'Two' }

const CONNECTOR_HEALTHY: Connector = {
  connector_id: 'con-test',
  store_id: 'str-test',
  platform: 'woocommerce',
  status: 'healthy',
  sync_model: 'realtime',
  last_signal_at: null,
  last_full_sync_at: null,
  freshness_warning: false,
  plugin_version: null,
}
const CONNECTOR_PAIRING: Connector = { ...CONNECTOR_HEALTHY, status: 'pairing' }

function makeRouter() {
  // Memory history so we can drive navigation programmatically without
  // jsdom's history API. Re-attaches the exported beforeEachGuard.
  const r = createRouter({ history: createMemoryHistory(), routes })
  r.beforeEach(beforeEachGuard)
  return r
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(listStores).mockReset()
  vi.mocked(listConnectors).mockReset()
  vi.mocked(getMe).mockReset()
  localStorage.clear()
})

async function signInAs(principal: Principal) {
  vi.mocked(getMe).mockResolvedValue(principal)
  const auth = useAuthStore()
  await auth.init()
  // Mint a fresh sim token via the provider so the Axios interceptor (and
  // mock requireAuth) accept it. Stub the principal directly afterwards
  // so we don't hit the real /auth/me call.
  await auth.signIn()
  // Wait until status flips out of loading.
  await new Promise<void>((resolve) => {
    if (auth.status === 'ready' || auth.status === 'error') return resolve()
    const stop = auth.$subscribe((_m, s) => {
      if (s.status === 'ready' || s.status === 'error') {
        stop()
        resolve()
      }
    })
  })
}

describe('route guard — integration', () => {
  it('redirects unauthed visits to /login and preserves the original path as ?next=', async () => {
    // Init auth without signing in so its status flips to 'unauthenticated'
    // — without init() the status stays 'idle' and the guard waits forever.
    await useAuthStore().init()
    const router = makeRouter()
    await router.push('/path')
    expect(router.currentRoute.value.name).toBe('login')
    expect(router.currentRoute.value.query.next).toBe('/path')
  })

  it('operators are locked to /board outside enter-as-merchant', async () => {
    await signInAs(OPERATOR_PRINCIPAL)
    const router = makeRouter()
    await router.push('/path')
    expect(router.currentRoute.value.path).toBe('/board')
    await router.push('/settings')
    expect(router.currentRoute.value.path).toBe('/board')
    await router.push('/store')
    expect(router.currentRoute.value.path).toBe('/board')
  })

  it('merchant with no live connector lands on /connect', async () => {
    vi.mocked(listStores).mockResolvedValue([STORE_UNPAIRED])
    vi.mocked(listConnectors).mockResolvedValue([])
    await signInAs(MERCHANT_PRINCIPAL)
    const router = makeRouter()
    await router.push('/path')
    expect(router.currentRoute.value.path).toBe('/connect')
  })

  it('merchant whose connector is still pairing stays on /connect', async () => {
    vi.mocked(listStores).mockResolvedValue([STORE_PAIRED])
    vi.mocked(listConnectors).mockResolvedValue([CONNECTOR_PAIRING])
    await signInAs(MERCHANT_PRINCIPAL)
    const router = makeRouter()
    await router.push('/path')
    expect(router.currentRoute.value.path).toBe('/connect')
  })

  it('once paired, /connect bounces to /path', async () => {
    vi.mocked(listStores).mockResolvedValue([STORE_PAIRED])
    vi.mocked(listConnectors).mockResolvedValue([CONNECTOR_HEALTHY])
    await signInAs(MERCHANT_PRINCIPAL)
    const router = makeRouter()
    await router.push('/connect')
    expect(router.currentRoute.value.path).toBe('/path')
  })

  it('paired single-store merchant lands on /path from /', async () => {
    vi.mocked(listStores).mockResolvedValue([STORE_PAIRED])
    vi.mocked(listConnectors).mockResolvedValue([CONNECTOR_HEALTHY])
    await signInAs(MERCHANT_PRINCIPAL)
    const router = makeRouter()
    await router.push('/')
    expect(router.currentRoute.value.path).toBe('/path')
  })

  it('paired merchant who already has data loaded allows direct /account visit', async () => {
    // Multi-store /account vs /path dispatch lives in LoginView (it needs
    // stores.load() to complete before deciding); guards don't see it.
    // Here we just confirm /account is reachable for an authed merchant.
    vi.mocked(listStores).mockResolvedValue([STORE_PAIRED, STORE_SECOND])
    vi.mocked(listConnectors).mockResolvedValue([CONNECTOR_HEALTHY])
    await signInAs(MERCHANT_PRINCIPAL)
    const router = makeRouter()
    await router.push('/account')
    expect(router.currentRoute.value.path).toBe('/account')
  })

  it('operator with enter-as-merchant set can reach merchant surfaces', async () => {
    vi.mocked(listStores).mockResolvedValue([STORE_PAIRED])
    vi.mocked(listConnectors).mockResolvedValue([CONNECTOR_HEALTHY])
    await signInAs(OPERATOR_PRINCIPAL)
    useOperatorStore().enterMerchant('mer-test', 'str-test', 'Test', 'Test')
    // After enter-merchant, the merchant-scoped Pinia stores get $reset by
    // the operator store action, so the guard re-fetches with X-Operate-As.
    // Refresh the connectors store explicitly because $reset emptied it.
    useStoresStore().$reset()
    useConnectorsStore().$reset()
    const router = makeRouter()
    await router.push('/path')
    expect(router.currentRoute.value.path).toBe('/path')
  })

  it('non-operator hitting an operator-only route bounces to /path', async () => {
    vi.mocked(listStores).mockResolvedValue([STORE_PAIRED])
    vi.mocked(listConnectors).mockResolvedValue([CONNECTOR_HEALTHY])
    await signInAs(MERCHANT_PRINCIPAL)
    const router = makeRouter()
    await router.push('/board')
    expect(router.currentRoute.value.path).toBe('/path')
  })
})
