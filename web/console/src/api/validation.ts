// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { api } from './client'
import type { components } from './schema'

type Status = components['schemas']['validation_status']
type Record = components['schemas']['conformance_record']
type Job = components['schemas']['job_accepted']
type ListEnvelope<T> = { data: T[]; page: { cursor: string | null; has_more: boolean } }

export async function getValidationStatus(storeId: string): Promise<Status> {
  const { data } = await api.get<Status>(
    `/stores/${encodeURIComponent(storeId)}/validation/status`,
  )
  return data
}

export async function triggerValidationRun(storeId: string): Promise<Job> {
  const { data } = await api.post<Job>(
    `/stores/${encodeURIComponent(storeId)}/validation/runs`,
  )
  return data
}

export async function listValidationRuns(storeId: string): Promise<Record[]> {
  const { data } = await api.get<ListEnvelope<Record>>(
    `/stores/${encodeURIComponent(storeId)}/validation/runs`,
  )
  return data.data
}
