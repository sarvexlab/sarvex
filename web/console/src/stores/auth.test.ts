// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Regression test: operator impersonation state ("Operator view" bar) must
// not survive sign-out into whoever signs in next. Uses the real simulator
// auth provider + real Pinia stores, mocking only the API wrapper.

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

vi.mock('@/api/auth', () => ({ getMe: vi.fn() }))

import { getMe } from '@/api/auth'
import { useAuthStore } from '@/stores/auth'
import { useOperatorStore } from '@/stores/operator'

type Principal = Awaited<ReturnType<typeof getMe>>

const OPERATOR_PRINCIPAL: Principal = {
  user_id: 'op-test',
  email: 'op@test',
  role: 'operator',
  merchant_id: '',
  store_ids: [],
}
const OWNER_PRINCIPAL: Principal = {
  user_id: 'usr-test',
  email: 'owner@test',
  role: 'owner',
  merchant_id: 'mer-test',
  store_ids: ['str-test'],
}

async function waitForSettled(auth: ReturnType<typeof useAuthStore>) {
  if (auth.status === 'ready' || auth.status === 'error' || auth.status === 'unauthenticated') return
  await new Promise<void>((resolve) => {
    const stop = auth.$subscribe((_m, s) => {
      if (s.status !== 'loading' && s.status !== 'idle') {
        stop()
        resolve()
      }
    })
  })
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(getMe).mockReset()
  localStorage.clear()
})

describe('operator impersonation does not survive sign-out', () => {
  it('clears enterAs on sign-out, so the next sign-in (a different, non-operator user) starts clean', async () => {
    vi.mocked(getMe).mockResolvedValue(OPERATOR_PRINCIPAL)
    const auth = useAuthStore()
    const operator = useOperatorStore()
    await auth.init()
    await auth.signIn()
    await waitForSettled(auth)

    // Simulate the operator entering a merchant — this is what makes
    // OperatorBar render (v-if="operator.enterAs").
    operator.enterMerchant('mer-1', 'str-1', 'Acme', 'Acme Store')
    expect(operator.isImpersonating).toBe(true)

    await auth.signOut()

    expect(operator.isImpersonating).toBe(false)
    expect(operator.enterAs).toBeNull()

    // Sign back in as a completely different, non-operator user.
    vi.mocked(getMe).mockResolvedValue(OWNER_PRINCIPAL)
    await auth.signIn()
    await waitForSettled(auth)

    expect(auth.isOperator).toBe(false)
    // The regression: without the fix, enterAs (and therefore OperatorBar)
    // stays set here even though this session was never an operator.
    expect(operator.enterAs).toBeNull()
    expect(operator.isImpersonating).toBe(false)
  })
})
