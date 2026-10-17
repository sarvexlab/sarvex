// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Real Kinde auth provider.
//
// Activated when VITE_AUTH_MODE=kinde AND the Kinde config env is set.
// Wraps @kinde-oss/kinde-auth-pkce-js so the rest of the app only sees
// the AuthProvider surface.
//
// OAuth flow: Authorization Code with PKCE (the SDK's only flow — it's
// PKCE-by-design for SPAs). Tokens are stored in memory by default;
// refresh is handled by the SDK and surfaced via getToken().
//
// Passkeys: enable the WebAuthn / Passkey connection in your Kinde
// tenant (Authentication → Passwordless / Passkey). The Kinde-hosted
// sign-in page will offer it alongside any other configured methods —
// no client code change needed.
//
// Token used for the Bearer header: the access_token. Configure your
// Kinde API so the backend recognises it and exposes the custom claims
// (merchant_id, role, store_ids) that Sarvex's /auth/me consumes.

import createKindeClient from '@kinde-oss/kinde-auth-pkce-js'
import type { AuthProvider } from './provider'

type Sub = (token: string | null) => void

type KindeClient = Awaited<ReturnType<typeof createKindeClient>>

let clientSingleton: KindeClient | null = null

async function getClient(): Promise<KindeClient> {
  if (clientSingleton) return clientSingleton

  const domain = import.meta.env.VITE_KINDE_DOMAIN
  const clientId = import.meta.env.VITE_KINDE_CLIENT_ID
  const redirectUri = import.meta.env.VITE_KINDE_REDIRECT_URI ?? window.location.origin
  const logoutUri = import.meta.env.VITE_KINDE_LOGOUT_URI ?? window.location.origin
  const audience = import.meta.env.VITE_KINDE_AUDIENCE

  if (!domain || !clientId) {
    throw new Error(
      'Kinde auth selected but config is incomplete. Set VITE_KINDE_DOMAIN and ' +
        'VITE_KINDE_CLIENT_ID in .env.local — or switch VITE_AUTH_MODE back to simulator.',
    )
  }

  // createKindeClient automatically handles the redirect callback when
  // it detects code + state in the URL. The Promise resolves once
  // initialisation is complete (including the auth-code exchange).
  clientSingleton = await createKindeClient({
    client_id: clientId,
    domain,
    redirect_uri: redirectUri,
    logout_uri: logoutUri,
    audience,
    // The SDK keeps tokens in memory by default (recommended). Refresh
    // tokens require local storage — keep it off until the tenant is
    // configured to issue them.
    is_dangerously_use_local_storage: false,
  })
  return clientSingleton
}

export async function createKindeProvider(): Promise<AuthProvider> {
  const kinde = await getClient()
  const subs = new Set<Sub>()
  let currentToken: string | null = null

  async function refreshToken(): Promise<string | null> {
    const isAuthed = await kinde.isAuthenticated()
    if (!isAuthed) return null
    const t = await kinde.getToken()
    return t ?? null
  }

  // Prime the cached token from the initial state (post-redirect or
  // restored session). Subsequent updates come from sign-in / sign-out
  // and a low-frequency refresh tick that re-pulls via getToken() —
  // the SDK refreshes the access token transparently on call.
  currentToken = await refreshToken()

  // Tokens are typically short-lived (~1h on Kinde defaults). Re-pull
  // every 10 minutes so the cached value stays fresh ahead of expiry.
  // setInterval rather than per-request because the Axios interceptor
  // expects a synchronous token reader.
  const REFRESH_MS = 10 * 60 * 1000
  const timer = window.setInterval(async () => {
    const t = await refreshToken()
    if (t !== currentToken) {
      currentToken = t
      for (const cb of subs) cb(currentToken)
    }
  }, REFRESH_MS)

  // Stop the refresh loop if the page is being torn down; mostly a
  // tidiness measure (the browser would reap it anyway).
  window.addEventListener('beforeunload', () => window.clearInterval(timer), { once: true })

  return {
    onToken(cb) {
      subs.add(cb)
      cb(currentToken)
      return () => subs.delete(cb)
    },
    async signIn() {
      // Redirects to the Kinde-hosted sign-in page. The page renders the
      // auth methods enabled for the tenant (Google, email link,
      // passkey, …). The page returns to redirect_uri with ?code= which
      // createKindeClient swaps for tokens on next load.
      await kinde.login()
    },
    async signOut() {
      await kinde.logout()
      currentToken = null
      for (const cb of subs) cb(null)
    },
  }
}
