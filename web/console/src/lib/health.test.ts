// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// The Health page's vocabulary helpers. Each test here corresponds to
// something the page previously showed a merchant verbatim from the
// monitoring system's internals.

import { describe, it, expect } from 'vitest'
import {
  ageSeconds,
  checkKey,
  countParam,
  formatApproximateAge,
  isFailingState,
  isSystemFinding,
  sortChecks,
  systemFindingSubject,
} from './health'

describe('formatApproximateAge', () => {
  // The server sends a Go duration; the page rendered it raw, so a two-hour
  // lag read "2h0m58.304129489s".
  it('renders a span in whole units, not nanosecond precision', () => {
    expect(formatApproximateAge(7597)).toBe('2 hours')
    expect(formatApproximateAge(125)).toBe('2 minutes')
    expect(formatApproximateAge(45)).toBe('45 seconds')
    expect(formatApproximateAge(90000)).toBe('1 day')
  })

  // A span, not a point in time: "2 hours", never "2 hours ago" — the copy
  // supplies the "ago".
  it('does not phrase the duration as a past instant', () => {
    expect(formatApproximateAge(7597)).not.toMatch(/ago/)
  })

  it('never renders a zero span', () => {
    expect(formatApproximateAge(0)).toBe('1 second')
  })

  it('returns null for absent or nonsensical input', () => {
    expect(formatApproximateAge(null)).toBeNull()
    expect(formatApproximateAge(undefined)).toBeNull()
    expect(formatApproximateAge(-5)).toBeNull()
    expect(formatApproximateAge(NaN)).toBeNull()
  })
})

describe('check identity', () => {
  it('recognises the four synthetic probes', () => {
    expect(checkKey('signature_roundtrip')).toBe('signature_roundtrip')
    expect(checkKey('profile_reachable')).toBe('profile_reachable')
  })

  // An unknown check must fall back to its raw id rather than render blank.
  it('reports an unknown check so the caller can fall back', () => {
    expect(checkKey('something_new')).toBeNull()
  })

  it('orders checks by the probe sequence, unknown ones last', () => {
    const entries: Array<[string, number]> = [
      ['catalog_lookup', 1],
      ['zzz_unknown', 2],
      ['profile_reachable', 3],
      ['signature_roundtrip', 4],
    ]
    expect(sortChecks(entries).map(([k]) => k)).toEqual([
      'profile_reachable',
      'signature_roundtrip',
      'catalog_lookup',
      'zzz_unknown',
    ])
  })

  // Degraded and timeout are failures for the purpose of explaining a
  // non-healthy composite status; only these states name a reason.
  it('counts degraded, fail and timeout as currently failing', () => {
    expect(isFailingState('degraded')).toBe(true)
    expect(isFailingState('fail')).toBe(true)
    expect(isFailingState('timeout')).toBe(true)
    expect(isFailingState('pass')).toBe(false)
    expect(isFailingState(undefined)).toBe(false)
  })
})

describe('statement params', () => {
  it('reads counts and ages, defaulting safely when absent', () => {
    expect(countParam({ critical_count: 1, warning_count: 0 }, 'critical_count')).toBe(1)
    expect(countParam(undefined, 'warning_count')).toBe(0)
    expect(countParam({ critical_count: 'nope' }, 'critical_count')).toBe(0)
    expect(ageSeconds({ age_seconds: 7597 })).toBe(7597)
    expect(ageSeconds({})).toBeNull()
    expect(ageSeconds(undefined)).toBeNull()
  })
})

describe('system findings', () => {
  // The plugin-gateway probe raises findings keyed system:plugin_gateway,
  // which the page rendered as though it were a product SKU:
  // "system:plugin_gateway · field server_health".
  it('distinguishes an integration finding from a product one', () => {
    expect(isSystemFinding('system:plugin_gateway')).toBe(true)
    expect(isSystemFinding('SIM-0001')).toBe(false)
    expect(isSystemFinding(undefined)).toBe(false)
  })

  it('extracts the subject so it can be named in plain language', () => {
    expect(systemFindingSubject('system:plugin_gateway')).toBe('plugin_gateway')
  })
})
