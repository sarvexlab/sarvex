// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, vi, afterEach } from 'vitest'
import { formatFriendlyDateTime } from './datetime'

describe('formatFriendlyDateTime', () => {
  const now = new Date('2026-07-16T12:00:00Z')

  afterEach(() => {
    vi.useRealTimers()
  })

  it('returns the fallback for null/undefined/empty input', () => {
    expect(formatFriendlyDateTime(null)).toBe('Never')
    expect(formatFriendlyDateTime(undefined)).toBe('Never')
    expect(formatFriendlyDateTime('')).toBe('Never')
  })

  it('returns a custom fallback when provided', () => {
    expect(formatFriendlyDateTime(null, 'Not yet')).toBe('Not yet')
  })

  it('returns the fallback for an unparseable string', () => {
    expect(formatFriendlyDateTime('not-a-date')).toBe('Never')
  })

  it('formats a few minutes ago relatively', () => {
    vi.useFakeTimers()
    vi.setSystemTime(now)
    const fiveMinAgo = new Date(now.getTime() - 5 * 60 * 1000).toISOString()
    expect(formatFriendlyDateTime(fiveMinAgo)).toBe('5 minutes ago')
  })

  it('formats a few hours ago relatively', () => {
    vi.useFakeTimers()
    vi.setSystemTime(now)
    const threeHoursAgo = new Date(now.getTime() - 3 * 60 * 60 * 1000).toISOString()
    expect(formatFriendlyDateTime(threeHoursAgo)).toBe('3 hours ago')
  })

  it('falls back to an absolute date beyond the relative window', () => {
    vi.useFakeTimers()
    vi.setSystemTime(now)
    const twoWeeksAgo = new Date(now.getTime() - 14 * 24 * 60 * 60 * 1000).toISOString()
    const result = formatFriendlyDateTime(twoWeeksAgo)
    // Exact locale rendering varies by environment; assert it's not a
    // relative phrase and does contain the year.
    expect(result).not.toMatch(/ago$/)
    expect(result).toContain('2026')
  })
})
