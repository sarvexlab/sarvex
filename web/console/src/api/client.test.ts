// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// The axios request interceptor attaches the session token — but must not
// replace an Authorization header the caller set explicitly (the simulator's
// operator-scoped merchant lookup on the login page sends its own token).

import { describe, it, expect } from 'vitest'
import type { AxiosAdapter } from 'axios'
import { api, configureClient } from './client'

function captureAuth(): { adapter: AxiosAdapter; seen: () => string | undefined } {
  let seen: string | undefined
  const adapter: AxiosAdapter = async (config) => {
    seen = config.headers?.Authorization as string | undefined
    return { data: {}, status: 200, statusText: 'OK', headers: {}, config }
  }
  return { adapter, seen: () => seen }
}

describe('api request interceptor', () => {
  configureClient({ getToken: () => 'session-token', onUnauthorized: () => {} })

  it('attaches the session token when the caller sets none', async () => {
    const { adapter, seen } = captureAuth()
    await api.get('/x', { adapter })
    expect(seen()).toBe('Bearer session-token')
  })

  it('keeps a caller-supplied Authorization header', async () => {
    const { adapter, seen } = captureAuth()
    await api.get('/x', { adapter, headers: { Authorization: 'Bearer operator-token' } })
    expect(seen()).toBe('Bearer operator-token')
  })
})
