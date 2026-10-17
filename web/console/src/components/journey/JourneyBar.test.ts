// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// JourneyBar renders the activation lifecycle keyed off the OpenAPI
// ucp_state enum. Pin: stage order, current/done/todo classes, selectable
// emit.
//
// The bar used to run on journey_state and end at "Live" meaning live on
// Google. It now ends at "Active" — the store's own endpoints live and
// monitored — which is a milestone every merchant can actually reach.

import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { i18n } from '@/i18n'
import JourneyBar from './JourneyBar.vue'

describe('JourneyBar', () => {
  it('renders the activation stages in order, ending at Active', () => {
    const w = mount(JourneyBar, { global: { plugins: [i18n] }, props: { current: 'draft' } })
    const labels = w.findAll('.jlabel').map((n) => n.text())
    expect(labels).toEqual(['Draft', 'Connected', 'Published', 'Verified', 'Active'])
  })

  // `suspended` is a condition, not a step, so it never appears as a node —
  // the same reason it is absent from activation.Order() on the backend.
  it('does not render suspended as a step', () => {
    const w = mount(JourneyBar, { global: { plugins: [i18n] }, props: { current: 'draft' } })
    const labels = w.findAll('.jlabel').map((n) => n.text())
    expect(labels).not.toContain('Suspended')
  })

  it('names no distribution partner', () => {
    const w = mount(JourneyBar, { global: { plugins: [i18n] }, props: { current: 'active' } })
    expect(w.text()).not.toMatch(/google/i)
  })

  it('marks earlier stages done, current as current, later as todo', () => {
    const w = mount(JourneyBar, {
      global: { plugins: [i18n] },
      props: { current: 'profile_published' },
    })
    const steps = w.findAll('.jstep')
    expect(steps[0].classes()).toContain('done')
    expect(steps[1].classes()).toContain('done')
    expect(steps[2].classes()).toContain('current')
    expect(steps[3].classes()).toContain('todo')
    expect(steps[4].classes()).toContain('todo')
  })

  it('renders as <div> by default and <button> when selectable', () => {
    const ro = mount(JourneyBar, { global: { plugins: [i18n] }, props: { current: 'draft' } })
    expect(ro.find('button.jstep').exists()).toBe(false)

    const sel = mount(JourneyBar, {
      global: { plugins: [i18n] },
      props: { current: 'draft', selectable: true },
    })
    expect(sel.findAll('button.jstep').length).toBe(5)
  })

  it('emits select with the clicked stage id when selectable', async () => {
    const w = mount(JourneyBar, {
      global: { plugins: [i18n] },
      props: { current: 'draft', selectable: true },
    })
    await w.findAll('button.jstep')[2].trigger('click')
    expect(w.emitted('select')?.[0]).toEqual(['profile_published'])
  })

  it('applies the selected class when selected differs from current', () => {
    const w = mount(JourneyBar, {
      global: { plugins: [i18n] },
      props: { current: 'draft', selected: 'active' },
    })
    const last = w.findAll('.jstep')[4]
    expect(last.classes()).toContain('selected')
  })
})
