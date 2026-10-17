// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

import { defineStore } from 'pinia'
import { getPaymentConfig, updatePaymentConfig } from '@/api/payment'
import type { components } from '@/api/schema'

type PaymentConfig = components['schemas']['payment_config']
type PaymentConfigUpdate = components['schemas']['payment_config_update']

export const usePaymentStore = defineStore('payment', {
  state: () => ({
    config: null as PaymentConfig | null,
    loading: false,
  }),
  actions: {
    async load(storeId: string) {
      this.loading = true
      try {
        this.config = await getPaymentConfig(storeId)
      } finally {
        this.loading = false
      }
    },
    async save(storeId: string, patch: PaymentConfigUpdate) {
      this.config = await updatePaymentConfig(storeId, patch)
    },
  },
})
