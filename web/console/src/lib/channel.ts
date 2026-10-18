// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Shared Channel Dashboard vocabulary: period-preset date math (R2), the
// surface label fallback that lets a server-added channel_surface render
// without a frontend change (R3), currency resolution (R5), and the
// inferred-session / no-sessionization-data qualifiers the funnel needs
// (R7/R8).
//
// Currency (R5): channel_summary/channel_funnel/channel_comparison now ship
// `currency` + `currency_mixed` (internal/planes/control/api/channel.go's
// aggregateCurrency, added after the console first surfaced this as a gap —
// see the "Close Channel Dashboard API-contract gaps" commit on main).
// `currency` is absent (with currency_mixed=false) when the range has no
// revenue-bearing day yet, or absent (with currency_mixed=true) when the
// summed days/surfaces genuinely disagree on currency — either way, never a
// silently-picked first-seen value. channel_daily_entry carries its own
// single-row currency instead (a single store/surface/day is never
// ambiguous the way a summed range can be, so it has no currency_mixed of
// its own) — resolveDailyCurrency below reproduces the same
// agree-or-flag-mixed logic client-side for the trend chart, which combines
// multiple already-fetched daily rows into one point per date.

/** Reuses the Lost Revenue Ledger's tier gate exactly — the OpenAPI's
 * Channel Dashboard section says "reuses the same gate as Ledger", and
 * internal/planes/control/api/channel.go's getChannelSummary/... handlers
 * are wired to the same requireAnalyticsTier middleware channel.go's
 * package doc names. See lib/ledger.ts's isAnalyticsTier. */
export { isAnalyticsTier } from '@/lib/ledger'

export type ChannelPeriodPreset = 'this_month' | 'last_month' | 'last_90_days' | 'custom'

export const CHANNEL_MAX_RANGE_DAYS = 92

function pad2(n: number): string {
  return String(n).padStart(2, '0')
}

/** yyyy-MM-dd in UTC — the wire format channel_from/channel_to expect. */
export function dateKey(d: Date): string {
  return `${d.getUTCFullYear()}-${pad2(d.getUTCMonth() + 1)}-${pad2(d.getUTCDate())}`
}

/** yyyy-MM in UTC — the wire format /channel/summary's `month` expects. */
export function monthKey(d: Date): string {
  return `${d.getUTCFullYear()}-${pad2(d.getUTCMonth() + 1)}`
}

function startOfUTCDay(d: Date): Date {
  return new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate()))
}

function addUTCDays(d: Date, days: number): Date {
  const next = new Date(d)
  next.setUTCDate(next.getUTCDate() + days)
  return next
}

function startOfUTCMonth(d: Date): Date {
  return new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), 1))
}

function endOfUTCMonth(d: Date): Date {
  return new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + 1, 0))
}

/** Inclusive day count between two yyyy-MM-dd date keys. */
export function rangeDays(from: string, to: string): number {
  const a = new Date(`${from}T00:00:00Z`)
  const b = new Date(`${to}T00:00:00Z`)
  return Math.round((b.getTime() - a.getTime()) / 86_400_000) + 1
}

/** True when [from, to] is a well-formed, non-inverted range within the
 * API's 92-day cap (R2) — the client-side check that stands in for letting
 * an out-of-range request 422 at the server. */
export function isValidRange(from: string, to: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(from) || !/^\d{4}-\d{2}-\d{2}$/.test(to)) return false
  const days = rangeDays(from, to)
  return days >= 1 && days <= CHANNEL_MAX_RANGE_DAYS
}

export type ChannelPeriod = {
  preset: ChannelPeriodPreset
  from: string
  to: string
  /** Set only for this_month/last_month — the two presets that map onto a
   * single calendar month for GET /channel/summary's `month` param. Absent
   * for last_90_days/custom, whose range spans a rolling window or an
   * arbitrary span the summary endpoint (monthly-only) has no way to
   * represent; the KPI row then falls back to the server's own default
   * (the current UTC month). */
  summaryMonth?: string
}

