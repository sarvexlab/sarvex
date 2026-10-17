// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Shared Lost Revenue Ledger vocabulary: the closed cause enum's ordering,
// severity coloring, the buyer_abandoned/fixable split (R7), and the
// forward-only status transition table (mirrors
// internal/planes/control/ledger/model.go's CanTransition — open can move
// to acknowledged or resolved, acknowledged only to resolved, resolved is
// terminal; skipping acknowledged is forward, so it's allowed).

import type { ChipKind } from '@/components/ui/Chip.vue'
import type { LedgerCause, LedgerEntryStatus } from '@/api/ledger'
import type { components } from '@/api/schema'

type Plan = components['schemas']['plan']

/** The Lost Revenue Ledger's read API is gated at the Concierge/Scale tier
 * server-side (requireAnalyticsTier, internal/planes/control/api/middleware.go)
 * — self_serve is locked. Mirrors that predicate client-side for TierGate. */
export function isAnalyticsTier(plan: Plan | null | undefined): boolean {
  return plan === 'concierge' || plan === 'scale'
}

/** Minimal shape of vue-i18n's `t` — avoids importing vue-i18n's composer
 * type just for this helper's signature. */
type Translate = (key: string) => string

/** Plain-language label for a ledger_cause, via vue-i18n (ledger.cause.*
 * in src/locales). Callers pass their component's own `t` so this stays a
 * plain function usable from both <script setup> and non-component code. */
export function causeLabel(t: Translate, cause: LedgerCause | null | undefined): string {
  if (!cause) return '—'
  return t(`ledger.cause.${cause}`)
}

/** All ledger_cause values (sarvex-v1-control-plane.yaml's closed enum),
 * in the order the classifier's rule table considers them. */
export const LEDGER_CAUSES: LedgerCause[] = [
  'payment_handler',
  'profile_mismatch',
  'sync_freshness',
  'escalation_dropoff',
  'platform_error',
  'timeout',
  'buyer_abandoned',
  'unclassified',
]

export const LEDGER_ENTRY_STATUSES: LedgerEntryStatus[] = ['open', 'acknowledged', 'resolved']

/** buyer_abandoned represents a buyer's own choice, not something on the
 * merchant's side to fix (R7) — every other cause is "fixable". */
export function isFixableCause(cause: LedgerCause | null | undefined): boolean {
  return cause != null && cause !== 'buyer_abandoned'
}

const CAUSE_CHIP_KIND: Record<LedgerCause, ChipKind> = {
  payment_handler: 'danger',
  escalation_dropoff: 'danger',
  platform_error: 'danger',
  profile_mismatch: 'warn',
  sync_freshness: 'warn',
  timeout: 'warn',
  buyer_abandoned: 'quiet',
  unclassified: 'quiet',
}

export function causeChipKind(cause: LedgerCause | null | undefined): ChipKind {
  return cause ? (CAUSE_CHIP_KIND[cause] ?? 'quiet') : 'quiet'
}

const STATUS_CHIP_KIND: Record<LedgerEntryStatus, ChipKind> = {
  open: 'warn',
  acknowledged: 'quiet',
  resolved: 'ok',
}

export function statusChipKind(status: LedgerEntryStatus | null | undefined): ChipKind {
  return status ? (STATUS_CHIP_KIND[status] ?? 'quiet') : 'quiet'
}

const STATUS_RANK: Record<LedgerEntryStatus, number> = { open: 0, acknowledged: 1, resolved: 2 }

/** Statuses strictly ahead of `current` — what the forward-only PATCH
 * contract allows next. Empty for `resolved` (terminal). */
export function forwardStatuses(current: LedgerEntryStatus): LedgerEntryStatus[] {
  return LEDGER_ENTRY_STATUSES.filter((s) => STATUS_RANK[s] > STATUS_RANK[current])
}

export function canTransition(current: LedgerEntryStatus, next: LedgerEntryStatus): boolean {
  return STATUS_RANK[next] > STATUS_RANK[current]
}

/** UTC month boundaries as [start, end) ISO instants for yyyy-MM. */
export function monthRange(month: string): { from: string; to: string } {
  const [y, m] = month.split('-').map(Number)
  const start = new Date(Date.UTC(y, m - 1, 1))
  const end = new Date(Date.UTC(y, m, 1))
  return { from: start.toISOString(), to: end.toISOString() }
}

/** The current UTC month as yyyy-MM — the ledger's default period (R2). */
export function currentMonth(): string {
  const now = new Date()
  return `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}`
}

const MONTH_RE = /^\d{4}-(0[1-9]|1[0-2])$/

export function isValidMonth(value: string | null | undefined): value is string {
  return !!value && MONTH_RE.test(value)
}

/** Sorts cause breakdown rows by lost_revenue_minor descending — the
 * server already returns them this way, but the cause bar UI lets a
 * merchant re-sort by count, and this is also the reset target on period
 * change / reload (R6: money order is the product invariant). */
export function sortCausesByRevenue<T extends { lost_revenue_minor?: number }>(rows: T[]): T[] {
  return [...rows].sort((a, b) => (b.lost_revenue_minor ?? 0) - (a.lost_revenue_minor ?? 0))
}

export function sortCausesByCount<T extends { count?: number }>(rows: T[]): T[] {
  return [...rows].sort((a, b) => (b.count ?? 0) - (a.count ?? 0))
}
