// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useLedgerStore, monthRange } from './ledger'
import type { LedgerEntry, LedgerSummary } from '@/api/ledger'

vi.mock('@/api/ledger', () => ({
  listLedgerSummaries: vi.fn(),
  listLedgerEntries: vi.fn(),
  patchLedgerEntryStatus: vi.fn(),
}))

import { listLedgerSummaries, listLedgerEntries, patchLedgerEntryStatus } from '@/api/ledger'

const entry = (over: Partial<LedgerEntry>): LedgerEntry => ({
  entry_id: 'led-1',
  occurred_at: '2026-08-05T10:00:00Z',
  checkout_id: 'chk-1',
  store_id: 'str-1',
  surface: 'ucp_checkout',
  stage: 'payment',
  failure_code: 'CONNECTOR_CREATE_FAILED',
  cause: 'payment_handler',
  recommended_action: 'Check your payment handler config',
  lost_revenue_minor: 1000,
  currency: 'USD',
  revenue_unknown: false,
  status: 'open',
  status_changed_at: '2026-08-05T10:00:00Z',
  created_at: '2026-08-05T10:00:00Z',
  ...over,
})

const summary = (over: Partial<LedgerSummary> = {}): LedgerSummary => ({
  month: '2026-08',
  currency: 'USD',
  causes: [],
  total_count: 0,
  total_lost_revenue_minor: 0,
  entries_with_unknown_revenue: 0,
  prior_month_delta_minor: 0,
  ...over,
})

const entriesPage = (data: LedgerEntry[], cursor: string | null = null, has_more = false) => ({
  data,
  page: { cursor, has_more },
})

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(listLedgerSummaries).mockReset()
  vi.mocked(listLedgerEntries).mockReset()
  vi.mocked(patchLedgerEntryStatus).mockReset()
})

describe('monthRange', () => {
  it('returns UTC month start/end boundaries', () => {
    expect(monthRange('2026-08')).toEqual({
      from: '2026-08-01T00:00:00.000Z',
      to: '2026-09-01T00:00:00.000Z',
    })
  })
})

describe('useLedgerStore.loadSummaries', () => {
  it('keys the list response by currency and never combines totals', async () => {
    vi.mocked(listLedgerSummaries).mockResolvedValue([
      summary({ currency: 'USD', total_lost_revenue_minor: 5000 }),
      summary({ currency: 'AUD', total_lost_revenue_minor: 3000 }),
    ])

    const ledger = useLedgerStore()
    await ledger.loadSummaries('2026-08')

    expect(listLedgerSummaries).toHaveBeenCalledWith({ month: '2026-08' })
    expect(ledger.currencies).toEqual(['AUD', 'USD'])
    expect(ledger.summaries.USD.total_lost_revenue_minor).toBe(5000)
    expect(ledger.summaries.AUD.total_lost_revenue_minor).toBe(3000)
    // No code path sums the two currencies into one number.
    expect(Object.values(ledger.summaries).some((s) => s.currency == null)).toBe(false)
  })

  it('renders a single currency group when the merchant only has one currency', async () => {
    vi.mocked(listLedgerSummaries).mockResolvedValue([summary({ currency: 'USD' })])

    const ledger = useLedgerStore()
    await ledger.loadSummaries('2026-08')

    expect(ledger.currencies).toEqual(['USD'])
  })

  it('resets breakdownSort to revenue on every load (period change or reload)', async () => {
    vi.mocked(listLedgerSummaries).mockResolvedValue([])
    const ledger = useLedgerStore()
    ledger.setBreakdownSort('count')
    expect(ledger.breakdownSort).toBe('count')

    await ledger.loadSummaries('2026-08')
    expect(ledger.breakdownSort).toBe('revenue')

    ledger.setBreakdownSort('count')
    await ledger.loadSummaries('2026-09')
    expect(ledger.breakdownSort).toBe('revenue')
  })

  it('captures a summary load failure as a recoverable error', async () => {
    vi.mocked(listLedgerSummaries).mockRejectedValue(new Error('network down'))
    const ledger = useLedgerStore()
    await ledger.loadSummaries('2026-08')
    expect(ledger.summariesError).toBe('network down')
  })
})

