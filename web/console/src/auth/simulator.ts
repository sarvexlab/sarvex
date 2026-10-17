// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Local-dev IdP simulator.
//
// Mints a token whose claim shape matches what the real Kinde IdP will
// issue, so the Axios layer and authStore are exercised the same way they
// will be in prod. The token is opaque to the console — Sarvex verifies it
// server-side. Picking a role lets us walk the operator vs merchant flows
// without needing two Google accounts in local dev.
//
// Persisted to localStorage so a refresh keeps you signed in across reloads.

import type { AuthProvider } from './provider'
import { apiMode } from '@/api/mocks'
import { api } from '@/api/client'

const STORAGE_KEY = 'sarvex.sim.token'
type Sub = (t: string | null) => void

type SimulatedClaims = {
  sub: string
  email: string
  role: 'owner' | 'staff' | 'operator'
  merchant_id: string
  store_ids: string[]
  iat: number
  exp: number
}

export type MerchantOption = {
  merchantId: string
  merchantName: string
  storeIds: string[]
  ownerEmail: string
  ownerSub: string
}

function mintToken(claims: SimulatedClaims): string {
  // base64url-encoded { header, payload, signature } — same structure as a real
  // JWT so the Axios layer treats it identically. Not cryptographically signed:
  // simulator tokens are accepted only by the simulator-mode backend.
  const enc = (o: unknown) =>
    btoa(JSON.stringify(o)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
  return `${enc({ alg: 'none', typ: 'JWT' })}.${enc(claims)}.sim`
}

function operatorToken(): string {
  const now = Math.floor(Date.now() / 1000)
  return mintToken({
    sub: 'op-sim-001',
    email: 'sim@sarvex.example.com',
    role: 'operator',
    merchant_id: '',
    store_ids: [],
    iat: now,
    exp: now + 60,
  })
}

function defaultClaims(role: SimulatedClaims['role'] = 'owner'): SimulatedClaims {
  const now = Math.floor(Date.now() / 1000)
  return {
    sub: role === 'operator' ? 'op-dana-001' : 'usr-owner-001',
    email: role === 'operator' ? 'dana@sarvex.example.com' : 'hello@sarvex.example.com',
    role,
    merchant_id: role === 'operator' ? '' : 'mer-acme-tea',
    store_ids: role === 'operator' ? [] : ['str-acme-au'],
    iat: now,
    exp: now + 60 * 60 * 8,
  }
}

// Fetches the list of merchants from the real backend using a temp operator
// token, and resolves each merchant's owner email and store_ids.
// Only usable in simulator mode (VITE_AUTH_MODE != 'kinde').
//
// Goes through the shared axios client (client.ts's `api`, base URL
// VITE_API_BASE) rather than a raw same-origin fetch: a bare `/api/...`
// path has no meaning once the console is deployed as a static Worker with
// no backend of its own at that origin — it would silently resolve to the
// Worker's own SPA fallback (a 200 with an HTML body) instead of erroring.
export async function fetchMerchantOptions(): Promise<MerchantOption[]> {
  const headers = { Authorization: `Bearer ${operatorToken()}` }

  // List endpoints return the { data, page } envelope, not a bare array.
  const { data: envelope } = await api.get('/operator/merchants', { headers })
  const merchants = envelope.data as Array<{ merchant_id: string; name: string }>

  const settled = await Promise.allSettled(
    merchants.map(async (m) => {
      const [storesRes, membersRes] = await Promise.all([
        api.get(`/operator/merchants/${encodeURIComponent(m.merchant_id)}/stores`, { headers }),
        api.get(`/operator/merchants/${encodeURIComponent(m.merchant_id)}/members`, { headers }),
      ])
      const stores = storesRes.data.data
      const members = membersRes.data.data
      const owner = (members as Array<{ email: string; user_id: string; roles: string[] }>).find(
        (mem) => mem.roles?.includes('owner'),
      ) ?? members[0]
      return {
        merchantId: m.merchant_id,
        merchantName: m.name,
        storeIds: (stores as Array<{ store_id: string }>).map((s) => s.store_id),
        ownerEmail: owner?.email ?? '',
        ownerSub: owner?.user_id ?? `usr-${m.merchant_id}`,
      } as MerchantOption
    }),
  )

  return settled
    .filter((r): r is PromiseFulfilledResult<MerchantOption> => r.status === 'fulfilled')
    .map((r) => r.value)
}

export function createSimulatorProvider(): AuthProvider {
  const subs = new Set<Sub>()
  let token = localStorage.getItem(STORAGE_KEY)

  const emit = (t: string | null) => {
    token = t
    if (t) localStorage.setItem(STORAGE_KEY, t)
    else localStorage.removeItem(STORAGE_KEY)
    for (const s of subs) s(t)
  }

  return {
    onToken(cb) {
      subs.add(cb)
      cb(token)
      return () => subs.delete(cb)
    },
    async signIn() {
      const role =
        (window as unknown as { __sarvexSimRole?: SimulatedClaims['role'] }).__sarvexSimRole ??
        'owner'

      if (role === 'operator') {
        emit(mintToken(defaultClaims('operator')))
        return
      }

      // For owner/staff: use the merchant the user picked in the login UI.
      const merchant = (
        window as unknown as { __sarvexSimMerchant?: MerchantOption }
      ).__sarvexSimMerchant

      if (!merchant) {
        // In real mode (VITE_USE_MOCKS=0) there is no fallback identity:
        // refuse rather than silently signing in as a fake merchant/store.
        // (A failed /operator/merchants fetch is reported by the login view.)
        if (apiMode('/operator/merchants') === 'real') {
          throw new Error('Select a merchant to sign in as — if none are listed, sign in as operator and create one first.')
        }
        // Mock mode: fall back to hardcoded defaults so the login doesn't
        // hard-block when there's no backend to fetch merchants from.
        emit(mintToken(defaultClaims(role)))
        return
      }

      const now = Math.floor(Date.now() / 1000)
      emit(
        mintToken({
          sub: merchant.ownerSub,
          email: merchant.ownerEmail,
          role,
          merchant_id: merchant.merchantId,
          store_ids: merchant.storeIds,
          iat: now,
          exp: now + 60 * 60 * 8,
        }),
      )
    },
    async signOut() {
      emit(null)
    },
  }
}
