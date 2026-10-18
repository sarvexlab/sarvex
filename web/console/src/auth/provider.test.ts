// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Pin the env-driven auth-mode selection. The Kinde implementation
// itself isn't unit-tested here — it touches the real Kinde SDK at
// module load and the redirect flow is browser-only. The wiring (which
// mode loads) is what we pin.

import { describe, it, expect, afterEach } from 'vitest'
import { selectedMode } from './provider'

afterEach(() => {
  const meta = import.meta.env as Record<string, string | undefined>
  delete meta.VITE_AUTH_MODE
})

describe('auth provider selection', () => {
  it('defaults to simulator when VITE_AUTH_MODE is unset', () => {
    delete (import.meta.env as Record<string, string | undefined>).VITE_AUTH_MODE
    expect(selectedMode()).toBe('simulator')
  })

  it('selects kinde when VITE_AUTH_MODE=kinde', () => {
    ;(import.meta.env as Record<string, string | undefined>).VITE_AUTH_MODE = 'kinde'
    expect(selectedMode()).toBe('kinde')
  })

  it('treats any other value as simulator (no third mode)', () => {
    ;(import.meta.env as Record<string, string | undefined>).VITE_AUTH_MODE = 'whatever'
    expect(selectedMode()).toBe('simulator')
  })
})
