// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

import { api } from './client'
import type { components } from './schema'

type PaymentConfig = components['schemas']['payment_config']
type PaymentConfigUpdate = components['schemas']['payment_config_update']

export async function getPaymentConfig(storeId: string): Promise<PaymentConfig> {
  const { data } = await api.get<PaymentConfig>(
    `/stores/${encodeURIComponent(storeId)}/payment-config`,
  )
  return data
}

export async function updatePaymentConfig(
  storeId: string,
  patch: PaymentConfigUpdate,
): Promise<PaymentConfig> {
  const { data } = await api.put<PaymentConfig>(
    `/stores/${encodeURIComponent(storeId)}/payment-config`,
    patch,
  )
  return data
}
