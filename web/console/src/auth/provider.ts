// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// The identity provider abstraction.
//
// Authentication is external (CLAUDE.md §2.4): the IdP issues the JWT;
// Sarvex only verifies. Two implementations:
//
//   - simulator (default): mints a JWT-shaped token locally so the
//     console runs end-to-end without an external IdP. Lets `/login`
//     expose a role picker so the operator flow is reachable without a
//     second real account.
//
//   - kinde: Authorization Code + PKCE via the Kinde SDK. Sign-in
//     redirects to the Kinde-hosted page; the SDK exchanges the code
//     for an access token on the return trip. Passkey support is
//     enabled at the Kinde tenant level (Authentication → Passwordless
//     / Passkey); the hosted page picks it up automatically.
//
// Selection is env-driven:
//   - VITE_AUTH_MODE=kinde → real Kinde (requires VITE_KINDE_DOMAIN +
//     VITE_KINDE_CLIENT_ID, see .env.example)
//   - anything else (including unset) → simulator
//
// Kinde SDK is lazy-imported so simulator builds don't carry it.

import { createSimulatorProvider } from './simulator'

export interface AuthProvider {
  /** Subscribe to token changes. Returns an unsubscribe fn. Fires once on subscribe. */
  onToken(cb: (token: string | null) => void): () => void
  /** Open the sign-in flow; resolves once the user is signed in. */
  signIn(): Promise<void>
  signOut(): Promise<void>
}

export type AuthMode = 'simulator' | 'kinde'

let active: AuthProvider | null = null
let activeMode: AuthMode | null = null

export function selectedMode(): AuthMode {
  return import.meta.env.VITE_AUTH_MODE === 'kinde' ? 'kinde' : 'simulator'
}

export async function initProvider(): Promise<AuthProvider> {
  if (active) return active
  activeMode = selectedMode()
  if (activeMode === 'kinde') {
    const mod = await import('./kinde')
    active = await mod.createKindeProvider()
  } else {
    active = createSimulatorProvider()
  }
  return active
}

export function getProvider(): AuthProvider {
  if (!active) throw new Error('auth provider not initialised')
  return active
}

