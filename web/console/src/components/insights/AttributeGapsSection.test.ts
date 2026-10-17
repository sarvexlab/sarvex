// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { i18n } from '@/i18n'
import AttributeGapsSection from './AttributeGapsSection.vue'

describe('AttributeGapsSection — no backend endpoint (R14)', () => {
  it('renders an honest unavailable explanation rather than empty/broken content', () => {
    const w = mount(AttributeGapsSection, { global: { plugins: [i18n] } })
    expect(w.text()).toContain('Not available yet')
    expect(w.text()).toContain('no way to name a requested attribute')
  })
})
