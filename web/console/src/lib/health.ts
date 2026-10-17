// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Health-page vocabulary: turning the monitoring system's internal names and
// numbers into something a merchant can act on.
//
// The Health page showed raw probe ids (`signature_roundtrip`), raw statement
// kinds (`sync_freshness`), Go duration strings ("2h0m58.304129489s") and a
// bare status word ("degraded") with no indication of what any of it meant or
// whether the merchant needed to do anything. Everything here exists to answer
// three questions per row: what is this, what does the current value mean, and
// is it mine to fix.

/** The four synthetic probes, in the order they run against a store. */
const CHECK_ORDER = [
  'profile_reachable',
  'signature_roundtrip',
  'negotiation_handshake',
  'catalog_lookup',
] as const


/** i18n key suffix for a check's label and explanation. Unknown check types
 *  fall back to their raw id rather than rendering an empty row. */
export function checkKey(checkType: string): string | null {
  return (CHECK_ORDER as readonly string[]).includes(checkType) ? checkType : null
}

/** Orders checks by the probe sequence, unknown ones last, so the list reads
 *  the same way every time rather than in map-iteration order. */
export function sortChecks<T>(entries: Array<[string, T]>): Array<[string, T]> {
  const rank = (c: string) => {
    const i = (CHECK_ORDER as readonly string[]).indexOf(c)
    return i === -1 ? CHECK_ORDER.length : i
  }
  return [...entries].sort(([a], [b]) => rank(a) - rank(b) || a.localeCompare(b))
}

/** True when a check state means something is actually wrong right now. */
export function isFailingState(state: string | undefined): boolean {
  return state === 'fail' || state === 'degraded' || state === 'timeout'
}

/**
 * A duration in seconds as a locale-formatted span ("2 hours", "45 minutes").
 *
 * Intl.NumberFormat's unit style rather than RelativeTimeFormat: we want the
 * length of time, not a point in it, and RelativeTimeFormat only produces
 * "2 hours ago" — whose unit word cannot be separated from the "ago" without
 * parsing locale-specific literals.
 *
 * Deliberately rounds to a single unit. The server's figure carries
 * nanosecond precision, which implies a measurement far more exact than
 * "your catalog is behind" warrants.
 */
export function formatApproximateAge(seconds: number | null | undefined): string | null {
  if (seconds == null || !Number.isFinite(seconds) || seconds < 0) return null

  const units: Array<[string, number]> = [
    ['day', 60 * 60 * 24],
    ['hour', 60 * 60],
    ['minute', 60],
    ['second', 1],
  ]

  for (const [unit, secondsInUnit] of units) {
    if (seconds >= secondsInUnit || unit === 'second') {
      const value = Math.max(1, Math.round(seconds / secondsInUnit))
      return new Intl.NumberFormat(undefined, {
        style: 'unit',
        unit,
        unitDisplay: 'long',
      }).format(value)
    }
  }
  return null
}

/** Reads age_seconds out of a statement's params, whatever numeric shape it
 *  arrived in. */
export function ageSeconds(params: Record<string, unknown> | undefined): number | null {
  const raw = params?.age_seconds
  return typeof raw === 'number' ? raw : null
}

/** Reads a count out of a statement's params. */
export function countParam(
  params: Record<string, unknown> | undefined,
  key: 'critical_count' | 'warning_count',
): number {
  const raw = params?.[key]
  return typeof raw === 'number' ? raw : 0
}

/**
 * Consistency findings are keyed by SKU, except the ones the plugin-gateway
 * probe raises, which use a `system:` prefix because they are about the
 * integration rather than any product. Rendering those as a product SKU is
 * what produced "system:plugin_gateway · field server_health".
 */
export function isSystemFinding(sku: string | undefined): boolean {
  return !!sku?.startsWith('system:')
}

/** The subject of a `system:` finding, e.g. "plugin_gateway". */
export function systemFindingSubject(sku: string | undefined): string {
  return sku?.slice('system:'.length) ?? ''
}