/** Resolves a period preset (+ optional custom bounds) to the from/to range
 * every non-summary channel endpoint needs, and the summary month where one
 * applies. `now` is injectable for tests. */
export function resolveChannelPeriod(
  preset: ChannelPeriodPreset,
  custom?: { from: string; to: string },
  now: Date = new Date(),
): ChannelPeriod {
  const today = startOfUTCDay(now)
  switch (preset) {
    case 'this_month': {
      const from = startOfUTCMonth(today)
      return { preset, from: dateKey(from), to: dateKey(today), summaryMonth: monthKey(today) }
    }
    case 'last_month': {
      const lastMonthAnchor = new Date(Date.UTC(today.getUTCFullYear(), today.getUTCMonth() - 1, 1))
      const from = startOfUTCMonth(lastMonthAnchor)
      const to = endOfUTCMonth(lastMonthAnchor)
      return { preset, from: dateKey(from), to: dateKey(to), summaryMonth: monthKey(lastMonthAnchor) }
    }
    case 'last_90_days': {
      const from = addUTCDays(today, -89)
      return { preset, from: dateKey(from), to: dateKey(today) }
    }
    case 'custom': {
      const from = custom?.from ?? dateKey(today)
      const to = custom?.to ?? dateKey(today)
      return { preset, from, to }
    }
  }
}

const KNOWN_SURFACES = ['all', 'google_ai_mode', 'google_gemini', 'openai_chatgpt', 'other_agent', 'unknown']

/** Minimal shape of vue-i18n's `t` — avoids importing vue-i18n's composer
 * type just for this helper's signature. */
type Translate = (key: string) => string

/** Plain-language label for a channel_surface value, via vue-i18n
 * (channel.surfaceLabel.* in src/locales). Falls back to a humanized form
 * of the raw string for any value not in the known set, so a surface added
 * server-side (R3 — e.g. a future real openai_chatgpt signal, or a wholly
 * new platform) renders legibly with zero frontend changes rather than
 * falling back to a blank or a raw enum string. Callers pass their
 * component's own `t` so this stays a plain function usable from both
 * <script setup> and non-component code — same pattern as
 * lib/ledger.ts's causeLabel. */
export function surfaceLabel(t: Translate, surface: string): string {
  if (KNOWN_SURFACES.includes(surface)) return t(`channel.surfaceLabel.${surface}`)
  return surface
    .split('_')
    .filter(Boolean)
    .map((w) => w[0].toUpperCase() + w.slice(1))
    .join(' ')
}

/** R7: the funnel's session-based stages must carry a visible qualifier
 * whenever any of the period's sessions had to be reconstructed rather than
 * observed directly. sessions_inferred/sessions_confirmed are counts, not a
 * single categorical attribution flag — an aggregate can honestly contain
 * both (internal/planes/control/api/channel.go's sessionsConfirmed doc
 * comment) — so "some inferred" is the qualifying condition, and the two
 * counts together are what the qualifier's tooltip reports. */
export function hasInferredSessions(sessionsInferred: number | undefined | null): boolean {
  return (sessionsInferred ?? 0) > 0
}

/** Currency display state for a summed range (channel_summary/
 * channel_funnel/channel_comparison's currency + currency_mixed pair). */
export type CurrencyState =
  | { kind: 'known'; currency: string }
  | { kind: 'mixed' }
  | { kind: 'none' }

export function resolveCurrencyState(currency: string | null | undefined, mixed: boolean | undefined): CurrencyState {
  if (mixed) return { kind: 'mixed' }
  if (currency) return { kind: 'known', currency }
  return { kind: 'none' }
}

