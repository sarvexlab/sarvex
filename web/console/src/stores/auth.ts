// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { defineStore } from 'pinia'
import { getMe } from '@/api/auth'
import { configureClient, type Principal } from '@/api/client'
import { getProvider, initProvider } from '@/auth/provider'
import { useOperatorStore } from '@/stores/operator'

type Status = 'idle' | 'loading' | 'ready' | 'unauthenticated' | 'error'

export const useAuthStore = defineStore('auth', {
  state: () => ({
    token: null as string | null,
    principal: null as Principal | null,
    status: 'idle' as Status,
    error: null as string | null,
    _bootstrapped: false as boolean,
  }),

  getters: {
    isAuthenticated: (s) => !!s.token && !!s.principal,
    role: (s) => s.principal?.role ?? null,
    isOperator: (s) => s.principal?.role === 'operator',
    isOwner: (s) => s.principal?.role === 'owner',
  },

  actions: {
    async init() {
      if (this._bootstrapped) return
      this._bootstrapped = true

      const operatorStore = useOperatorStore()
      configureClient({
        getToken: () => this.token,
        getOperateAs: () => operatorStore.operateAsMerchantId,
        onUnauthorized: () => {
          this.token = null
          this.principal = null
          this.status = 'unauthenticated'
          // A 401 ends the session same as an explicit sign-out — clear any
          // operator "enter as merchant" context so it can't survive into
          // whoever signs in next (see the onToken(null) branch below).
          operatorStore.exitImpersonation()
        },
      })

      const provider = await initProvider()
      provider.onToken(async (t) => {
        this.token = t
        if (!t) {
          this.principal = null
          this.status = 'unauthenticated'
          // Sign-out ends the session; the operator's impersonation context
          // is per-session, not per-browser-tab, so it must not survive into
          // the next sign-in (e.g. a different, non-operator user) — without
          // this, OperatorBar keeps rendering ("operator.enterAs" is still
          // set) for whoever logs in next, regardless of their own role.
          operatorStore.exitImpersonation()
          return
        }
        await this.loadPrincipal()
      })
    },

    async loadPrincipal() {
      this.status = 'loading'
      this.error = null
      try {
        this.principal = await getMe()
        this.status = 'ready'
      } catch (e: unknown) {
        this.principal = null
        this.status = 'error'
        this.error = e instanceof Error ? e.message : 'Failed to load principal'
      }
    },

    async signIn() {
      this.error = null
      try {
        await getProvider().signIn()
      } catch (e: unknown) {
        this.status = 'error'
        this.error = e instanceof Error ? e.message : 'Sign-in failed'
      }
    },

    async signOut() {
      await getProvider().signOut()
    },
  },
})
