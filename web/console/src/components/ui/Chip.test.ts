// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import Chip from './Chip.vue'

describe('Chip', () => {
  it.each([
    ['you', 'chip-you'],
    ['us', 'chip-us'],
    ['external', 'chip-external'],
    ['ok', 'chip-ok'],
    ['warn', 'chip-warn'],
    ['danger', 'chip-danger'],
    ['quiet', 'chip-quiet'],
  ] as const)('maps kind=%s to class %s', (kind, expected) => {
    const w = mount(Chip, { props: { kind }, slots: { default: 'Hello' } })
    expect(w.classes()).toContain(expected)
    expect(w.text()).toBe('Hello')
  })
})
