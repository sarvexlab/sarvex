// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Team panel's self-serve invite: the account owner can invite teammates
// directly from Settings → Team, capped at the plan's seat limit
// (GET /billing → usage.member_cap). Pins:
//   - staff principals don't see the invite form at all
//   - owner: successful invite clears the field and appends the member
//   - owner: form disables and shows a specific note once at the seat cap
//   - owner: 409 member_already_active / member_limit_reached surface as
//     distinct inline messages, not a generic failure

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import { i18n } from '@/i18n'
import SettingsDrawer from './SettingsDrawer.vue'
import { useAccountStore } from '@/stores/account'
import { useAuthStore } from '@/stores/auth'
import { useBillingStore } from '@/stores/billing'
import { useConnectorsStore } from '@/stores/connectors'
import { useStoresStore } from '@/stores/stores'
import type { components } from '@/api/schema'

type Member = components['schemas']['member']
type Principal = components['schemas']['principal']
type Connector = components['schemas']['connector']
type Store = components['schemas']['store']

vi.mock('@/api/account', () => ({
  inviteMember: vi.fn(),
}))

vi.mock('@/api/connectors', () => ({
  disconnectConnector: vi.fn(),
  resyncConnector: vi.fn(),
  listConnectors: vi.fn(),
  getConnector: vi.fn(),
  createConnector: vi.fn(),
}))

import { inviteMember } from '@/api/account'
import { disconnectConnector, listConnectors } from '@/api/connectors'

function testRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', name: 'path', component: { template: '<div />' } },
      { path: '/connect', name: 'connect', component: { template: '<div />' } },
    ],
  })
}

const OWNER: Member = { user_id: 'u-1', email: 'owner@example.com', role: 'owner', status: 'active' }

function seed(opts: { role: Principal['role']; members: Member[]; memberCap?: number }) {
  const pinia = createPinia()
  setActivePinia(pinia)

  const auth = useAuthStore()
  auth.principal = { user_id: 'u-x', email: 'x@example.com', role: opts.role }

  const account = useAccountStore()
  account.members = opts.members
  account.loaded = true

  const billing = useBillingStore()
  billing.billing = { plan: 'concierge', status: 'active', usage: { member_cap: opts.memberCap ?? 5 } }

  return { pinia, account, auth, billing }
}

function mountDrawer(pinia: ReturnType<typeof createPinia>, panel: 'team' | 'connector' = 'team') {
  return mount(SettingsDrawer, {
    props: { panel },
    global: { plugins: [pinia, testRouter(), i18n] },
  })
}

beforeEach(() => {
  vi.mocked(inviteMember).mockReset()
  vi.mocked(disconnectConnector).mockReset()
  vi.mocked(listConnectors).mockReset()
})