/** Reproduces channel.go's aggregateCurrency client-side for the trend
 * chart: GET /channel/daily returns one row per store/surface/day, each
 * with its own single-row currency, never a rolled-up currency/
 * currency_mixed pair the way summary/funnel/comparison do (a single row is
 * never ambiguous, so the API doesn't need one). Once the chart collapses
 * several already-fetched rows into one point per date (surface filter =
 * "all"), the same agree-or-flag-mixed question the backend answers for its
 * own rollups applies here too — this is that same logic run over data
 * already on the wire, not an inference. */
export function resolveDailyCurrency(rows: Array<{ currency?: string | null }>): CurrencyState {
  const seen = new Set(rows.map((r) => r.currency).filter((c): c is string => !!c))
  if (seen.size === 0) return { kind: 'none' }
  if (seen.size > 1) return { kind: 'mixed' }
  return { kind: 'known', currency: [...seen][0] }
}

/** Zero-safe ratio between two consecutive funnel-stage counts, computed at
 * render time from the API's raw counts (R6) — used for the funnel's
 * stage-to-stage conversion rates rather than channel_rates.completion_rate,
 * whose denominator (checkouts_initiated) equals `sessions` by construction
 * (every session here is checkout-anchored — see channel/model.go), not
 * sessions_reached_checkout. That makes completion_rate the
 * whole-funnel conversion, not the reached-checkout→completed segment R6's
 * funnel needs between its last two stages. */
export function stageRate(numerator: number | undefined | null, denominator: number | undefined | null): number {
  const d = denominator ?? 0
  if (d === 0) return 0
  return (numerator ?? 0) / d
}

/** GET /channel/daily never aggregates across surfaces server-side — it
 * returns "one row per store/surface/day in range" even when no `surface`
 * filter is given (internal/planes/control/channel/mongo_store.go's
 * ListDailyAggregates only filters by surface when one is passed; omitted,
 * every surface's row for that date comes back separately). The trend
 * chart needs exactly one point per date, so when the page's surface
 * filter is "all", this sums the already-fetched per-surface counts for
 * each date — a plain sum of raw counts already on the wire, not a derived
 * or estimated figure. */
export function collapseDailyBySurface<
  T extends {
    date?: string
    checkouts_initiated?: number
    checkouts_completed?: number
    revenue_minor?: number
  },
>(rows: T[]): Array<{ date: string; checkouts_initiated: number; checkouts_completed: number; revenue_minor: number }> {
  const byDate = new Map<string, { date: string; checkouts_initiated: number; checkouts_completed: number; revenue_minor: number }>()
  for (const r of rows) {
    if (!r.date) continue
    const acc = byDate.get(r.date) ?? { date: r.date, checkouts_initiated: 0, checkouts_completed: 0, revenue_minor: 0 }
    acc.checkouts_initiated += r.checkouts_initiated ?? 0
    acc.checkouts_completed += r.checkouts_completed ?? 0
    acc.revenue_minor += r.revenue_minor ?? 0
    byDate.set(r.date, acc)
  }
  return [...byDate.values()].sort((a, b) => a.date.localeCompare(b.date))
}

/** Formats a yyyy-MM-dd UTC date key as a short local-timezone date label
 * for the trend chart's X axis (R9) — the instant midnight UTC on that day
 * falls on, rendered in whatever timezone the browser is in. The bucket
 * itself stays a UTC calendar day; see ChannelTrendChart.vue's on-chart note
 * for the disclosure that pairs with this (R9: never silently re-bucket). */
export function localDateLabel(dateKeyUtc: string): string {
  const d = new Date(`${dateKeyUtc}T00:00:00Z`)
  return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

/** R8: the funnel must degrade gracefully rather than render empty/broken
 * when there's no session data for the period. The API has no explicit
 * "sessionization unavailable" flag (checkouts_initiated is defined to
 * equal sessions by construction — see channel/model.go — so the "checkout
 * data survives without session data" case R8 describes cannot occur under
 * this backend's model); `sessions === 0` is the closest honest read of
 * "there is nothing to show". */
export function hasNoFunnelData(sessions: number | undefined | null): boolean {
  return (sessions ?? 0) === 0
}