describe('useLedgerStore.loadEntries', () => {
  it('never passes a client-imposed default sort — the server order is authoritative', async () => {
    vi.mocked(listLedgerEntries).mockResolvedValue(entriesPage([entry({})]))
    const ledger = useLedgerStore()
    await ledger.loadEntries({ store_id: 'str-1' })
    expect(listLedgerEntries).toHaveBeenCalledWith({ store_id: 'str-1' })
    expect(vi.mocked(listLedgerEntries).mock.calls[0][0]).not.toHaveProperty('sort')
  })

  it('stores entries in the exact order the server returned them', async () => {
    vi.mocked(listLedgerEntries).mockResolvedValue(
      entriesPage([entry({ entry_id: 'b', lost_revenue_minor: 100 }), entry({ entry_id: 'a', lost_revenue_minor: 900 })]),
    )
    const ledger = useLedgerStore()
    await ledger.loadEntries()
    expect(ledger.entries.map((e) => e.entry_id)).toEqual(['b', 'a'])
  })

  describe('pagination', () => {
    it('loadMoreEntries appends using the returned cursor', async () => {
      vi.mocked(listLedgerEntries).mockResolvedValueOnce(
        entriesPage([entry({ entry_id: 'a' })], 'cur-a', true),
      )
      const ledger = useLedgerStore()
      await ledger.loadEntries()

      vi.mocked(listLedgerEntries).mockResolvedValueOnce(
        entriesPage([entry({ entry_id: 'b' })], 'cur-b', true),
      )
      await ledger.loadMoreEntries()
      expect(listLedgerEntries).toHaveBeenLastCalledWith({ cursor: 'cur-a' })
      expect(ledger.entries.map((e) => e.entry_id)).toEqual(['a', 'b'])
      expect(ledger.entriesHasMore).toBe(true)
      expect(ledger.entriesCursor).toBe('cur-b')
    })

    it('handles the terminal page correctly (has_more false, cursor null)', async () => {
      vi.mocked(listLedgerEntries).mockResolvedValueOnce(
        entriesPage([entry({ entry_id: 'a' })], 'cur-a', true),
      )
      const ledger = useLedgerStore()
      await ledger.loadEntries()

      vi.mocked(listLedgerEntries).mockResolvedValueOnce(
        entriesPage([entry({ entry_id: 'b' })], null, false),
      )
      await ledger.loadMoreEntries()
      expect(ledger.entriesHasMore).toBe(false)
      expect(ledger.entriesCursor).toBeNull()

      // A further call is a no-op — there is no next page.
      await ledger.loadMoreEntries()
      expect(listLedgerEntries).toHaveBeenCalledTimes(2)
    })
  })
})

describe('useLedgerStore status transitions', () => {
  it('applies an optimistic update immediately, then reconciles with the server response', async () => {
    vi.mocked(listLedgerEntries).mockResolvedValue(entriesPage([entry({ entry_id: 'a', status: 'open' })]))
    const ledger = useLedgerStore()
    await ledger.loadEntries()

    let resolvePatch!: (v: LedgerEntry) => void
    vi.mocked(patchLedgerEntryStatus).mockReturnValue(
      new Promise((resolve) => {
        resolvePatch = resolve
      }),
    )

    const applying = ledger.applyStatus('a', 'acknowledged')
    expect(ledger.entries[0].status).toBe('acknowledged')
    expect(ledger.statusPending.a).toBe(true)

    resolvePatch(entry({ entry_id: 'a', status: 'acknowledged', status_changed_at: '2026-08-06T00:00:00Z' }))
    await applying
    expect(ledger.entries[0].status_changed_at).toBe('2026-08-06T00:00:00Z')
    expect(ledger.statusPending.a).toBeUndefined()
  })

  it('rolls back the optimistic update and surfaces an error on failure', async () => {
    vi.mocked(listLedgerEntries).mockResolvedValue(entriesPage([entry({ entry_id: 'a', status: 'open' })]))
    const ledger = useLedgerStore()
    await ledger.loadEntries()

    vi.mocked(patchLedgerEntryStatus).mockRejectedValue(new Error('server rejected'))
    const ok = await ledger.applyStatus('a', 'acknowledged')

    expect(ok).toBe(false)
    expect(ledger.entries[0].status).toBe('open')
    expect(ledger.lastStatusError).toBe('server rejected')
  })

  it('rejects a backward transition without calling the API', async () => {
    vi.mocked(listLedgerEntries).mockResolvedValue(entriesPage([entry({ entry_id: 'a', status: 'resolved' })]))
    const ledger = useLedgerStore()
    await ledger.loadEntries()

    const ok = await ledger.applyStatus('a', 'open')
    expect(ok).toBe(false)
    expect(patchLedgerEntryStatus).not.toHaveBeenCalled()
    expect(ledger.entries[0].status).toBe('resolved')
  })

  it('bulkApplyStatus reports partial failure counts rather than applying silently', async () => {
    vi.mocked(listLedgerEntries).mockResolvedValue(
      entriesPage([
        entry({ entry_id: 'a', status: 'open' }),
        entry({ entry_id: 'b', status: 'open' }),
        entry({ entry_id: 'c', status: 'open' }),
      ]),
    )
    const ledger = useLedgerStore()
    await ledger.loadEntries()

    vi.mocked(patchLedgerEntryStatus).mockImplementation(async (id, status) => {
      if (id === 'b') throw new Error('boom')
      return entry({ entry_id: id, status })
    })

    const result = await ledger.bulkApplyStatus(['a', 'b', 'c'], 'acknowledged')
    expect(result).toEqual({ succeeded: 2, failed: 1 })
    expect(ledger.entries.find((e) => e.entry_id === 'a')?.status).toBe('acknowledged')
    expect(ledger.entries.find((e) => e.entry_id === 'b')?.status).toBe('open')
    expect(ledger.entries.find((e) => e.entry_id === 'c')?.status).toBe('acknowledged')
  })
})
