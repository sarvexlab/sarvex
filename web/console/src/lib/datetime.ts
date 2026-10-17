// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Friendly datetime formatting shared across the console — relative for
// anything recent enough that "N units ago" is more useful than a date
// ("5 minutes ago", "yesterday"), falling back to a readable absolute
// date/time once it's far enough back that relative phrasing stops helping.
// No date library: Intl.RelativeTimeFormat/DateTimeFormat cover this natively.

const RELATIVE_UNITS: Array<[Intl.RelativeTimeFormatUnit, number]> = [
  ['year', 60 * 60 * 24 * 365],
  ['month', 60 * 60 * 24 * 30],
  ['week', 60 * 60 * 24 * 7],
  ['day', 60 * 60 * 24],
  ['hour', 60 * 60],
  ['minute', 60],
  ['second', 1],
]

const relativeFormatter = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' })
const absoluteFormatter = new Intl.DateTimeFormat(undefined, {
  dateStyle: 'medium',
  timeStyle: 'short',
})

const RELATIVE_WINDOW_SECONDS = 60 * 60 * 24 * 7 // 1 week

/**
 * Formats an ISO datetime string as relative time within the last week
 * ("5 minutes ago", "3 days ago"), or a readable absolute date/time beyond
 * that. Returns `fallback` for null/undefined/unparseable input.
 */
export function formatFriendlyDateTime(iso: string | null | undefined, fallback = 'Never'): string {
  if (!iso) return fallback
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return fallback

  const diffSeconds = (date.getTime() - Date.now()) / 1000
  const absSeconds = Math.abs(diffSeconds)

  if (absSeconds < RELATIVE_WINDOW_SECONDS) {
    for (const [unit, secondsInUnit] of RELATIVE_UNITS) {
      if (absSeconds >= secondsInUnit || unit === 'second') {
        const value = Math.round(diffSeconds / secondsInUnit)
        return relativeFormatter.format(value, unit)
      }
    }
  }
  return absoluteFormatter.format(date)
}

/**
 * Always-relative phrasing ("5 minutes ago", "in 2 hours"), regardless of
 * how far in the past/future — for read-outs (Checkout Escalation Console
 * timeline, "updated Xs ago" poll indicator) that always want the relative
 * form, with the absolute instant available separately via
 * `formatUTCTimestamp` for a hover/title (R13: the backend stores UTC and
 * the UI must not obscure that). Returns `fallback` for null/undefined/
 * unparseable input.
 */
export function formatRelativeTime(iso: string | null | undefined, fallback = '—'): string {
  if (!iso) return fallback
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return fallback

  const diffSeconds = (date.getTime() - Date.now()) / 1000
  const absSeconds = Math.abs(diffSeconds)
  for (const [unit, secondsInUnit] of RELATIVE_UNITS) {
    if (absSeconds >= secondsInUnit || unit === 'second') {
      const value = Math.round(diffSeconds / secondsInUnit)
      return relativeFormatter.format(value, unit)
    }
  }
  return relativeFormatter.format(0, 'second')
}

const utcFormatter = new Intl.DateTimeFormat(undefined, {
  dateStyle: 'medium',
  timeStyle: 'medium',
  timeZone: 'UTC',
})

/**
 * Absolute UTC instant, formatted for a hover/title attribute alongside a
 * relative-time label — pairs with `formatRelativeTime`. Returns `fallback`
 * for null/undefined/unparseable input.
 */
export function formatUTCTimestamp(iso: string | null | undefined, fallback = '—'): string {
  if (!iso) return fallback
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return fallback
  return `${utcFormatter.format(date)} UTC`
}

/**
 * Elapsed duration between two ISO instants (or `from` and now, if `to` is
 * omitted) as a compact "Xh Ym" / "Ym Zs" string — for a session's elapsed
 * time or an unresolved escalation's time-since-handoff.
 */
export function formatDuration(from: string | null | undefined, to?: string | null): string {
  if (!from) return '—'
  const start = new Date(from)
  if (Number.isNaN(start.getTime())) return '—'
  const end = to ? new Date(to) : new Date()
  if (Number.isNaN(end.getTime())) return '—'

  const totalSeconds = Math.max(0, Math.round((end.getTime() - start.getTime()) / 1000))
  const days = Math.floor(totalSeconds / 86400)
  const hours = Math.floor((totalSeconds % 86400) / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60

  if (days > 0) return `${days}d ${hours}h`
  if (hours > 0) return `${hours}h ${minutes}m`
  if (minutes > 0) return `${minutes}m ${seconds}s`
  return `${seconds}s`
}
