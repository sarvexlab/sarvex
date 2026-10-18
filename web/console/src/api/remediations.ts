// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { api } from './client'
import type { components } from './schema'

type ApplyRemediationsResponse = { applied: number; write_back_ids: string[] }
type RevertResponse = { reverted: boolean }

export async function applyRemediations(
  storeId: string,
  remediationIds: string[],
): Promise<ApplyRemediationsResponse> {
  const { data } = await api.post<ApplyRemediationsResponse>(
    `/stores/${encodeURIComponent(storeId)}/catalog/remediations/apply`,
    { remediation_ids: remediationIds },
  )
  return data
}

export async function revertWriteBack(storeId: string, writeBackId: string): Promise<RevertResponse> {
  const { data } = await api.post<RevertResponse>(
    `/stores/${encodeURIComponent(storeId)}/catalog/write-backs/${encodeURIComponent(writeBackId)}/revert`,
  )
  return data
}

export type Remediation = components['schemas']['remediation']
