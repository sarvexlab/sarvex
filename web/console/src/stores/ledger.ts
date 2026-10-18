// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Lost Revenue Ledger state: the per-currency monthly summary (R4 — never
// summed or converted across currencies), the entries table (server-order,
// cursor-paginated), and forward-only status triage (single + bulk).

import { defineStore } from 'pinia'
import {
  listLedgerSummaries,
  listLedgerEntries,
  patchLedgerEntryStatus,
  type LedgerEntry,
  type LedgerEntryStatus,
  type LedgerSummary,
  type ListLedgerEntriesFilter,
} from '@/api/ledger'
import { canTransition, monthRange } from '@/lib/ledger'

export { monthRange }

export type BreakdownSort = 'revenue' | 'count'

export const useLedgerStore = defineStore('ledger', {
  state: () => ({
    month: null as string | null,

    // Summary — one entry per currency actually in use this month.
    summaries: {} as Record<string, LedgerSummary>,
    summariesLoading: false,
    summariesLoaded: false,
    summariesError: null as string | null,
    // Resets to 'revenue' on every loadSummaries() call (period change or
    // reload) — R6: money order is the product invariant, not a
    // remembered preference.
    breakdownSort: 'revenue' as BreakdownSort,

    // Entries table — server order, cursor-paginated (R9/R12).
    entries: [] as LedgerEntry[],
    entriesFilter: {} as ListLedgerEntriesFilter,
    entriesCursor: null as string | null,
    entriesHasMore: false,
    entriesLoading: false,
    entriesLoadingMore: false,
    entriesLoaded: false,
    entriesError: null as string | null,

    // Status triage (R14/R15).
    statusPending: {} as Record<string, boolean>,
    lastStatusError: null as string | null,
    bulkResult: null as { succeeded: number; failed: number } | null,
  }),

  getters: {
    currencies: (s): string[] => Object.keys(s.summaries).sort(),
  },

  actions: {
    setBreakdownSort(sort: BreakdownSort) {
      this.breakdownSort = sort
    },

    async loadSummaries(month: string) {
      this.month = month
      this.breakdownSort = 'revenue'
      this.summariesLoading = true
      this.summariesError = null
      try {
        // currency omitted — the API returns one element per currency the
        // merchant actually has a rollup for that month (never summed, R9).
        const results = await listLedgerSummaries({ month })
        const next: Record<string, LedgerSummary> = {}
        for (const s of results) {
          if (s.currency) next[s.currency] = s
        }
        this.summaries = next
        this.summariesLoaded = true
      } catch (e: unknown) {
        this.summariesError = e instanceof Error ? e.message : 'Failed to load the ledger summary'
      } finally {
        this.summariesLoading = false
      }
    },

    async loadEntries(filter: ListLedgerEntriesFilter = {}) {
      this.entriesFilter = filter
      this.entriesLoading = true
      this.entriesError = null
      try {
        // No `sort` unless the caller set one — the server's default
        // (lost_revenue_minor descending) is the product invariant (R9);
        // the store must not impose a client-side default of its own.
        const page = await listLedgerEntries(filter)
        this.entries = page.data
        this.entriesCursor = page.page.cursor
        this.entriesHasMore = page.page.has_more
        this.entriesLoaded = true
      } catch (e: unknown) {
        this.entriesError = e instanceof Error ? e.message : 'Failed to load ledger entries'
      } finally {
        this.entriesLoading = false
      }
    },

    async loadMoreEntries() {
      if (!this.entriesHasMore || this.entriesLoadingMore || !this.entriesCursor) return
      this.entriesLoadingMore = true
      this.entriesError = null
      try {
        const page = await listLedgerEntries({ ...this.entriesFilter, cursor: this.entriesCursor })
        this.entries = [...this.entries, ...page.data]
        this.entriesCursor = page.page.cursor
        this.entriesHasMore = page.page.has_more
      } catch (e: unknown) {
        this.entriesError = e instanceof Error ? e.message : 'Failed to load more ledger entries'
      } finally {
        this.entriesLoadingMore = false
      }
    },

    /** Optimistic single-entry status change with rollback on failure.
     * Returns true on success. No-ops (returns false, no request sent) for
     * a non-forward transition — the table only ever offers forward
     * buttons, but the store enforces it independently of the UI. */
    async applyStatus(entryId: string, status: LedgerEntryStatus): Promise<boolean> {
      const i = this.entries.findIndex((e) => e.entry_id === entryId)
      if (i < 0) return false
      const previous = this.entries[i]
      if (!previous.status || !canTransition(previous.status, status)) return false

      this.statusPending = { ...this.statusPending, [entryId]: true }
      this.entries.splice(i, 1, { ...previous, status })
      try {
        const updated = await patchLedgerEntryStatus(entryId, status)
        const j = this.entries.findIndex((e) => e.entry_id === entryId)
        if (j >= 0) this.entries.splice(j, 1, updated)
        this.lastStatusError = null
        return true
      } catch (e: unknown) {
        const j = this.entries.findIndex((e) => e.entry_id === entryId)
        if (j >= 0) this.entries.splice(j, 1, previous)
        this.lastStatusError = e instanceof Error ? e.message : 'Failed to update entry status'
        return false
      } finally {
        const next = { ...this.statusPending }
        delete next[entryId]
        this.statusPending = next
      }
    },

    async bulkApplyStatus(entryIds: string[], status: LedgerEntryStatus) {
      this.bulkResult = null
      const results = await Promise.allSettled(entryIds.map((id) => this.applyStatus(id, status)))
      const succeeded = results.filter((r) => r.status === 'fulfilled' && r.value).length
      this.bulkResult = { succeeded, failed: entryIds.length - succeeded }
      return this.bulkResult
    },

    clearBulkResult() {
      this.bulkResult = null
    },

    clearStatusError() {
      this.lastStatusError = null
    },
  },
})
