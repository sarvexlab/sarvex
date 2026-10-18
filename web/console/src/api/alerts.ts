// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { api } from './client'
import type { components } from './schema'

export type Alert = components['schemas']['alert']

export async function acknowledgeAlert(alertId: string): Promise<Alert> {
  const { data } = await api.patch<Alert>(`/alerts/${encodeURIComponent(alertId)}`, {
    status: 'acknowledged',
  })
  return data
}

export async function resolveAlert(alertId: string): Promise<Alert> {
  const { data } = await api.patch<Alert>(`/alerts/${encodeURIComponent(alertId)}`, {
    status: 'resolved',
  })
  return data
}
