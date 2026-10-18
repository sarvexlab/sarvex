// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Login view (simulator mode) — the sign-in button must stay disabled for
// owner/staff when the real backend has no merchant to sign in as. Before
// this, an empty merchant list left the button enabled and signIn() threw a
// misleading "Could not connect to the server".

import { describe, it, expect, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { routes } from '@/router'
import { i18n } from '@/i18n'

const merchantOptions = vi.fn()
let mode: 'real' | 'mock' = 'real'

vi.mock('@/auth/provider', async (orig) => ({
  ...(await orig<typeof import('@/auth/provider')>()),
  selectedMode: () => 'simulator',
}))
vi.mock('@/auth/simulator', () => ({ fetchMerchantOptions: () => merchantOptions() }))
vi.mock('@/api/mocks', async (orig) => ({
  ...(await orig<typeof import('@/api/mocks')>()),
  apiMode: () => mode,
}))

const wrappers: Array<ReturnType<typeof mount>> = []
afterEach(() => {
  for (const w of wrappers.splice(0)) w.unmount()
})

async function mountLogin() {
  setActivePinia(createPinia())
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push('/login')
  await router.isReady()
  const { default: LoginView } = await import('./LoginView.vue')
  const wrapper = mount(LoginView, { global: { plugins: [router, i18n] } })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}

const signInButton = (w: ReturnType<typeof mount>) => w.find('button.primary')

describe('LoginView merchant picker', () => {
  it('disables owner sign-in in real mode when no merchants exist', async () => {
    mode = 'real'
    merchantOptions.mockResolvedValue([])
    const w = await mountLogin()
    expect(w.text()).toContain('No merchants found')
    expect(signInButton(w).attributes('disabled')).toBeDefined()
  })

  it('enables owner sign-in in real mode once a merchant is listed', async () => {
    mode = 'real'
    merchantOptions.mockResolvedValue([
      { merchantId: 'm1', merchantName: 'M1', storeIds: [], ownerEmail: 'o@x', ownerSub: 'u1' },
    ])
    const w = await mountLogin()
    expect(signInButton(w).attributes('disabled')).toBeUndefined()
  })

  it('keeps owner sign-in enabled in mock mode with no merchants (default claims fallback)', async () => {
    mode = 'mock'
    merchantOptions.mockResolvedValue([])
    const w = await mountLogin()
    expect(signInButton(w).attributes('disabled')).toBeUndefined()
  })

  it('re-enables sign-in when switching to operator after a merchant-load failure', async () => {
    mode = 'real'
    merchantOptions.mockRejectedValue(new Error('403'))
    const w = await mountLogin()
    expect(w.text()).toContain('Could not connect')
    expect(signInButton(w).attributes('disabled')).toBeDefined()

    const operatorTab = w.findAll('button').find((b) => b.text() === 'operator')!
    await operatorTab.trigger('click')
    await flushPromises()
    expect(w.text()).not.toContain('Could not connect')
    expect(signInButton(w).attributes('disabled')).toBeUndefined()
  })
})
