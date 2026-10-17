// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Money formatting. The wire shape is always integer minor units + an
// ISO-4217 currency (sarvex-v1-control-plane.yaml `money`), so every display
// path has to divide before formatting. Intl carries the per-currency
// fraction digits, so no per-currency table is needed here.

import type { components } from '@/api/schema'

type Money = components['schemas']['money']

const formatters = new Map<string, Intl.NumberFormat>()

function formatterFor(currency: string): Intl.NumberFormat {
  let f = formatters.get(currency)
  if (!f) {
    f = new Intl.NumberFormat(undefined, { style: 'currency', currency })
    formatters.set(currency, f)
  }
  return f
}

/**
 * Formats integer minor units as a localised currency amount
 * ("$2,140.00"). Returns `fallback` when the currency is missing or not a
 * currency Intl recognises.
 */
export function formatMoney(money: Money | null | undefined, fallback = '—'): string {
  if (!money?.currency) return fallback
  return formatMinorUnits(money.amount_minor ?? 0, money.currency, fallback)
}

/**
 * Same conversion as `formatMoney`, for wire shapes that don't use the
 * shared `money` schema's `amount_minor` field name (e.g. `cart_summary`'s
 * `total_minor` — sarvex-v1-control-plane.yaml).
 */
export function formatMinorUnits(minor: number, currency: string | undefined, fallback = '—'): string {
  if (!currency) return fallback
  try {
    const f = formatterFor(currency)
    const digits = f.resolvedOptions().maximumFractionDigits ?? 2
    return f.format(minor / 10 ** digits)
  } catch {
    return fallback
  }
}

/**
 * Sums a set of amounts that share a currency. Amounts in any other currency
 * are ignored — the caller picks the dominant currency first (see
 * `dominantCurrency`), since a mixed-currency total is meaningless.
 */
export function sumMoney(values: Array<Money | undefined>, currency: string): Money {
  const amount_minor = values.reduce(
    (acc, v) => (v?.currency === currency ? acc + (v.amount_minor ?? 0) : acc),
    0,
  )
  return { amount_minor, currency }
}

/** The currency that appears most often in `values`, or null if there are none. */
export function dominantCurrency(values: Array<Money | undefined>): string | null {
  const counts = new Map<string, number>()
  for (const v of values) {
    if (!v?.currency) continue
    counts.set(v.currency, (counts.get(v.currency) ?? 0) + 1)
  }
  let best: string | null = null
  let bestCount = 0
  for (const [currency, count] of counts) {
    if (count > bestCount) {
      best = currency
      bestCount = count
    }
  }
  return best
}
