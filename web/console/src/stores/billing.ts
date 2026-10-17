// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

import { defineStore } from 'pinia'
import { createPortalLink, getBilling } from '@/api/billing'
import type { components } from '@/api/schema'

type Billing = components['schemas']['billing']

export const useBillingStore = defineStore('billing', {
  state: () => ({
    billing: null as Billing | null,
    loading: false,
  }),
  actions: {
    async load() {
      this.loading = true
      try {
        this.billing = await getBilling()
      } finally {
        this.loading = false
      }
    },
    // Payment method, invoices, plan switching, and cancellation all live in
    // this one Stripe-hosted portal session.
    async openPortal() {
      const url = await createPortalLink()
      window.open(url, '_blank', 'noopener')
    },
  },
})
