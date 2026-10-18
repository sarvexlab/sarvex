// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Lost Revenue Ledger read/write API — GET /ledger/summary, GET
// /ledger/entries, PATCH /ledger/entries/{entry_id} (sarvex-v1-control-plane.yaml).
// Gated server-side at the Concierge/Scale tier (403 analytics_tier_required);
// the store decides what to render, this module only shapes requests.
//
// /ledger/summary's `currency` is optional: omitted, the response holds
// one ledger_summary per currency the merchant actually has a rollup for
// that month, read from the rollups themselves — never derived from store
// configuration, and never summed across currencies either way (R9).

import { api } from './client'
import type { components } from './schema'

export type LedgerSummary = components['schemas']['ledger_summary']
export type LedgerEntry = components['schemas']['ledger_entry']
export type LedgerCause = components['schemas']['ledger_cause']
export type LedgerEntryStatus = components['schemas']['ledger_entry_status']

type ListEnvelope<T> = { data: T[]; page: { cursor: string | null; has_more: boolean } }

export async function listLedgerSummaries(params: {
  month?: string
  currency?: string
}): Promise<LedgerSummary[]> {
  const { data } = await api.get<{ data: LedgerSummary[] }>('/ledger/summary', { params })
  return data.data
}

export type ListLedgerEntriesFilter = {
  cause?: LedgerCause
  status?: LedgerEntryStatus
  store_id?: string
  surface?: string
  from?: string
  to?: string
  sort?: 'lost_revenue_minor' | 'occurred_at'
  cursor?: string
  limit?: number
}

export async function listLedgerEntries(
  filter: ListLedgerEntriesFilter = {},
): Promise<ListEnvelope<LedgerEntry>> {
  const { data } = await api.get<ListEnvelope<LedgerEntry>>('/ledger/entries', {
    params: filter,
  })
  return data
}

export async function patchLedgerEntryStatus(
  entryId: string,
  status: LedgerEntryStatus,
): Promise<LedgerEntry> {
  const { data } = await api.patch<LedgerEntry>(
    `/ledger/entries/${encodeURIComponent(entryId)}`,
    { status },
  )
  return data
}
