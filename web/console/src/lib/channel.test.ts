// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest'
import { i18n } from '@/i18n'
import {
  CHANNEL_MAX_RANGE_DAYS,
  collapseDailyBySurface,
  dateKey,
  hasInferredSessions,
  hasNoFunnelData,
  isValidRange,
  localDateLabel,
  monthKey,
  rangeDays,
  resolveChannelPeriod,
  stageRate,
  surfaceLabel,
} from './channel'

describe('dateKey / monthKey', () => {
  it('formats a UTC date as yyyy-MM-dd / yyyy-MM', () => {
    const d = new Date(Date.UTC(2026, 7, 6)) // 2026-08-06
    expect(dateKey(d)).toBe('2026-08-06')
    expect(monthKey(d)).toBe('2026-08')
  })
})

describe('rangeDays / isValidRange', () => {
  it('counts inclusive days', () => {
    expect(rangeDays('2026-08-01', '2026-08-01')).toBe(1)
    expect(rangeDays('2026-08-01', '2026-08-31')).toBe(31)
  })

  it('accepts a range up to the 92-day cap', () => {
    expect(isValidRange('2026-01-01', '2026-04-02')).toBe(true) // exactly 92 days
  })

  it('rejects a range over the 92-day cap (R2)', () => {
    expect(isValidRange('2026-01-01', '2026-04-03')).toBe(false) // 93 days
  })

  it('rejects an inverted range', () => {
    expect(isValidRange('2026-08-10', '2026-08-01')).toBe(false)
  })

  it('rejects malformed dates', () => {
    expect(isValidRange('2026/08/01', '2026-08-10')).toBe(false)
  })

  it('exposes the cap as a constant matching the OpenAPI contract', () => {
    expect(CHANNEL_MAX_RANGE_DAYS).toBe(92)
  })
})

describe('resolveChannelPeriod', () => {
  const now = new Date('2026-08-06T15:00:00Z')

  it('this_month: from the 1st of the current UTC month through today, with a summary month', () => {
    const p = resolveChannelPeriod('this_month', undefined, now)
    expect(p).toEqual({ preset: 'this_month', from: '2026-08-01', to: '2026-08-06', summaryMonth: '2026-08' })
  })

  it('last_month: the full previous UTC calendar month, with a summary month', () => {
    const p = resolveChannelPeriod('last_month', undefined, now)
    expect(p).toEqual({ preset: 'last_month', from: '2026-07-01', to: '2026-07-31', summaryMonth: '2026-07' })
  })

  it('last_90_days: a 90-day rolling window ending today, with no summary month', () => {
    const p = resolveChannelPeriod('last_90_days', undefined, now)
    expect(p.to).toBe('2026-08-06')
    expect(rangeDays(p.from, p.to)).toBe(90)
    expect(p.summaryMonth).toBeUndefined()
  })

  it('custom: uses the caller-supplied bounds, with no summary month', () => {
    const p = resolveChannelPeriod('custom', { from: '2026-05-01', to: '2026-05-15' }, now)
    expect(p).toEqual({ preset: 'custom', from: '2026-05-01', to: '2026-05-15' })
  })

  it('last_month rolls the year back correctly in January', () => {
    const jan = new Date('2026-01-15T00:00:00Z')
    const p = resolveChannelPeriod('last_month', undefined, jan)
    expect(p.summaryMonth).toBe('2025-12')
    expect(p.from).toBe('2025-12-01')
    expect(p.to).toBe('2025-12-31')
  })
})

describe('surfaceLabel (R3)', () => {
  const t = i18n.global.t

  it('labels known channel_surface values', () => {
    expect(surfaceLabel(t, 'google_ai_mode')).toBe('Google AI Mode')
    expect(surfaceLabel(t, 'google_gemini')).toBe('Google Gemini')
  })

  it('humanizes an unrecognized surface instead of failing — a server-added surface renders with no frontend change', () => {
    expect(surfaceLabel(t, 'brand_new_agent_platform')).toBe('Brand New Agent Platform')
  })
})

describe('hasInferredSessions / hasNoFunnelData (R7/R8)', () => {
  it('flags inferred only when the count is positive', () => {
    expect(hasInferredSessions(0)).toBe(false)
    expect(hasInferredSessions(undefined)).toBe(false)
    expect(hasInferredSessions(3)).toBe(true)
  })

  it('flags no-data only when sessions is exactly zero', () => {
    expect(hasNoFunnelData(0)).toBe(true)
    expect(hasNoFunnelData(undefined)).toBe(true)
    expect(hasNoFunnelData(1)).toBe(false)
  })
})

describe('stageRate', () => {
  it('divides zero-safely', () => {
    expect(stageRate(50, 100)).toBe(0.5)
    expect(stageRate(5, 0)).toBe(0)
    expect(stageRate(undefined, undefined)).toBe(0)
  })
})

describe('collapseDailyBySurface', () => {
  it('sums same-date rows from different surfaces into one point', () => {
    const rows = [
      { date: '2026-08-01', surface: 'google_ai_mode' as const, checkouts_initiated: 10, checkouts_completed: 6, revenue_minor: 1000 },
      { date: '2026-08-01', surface: 'google_gemini' as const, checkouts_initiated: 4, checkouts_completed: 2, revenue_minor: 400 },
      { date: '2026-08-02', surface: 'google_ai_mode' as const, checkouts_initiated: 5, checkouts_completed: 3, revenue_minor: 500 },
    ]
    const collapsed = collapseDailyBySurface(rows)
    expect(collapsed).toEqual([
      { date: '2026-08-01', checkouts_initiated: 14, checkouts_completed: 8, revenue_minor: 1400 },
      { date: '2026-08-02', checkouts_initiated: 5, checkouts_completed: 3, revenue_minor: 500 },
    ])
  })

  it('returns dates sorted ascending regardless of input order', () => {
    const rows = [
      { date: '2026-08-03', checkouts_initiated: 1, checkouts_completed: 1, revenue_minor: 1 },
      { date: '2026-08-01', checkouts_initiated: 1, checkouts_completed: 1, revenue_minor: 1 },
    ]
    expect(collapseDailyBySurface(rows).map((r) => r.date)).toEqual(['2026-08-01', '2026-08-03'])
  })
})

describe('localDateLabel', () => {
  it('renders a short local-timezone label for a UTC date bucket', () => {
    // Exact wording depends on the test runner's locale/timezone; just
    // assert it produced a non-empty, plausible label rather than throwing
    // or returning the raw key.
    const label = localDateLabel('2026-08-06')
    expect(label).not.toBe('')
    expect(label).not.toBe('2026-08-06')
  })
})