describe('SettingsDrawer — team panel invite', () => {
  it('hides the invite form for a staff principal', () => {
    const { pinia } = seed({ role: 'staff', members: [OWNER] })
    const w = mountDrawer(pinia)

    expect(w.find('input[type="email"]').exists()).toBe(false)
    expect(w.text()).toContain('Only the account owner can invite new teammates.')
  })

  it('shows the invite form and seat count for the owner', () => {
    const { pinia } = seed({ role: 'owner', members: [OWNER], memberCap: 5 })
    const w = mountDrawer(pinia)

    expect(w.find('input[type="email"]').exists()).toBe(true)
    expect(w.text()).toContain('1 of 5 seats used')
  })

  it('invites a teammate and clears the field on success', async () => {
    const { pinia, account } = seed({ role: 'owner', members: [OWNER], memberCap: 5 })
    const invited: Member = { user_id: 'pending:new@example.com', email: 'new@example.com', role: 'staff', status: 'invited' }
    vi.mocked(inviteMember).mockResolvedValue(invited)

    const w = mountDrawer(pinia)
    const input = w.find('input[type="email"]')
    await input.setValue('new@example.com')
    await w.find('.invite-row button').trigger('click')
    await flushPromises()

    expect(inviteMember).toHaveBeenCalledWith('new@example.com')
    expect((input.element as HTMLInputElement).value).toBe('')
    expect(account.members).toHaveLength(2)
    expect(w.text()).toContain('new@example.com')
  })

  it('disables the form and explains once the seat cap is reached', () => {
    const members = [OWNER, { user_id: 'u-2', email: 'staff@example.com', role: 'staff', status: 'active' } as Member]
    const { pinia } = seed({ role: 'owner', members, memberCap: 2 })
    const w = mountDrawer(pinia)

    expect((w.find('input[type="email"]').element as HTMLInputElement).disabled).toBe(true)
    expect((w.find('.invite-row button').element as HTMLButtonElement).disabled).toBe(true)
    expect(w.text()).toContain("You've used all 2 seats on your plan.")
  })

  it('surfaces member_already_active as a specific message', async () => {
    const { pinia } = seed({ role: 'owner', members: [OWNER], memberCap: 5 })
    vi.mocked(inviteMember).mockRejectedValue({
      isAxiosError: true,
      response: { data: { error: { code: 'member_already_active', message: 'member is already active' } } },
    })

    const w = mountDrawer(pinia)
    await w.find('input[type="email"]').setValue('owner@example.com')
    await w.find('.invite-row button').trigger('click')
    await flushPromises()

    expect(w.text()).toContain('This person is already on the team.')
  })

  it('surfaces member_limit_reached as a specific message', async () => {
    const { pinia } = seed({ role: 'owner', members: [OWNER], memberCap: 5 })
    vi.mocked(inviteMember).mockRejectedValue({
      isAxiosError: true,
      response: { data: { error: { code: 'member_limit_reached', message: 'limit reached' } } },
    })

    const w = mountDrawer(pinia)
    await w.find('input[type="email"]').setValue('new@example.com')
    await w.find('.invite-row button').trigger('click')
    await flushPromises()

    expect(w.text()).toContain("You've reached your team limit of 5.")
  })
})

// Bug: disconnecting left the merchant stranded on their current page,
// looking connected, until they force-refreshed — needsConnect (router/index.ts)
// decides purely from the connectors store, which onDisconnect never
// refreshed, and nothing re-runs that guard without an actual navigation.
describe('SettingsDrawer — connector panel disconnect', () => {
  const CONNECTOR: Connector = {
    connector_id: 'conn-1',
    store_id: 'store-1',
    platform: 'woocommerce',
    status: 'healthy',
  } as Connector
  const STORE: Store = { store_id: 'store-1', name: 'Test Store' } as Store

  function seedConnected() {
    const pinia = createPinia()
    setActivePinia(pinia)

    useAuthStore().principal = { user_id: 'u-1', email: 'owner@example.com', role: 'owner' }
    useBillingStore().billing = { plan: 'concierge', status: 'active', usage: { member_cap: 5 } }

    const stores = useStoresStore()
    stores.stores = [STORE]
    stores.currentStoreId = 'store-1'
    stores.loaded = true

    const connectors = useConnectorsStore()
    connectors.connectors = [CONNECTOR]

    return { pinia, connectors }
  }

  it('refreshes the connectors store and routes to /connect on success', async () => {
    const { pinia, connectors } = seedConnected()
    vi.mocked(disconnectConnector).mockResolvedValue(undefined)
    vi.mocked(listConnectors).mockResolvedValue([]) // disconnected: gone from the list

    const w = mountDrawer(pinia, 'connector')
    const router = w.vm.$router

    await w.find('.btn-secondary').trigger('click') // "Disconnect" opens the confirm dialog
    await w.find('.btn-danger').trigger('click') // confirm
    await flushPromises()

    expect(disconnectConnector).toHaveBeenCalledWith('conn-1')
    expect(listConnectors).toHaveBeenCalled()
    expect(connectors.connectors).toHaveLength(0)
    expect(router.currentRoute.value.name).toBe('connect')
  })

  it('shows an error toast and does not navigate if disconnect fails', async () => {
    const { pinia, connectors } = seedConnected()
    vi.mocked(disconnectConnector).mockRejectedValue(new Error('network down'))

    const w = mountDrawer(pinia, 'connector')
    const router = w.vm.$router

    await w.find('.btn-secondary').trigger('click')
    await w.find('.btn-danger').trigger('click')
    await flushPromises()

    expect(w.emitted('toast')?.[0]).toEqual(['Failed to disconnect: network down'])
    expect(listConnectors).not.toHaveBeenCalled()
    expect(connectors.connectors).toHaveLength(1)
    expect(router.currentRoute.value.name).not.toBe('connect')
  })
})
