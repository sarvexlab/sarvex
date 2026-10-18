// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest'
import { dimLabel } from './readiness'

describe('dimLabel', () => {
  it('names the four readiness areas in plain English', () => {
    expect(dimLabel('completeness')).toBe('Complete listings')
    expect(dimLabel('consistency')).toBe('Consistent data')
    expect(dimLabel('attribute_quality')).toBe('Attribute quality')
    expect(dimLabel('delivery_data')).toBe('Delivery info')
  })

  it('falls back to a readable name for an area it does not know', () => {
    expect(dimLabel('some_new_area')).toBe('some new area')
  })
})
