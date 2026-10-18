// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Operator-only state: the "enter as merchant" context + audit log +
// provisioning helpers. The enterAs context is read by the Axios layer
// (X-Operate-As header) and by AppShell (to render the OperatorBar).
//
// Entering / exiting impersonation clears every merchant-scoped Pinia
// store so the next surface fetch goes out with the right scope — the
// merchant fixtures aren't kept around after exit, and a different
// merchant on re-enter isn't served from a stale cache.

import { defineStore } from 'pinia'
import {
  createMerchant as apiCreateMerchant,
  createMerchantStore as apiCreateStore,
  inviteMember as apiInviteMember,
  listAuditLog,
  listMerchants as apiListMerchants,
  listMerchantStores as apiListMerchantStores,
  logManualStep as apiLogManualStep,
} from '@/api/operator'
import type { components } from '@/api/schema'
import { useStoresStore } from '@/stores/stores'
import { useConnectorsStore } from '@/stores/connectors'
import { useJourneyStore } from '@/stores/journey'
import { useTasksStore } from '@/stores/tasks'
import { usePresenceStore } from '@/stores/presence'
import { useCatalogStore } from '@/stores/catalog'
import { useOrdersStore } from '@/stores/orders'
import { useAccountStore } from '@/stores/account'
import { useBillingStore } from '@/stores/billing'
import { useNotificationsStore } from '@/stores/notifications'
import { usePaymentStore } from '@/stores/payment'
import { useHealthStore } from '@/stores/health'

function resetMerchantStores() {
  useStoresStore().$reset()
  useConnectorsStore().$reset()
  // $reset() replaces state wholesale, which would drop the open journey
  // stream's cleanup closure without ever calling it — close it explicitly
  // first so entering/exiting "act as merchant" doesn't leak a WebSocket.
  useJourneyStore().disconnectStream()
  useJourneyStore().$reset()
  useTasksStore().$reset()
  usePresenceStore().$reset()
  useCatalogStore().$reset()
  useOrdersStore().$reset()
  useAccountStore().$reset()
  useBillingStore().$reset()
  useNotificationsStore().$reset()
  usePaymentStore().$reset()
  useHealthStore().$reset()
}

type Account = components['schemas']['account']
type Store = components['schemas']['store']
type AuditEntry = components['schemas']['audit_entry']
type MemberInvite = components['schemas']['member_invite']
type Platform = components['schemas']['platform']
type ManualStep = components['schemas']['manual_step']

export const useOperatorStore = defineStore('operator', {
  state: () => ({
    enterAs: null as { merchantId: string; storeId: string; merchantName?: string; storeName?: string } | null,
    merchants: [] as Account[],
    storesByMerchant: {} as Record<string, Store[]>,
    audit: [] as AuditEntry[],
    showAudit: false,
    // Captured manual steps this session — the OpenAPI has no GET endpoint
    // for /operator/manual-steps, so the board renders only what's been
    // captured during the current session.
    capturedSteps: [] as ManualStep[],
  }),

  getters: {
    isImpersonating: (s) => s.enterAs !== null,
    operateAsMerchantId: (s) => s.enterAs?.merchantId ?? null,
  },

  actions: {
    enterMerchant(merchantId: string, storeId: string, merchantName?: string, storeName?: string) {
      resetMerchantStores()
      this.enterAs = { merchantId, storeId, merchantName, storeName }
    },
    exitImpersonation() {
      this.enterAs = null
      this.showAudit = false
      resetMerchantStores()
    },
    toggleAudit() {
      this.showAudit = !this.showAudit
    },

    async loadMerchants() {
      this.merchants = await apiListMerchants()
      await Promise.allSettled(
        this.merchants
          .filter((m) => !!m.merchant_id)
          .map((m) => this.loadMerchantStores(m.merchant_id!)),
      )
    },
    async loadMerchantStores(merchantId: string) {
      const stores = await apiListMerchantStores(merchantId)
      this.storesByMerchant[merchantId] = stores
    },
    async loadAudit() {
      this.audit = await listAuditLog(this.enterAs?.merchantId)
    },

    async createMerchant(name: string, ownerEmail: string): Promise<Account> {
      const account = await apiCreateMerchant(name, ownerEmail)
      this.merchants.unshift(account)
      return account
    },
    async inviteMember(merchantId: string, invite: MemberInvite) {
      return apiInviteMember(merchantId, invite)
    },
    async createStore(merchantId: string, name: string, platform: Platform) {
      const store = await apiCreateStore(merchantId, { platform, name })
      await this.loadMerchantStores(merchantId)
      return store
    },
    async captureManualStep(step: ManualStep) {
      const saved = await apiLogManualStep(step)
      this.capturedSteps.unshift(saved)
      return saved
    },
  },
})
