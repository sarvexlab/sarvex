// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest'
import { formatMoney, sumMoney, dominantCurrency } from './money'

describe('formatMoney', () => {
  it('divides minor units by the currency’s fraction digits', () => {
    expect(formatMoney({ amount_minor: 214000, currency: 'USD' })).toBe('$2,140.00')
  })

  it('handles zero-decimal currencies without shifting the amount', () => {
    // JPY has no minor unit, so 2140 minor units is ¥2,140 — not ¥21.40.
    expect(formatMoney({ amount_minor: 2140, currency: 'JPY' })).toBe('¥2,140')
  })

  it('falls back when the money is missing or the currency is unusable', () => {
    expect(formatMoney(null)).toBe('—')
    expect(formatMoney(undefined)).toBe('—')
    expect(formatMoney({ amount_minor: 100, currency: 'not-a-currency' }, 'n/a')).toBe('n/a')
  })
})

describe('sumMoney', () => {
  it('adds only the amounts in the requested currency', () => {
    const total = sumMoney(
      [
        { amount_minor: 120000, currency: 'USD' },
        { amount_minor: 94000, currency: 'USD' },
        { amount_minor: 999999, currency: 'EUR' },
        undefined,
      ],
      'USD',
    )
    expect(total).toEqual({ amount_minor: 214000, currency: 'USD' })
  })

  it('returns zero rather than throwing on an empty set', () => {
    expect(sumMoney([], 'AUD')).toEqual({ amount_minor: 0, currency: 'AUD' })
  })
})

describe('dominantCurrency', () => {
  it('picks the most common currency', () => {
    expect(
      dominantCurrency([
        { amount_minor: 1, currency: 'USD' },
        { amount_minor: 1, currency: 'EUR' },
        { amount_minor: 1, currency: 'USD' },
      ]),
    ).toBe('USD')
  })

  it('is null when nothing has a currency', () => {
    expect(dominantCurrency([undefined, {} as never])).toBeNull()
  })
})
