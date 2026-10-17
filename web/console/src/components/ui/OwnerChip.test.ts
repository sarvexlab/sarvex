// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// OwnerChip is the only place that maps the OpenAPI task_owner enum
// (merchant / sarvex / external) to the design's visual styles (you / us /
// external) and labels. Pin the mapping.

import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { i18n } from '@/i18n'
import OwnerChip from './OwnerChip.vue'

describe('OwnerChip', () => {
  it('renders You for merchant owner with the accent chip', () => {
    const w = mount(OwnerChip, { global: { plugins: [i18n] }, props: { owner: 'merchant' } })
    expect(w.text()).toContain('You')
    expect(w.html()).toContain('chip-you')
  })

  it('renders Sarvex for sarvex owner with the muted chip', () => {
    const w = mount(OwnerChip, { global: { plugins: [i18n] }, props: { owner: 'sarvex' } })
    expect(w.text()).toContain('Sarvex')
    expect(w.html()).toContain('chip-us')
  })

  it('renders External for external owner with the outlined chip', () => {
    const w = mount(OwnerChip, { global: { plugins: [i18n] }, props: { owner: 'external' } })
    expect(w.text()).toContain("External")
    expect(w.html()).toContain('chip-external')
  })
})
