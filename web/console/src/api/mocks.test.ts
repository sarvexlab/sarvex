// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Routing decisions for the dual-mode adapter. These pin the two modes
// (mock-by-default + real exceptions, real-by-default + mock exceptions)
// so the env-driven config doesn't silently regress.
//
// The module captures the env at evaluation time, so each scenario sets
// import.meta.env then dynamically imports a fresh module.

import { describe, it, expect, vi, afterEach } from 'vitest'

type Env = {
  VITE_USE_MOCKS?: string
  VITE_REAL_ROUTES?: string
  VITE_MOCK_ROUTES?: string
}

async function loadWithEnv(env: Env): Promise<typeof import('./mocks')> {
  // Reset the env to a known state, then apply the scenario.
  const meta = import.meta.env as Record<string, string | undefined>
  delete meta.VITE_USE_MOCKS
  delete meta.VITE_REAL_ROUTES
  delete meta.VITE_MOCK_ROUTES
  Object.assign(meta, env)
  vi.resetModules()
  return import('./mocks')
}

afterEach(() => {
  const meta = import.meta.env as Record<string, string | undefined>
  delete meta.VITE_USE_MOCKS
  delete meta.VITE_REAL_ROUTES
  delete meta.VITE_MOCK_ROUTES
})

describe('apiMode (mock-by-default)', () => {
  it('mocks everything by default', async () => {
    const { apiMode } = await loadWithEnv({ VITE_USE_MOCKS: '1' })
    expect(apiMode('/auth/me')).toBe('mock')
    expect(apiMode('/tasks')).toBe('mock')
    expect(apiMode('/operator/board')).toBe('mock')
  })

  it('routes listed paths to real, everything else mock', async () => {
    const { apiMode } = await loadWithEnv({
      VITE_USE_MOCKS: '1',
      VITE_REAL_ROUTES: '/auth/me,/journey',
    })
    expect(apiMode('/auth/me')).toBe('real')
    expect(apiMode('/journey')).toBe('real')
    expect(apiMode('/tasks')).toBe('mock')
  })

  it('matches by prefix — /operator catches every operator path', async () => {
    const { apiMode } = await loadWithEnv({
      VITE_USE_MOCKS: '1',
      VITE_REAL_ROUTES: '/operator',
    })
    expect(apiMode('/operator')).toBe('real')
    expect(apiMode('/operator/board')).toBe('real')
    expect(apiMode('/operator/merchants/x/members')).toBe('real')
    expect(apiMode('/tasks')).toBe('mock')
  })
})

describe('apiMode (real-by-default)', () => {
  it('routes everything real when VITE_USE_MOCKS=0 and no mock-routes set', async () => {
    const { apiMode } = await loadWithEnv({ VITE_USE_MOCKS: '0' })
    expect(apiMode('/auth/me')).toBe('real')
    expect(apiMode('/tasks')).toBe('real')
  })

  it('routes listed paths to mock, everything else real', async () => {
    const { apiMode } = await loadWithEnv({
      VITE_USE_MOCKS: '0',
      VITE_MOCK_ROUTES: '/operator,/tasks',
    })
    expect(apiMode('/operator/board')).toBe('mock')
    expect(apiMode('/tasks')).toBe('mock')
    expect(apiMode('/auth/me')).toBe('real')
  })
})
