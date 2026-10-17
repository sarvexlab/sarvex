// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// The simulator mints JWT-shaped tokens used in dev. These tests pin the
// claim shape so the rest of the console (Axios interceptor, mock
// adapter's decodeSimToken, role-based routing) keeps decoding it the
// same way.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { createSimulatorProvider } from './simulator'

beforeEach(() => {
  localStorage.clear()
  delete (window as unknown as { __sarvexSimRole?: string }).__sarvexSimRole
})

afterEach(() => {
  delete (import.meta.env as Record<string, string | undefined>).VITE_USE_MOCKS
})

function decode(token: string): Record<string, unknown> {
  const parts = token.split('.')
  expect(parts).toHaveLength(3)
  return JSON.parse(atob(parts[1].replace(/-/g, '+').replace(/_/g, '/')))
}

describe('simulator', () => {
  it('emits null token before sign-in', () => {
    const p = createSimulatorProvider()
    let last: string | null | undefined
    p.onToken((t) => (last = t))
    expect(last).toBe(null)
  })

  it('mints an owner token with merchant scope by default', async () => {
    const p = createSimulatorProvider()
    let token: string | null = null
    p.onToken((t) => (token = t))
    await p.signIn()
    expect(token).not.toBeNull()
    const claims = decode(token as unknown as string)
    expect(claims.role).toBe('owner')
    expect(claims.merchant_id).toBe('mer-acme-tea')
    expect((claims.store_ids as string[]).length).toBeGreaterThan(0)
  })

  it('honours window.__sarvexSimRole for operator sign-in', async () => {
    ;(window as unknown as { __sarvexSimRole?: string }).__sarvexSimRole = 'operator'
    const p = createSimulatorProvider()
    let token: string | null = null
    p.onToken((t) => (token = t))
    await p.signIn()
    const claims = decode(token as unknown as string)
    expect(claims.role).toBe('operator')
    // Operators have no merchant scope of their own.
    expect(claims.merchant_id).toBe('')
    expect((claims.store_ids as string[]).length).toBe(0)
  })

  it('persists the token across provider instances', async () => {
    const p1 = createSimulatorProvider()
    await p1.signIn()
    const p2 = createSimulatorProvider()
    let token: string | null = null
    p2.onToken((t) => (token = t))
    expect(token).not.toBeNull()
  })

  it('throws instead of minting a fake merchant when no merchant is selected in real mode', async () => {
    ;(import.meta.env as Record<string, string | undefined>).VITE_USE_MOCKS = '0'
    vi.resetModules()
    const { createSimulatorProvider: createProvider } = await import('./simulator')
    const p = createProvider()
    await expect(p.signIn()).rejects.toThrow(/select a merchant/i)
  })

  it('signOut clears the token and notifies subscribers', async () => {
    const p = createSimulatorProvider()
    await p.signIn()
    let last: string | null | undefined
    p.onToken((t) => (last = t))
    expect(last).not.toBeNull()
    await p.signOut()
    expect(last).toBeNull()
    expect(localStorage.getItem('sarvex.sim.token')).toBeNull()
  })
})

describe('fetchMerchantOptions', () => {
  it('reads merchants, stores and owner from the list envelopes', async () => {
    const { fetchMerchantOptions } = await import('./simulator')
    const { api } = await import('@/api/client')
    const envelope = (data: unknown[]) => ({ data: { data, page: { cursor: null, has_more: false } } })
    const get = vi.spyOn(api, 'get').mockImplementation(async (url: string) => {
      if (url === '/operator/merchants') return envelope([{ merchant_id: 'm1', name: 'Merch 1' }])
      if (url.endsWith('/stores')) return envelope([{ store_id: 's1' }])
      if (url.endsWith('/members'))
        return envelope([{ user_id: 'u1', email: 'o@m1.test', roles: ['owner'] }])
      throw new Error(`unexpected ${url}`)
    })
    try {
      expect(await fetchMerchantOptions()).toEqual([
        { merchantId: 'm1', merchantName: 'Merch 1', storeIds: ['s1'], ownerEmail: 'o@m1.test', ownerSub: 'u1' },
      ])
    } finally {
      get.mockRestore()
    }
  })
})
