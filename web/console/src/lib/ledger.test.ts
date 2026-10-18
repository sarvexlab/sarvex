// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest'
import {
  isFixableCause,
  forwardStatuses,
  canTransition,
  sortCausesByRevenue,
  sortCausesByCount,
} from './ledger'

describe('isFixableCause', () => {
  it('treats buyer_abandoned as not fixable', () => {
    expect(isFixableCause('buyer_abandoned')).toBe(false)
  })
  it('treats every other cause as fixable', () => {
    expect(isFixableCause('payment_handler')).toBe(true)
    expect(isFixableCause('unclassified')).toBe(true)
  })
  it('treats null/undefined as not fixable', () => {
    expect(isFixableCause(null)).toBe(false)
    expect(isFixableCause(undefined)).toBe(false)
  })
})

describe('forward-only status transitions', () => {
  it('open can move to acknowledged or resolved', () => {
    expect(forwardStatuses('open')).toEqual(['acknowledged', 'resolved'])
  })
  it('acknowledged can only move to resolved', () => {
    expect(forwardStatuses('acknowledged')).toEqual(['resolved'])
  })
  it('resolved is terminal', () => {
    expect(forwardStatuses('resolved')).toEqual([])
  })
  it('canTransition rejects backward and sideways moves', () => {
    expect(canTransition('resolved', 'open')).toBe(false)
    expect(canTransition('acknowledged', 'open')).toBe(false)
    expect(canTransition('open', 'open')).toBe(false)
  })
  it('canTransition allows skipping acknowledged (still forward)', () => {
    expect(canTransition('open', 'resolved')).toBe(true)
  })
})

describe('sortCausesByRevenue', () => {
  it('sorts descending by lost_revenue_minor without mutating the input', () => {
    const rows = [
      { cause: 'a', lost_revenue_minor: 100 },
      { cause: 'b', lost_revenue_minor: 500 },
      { cause: 'c', lost_revenue_minor: 200 },
    ]
    const sorted = sortCausesByRevenue(rows)
    expect(sorted.map((r) => r.cause)).toEqual(['b', 'c', 'a'])
    expect(rows.map((r) => r.cause)).toEqual(['a', 'b', 'c'])
  })
})

describe('sortCausesByCount', () => {
  it('sorts descending by count', () => {
    const rows = [
      { cause: 'a', count: 3 },
      { cause: 'b', count: 9 },
      { cause: 'c', count: 1 },
    ]
    expect(sortCausesByCount(rows).map((r) => r.cause)).toEqual(['b', 'a', 'c'])
  })
})
