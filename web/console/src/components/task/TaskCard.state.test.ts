// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// One appearance per state.
//
// The card treatment used to key off owner while the status chip keyed off
// state, so the two could disagree — a Google-owned checklist task raised
// `open` drew the dashed waiting card but no waiting chip, and `in_progress`
// was pixel-identical to `open`. State is derived server-side from owner and
// resolution now (domain.DeriveTaskState), so it is the one input that
// decides how the card looks.

import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { i18n } from '@/i18n'
import TaskCard from './TaskCard.vue'
import type { components } from '@/api/schema'

type Task = components['schemas']['task']
type State = NonNullable<Task['state']>

const task = (over: Partial<Task> = {}): Task => ({
  task_id: 't-1',
  store_id: 's1',
  title: 'A task',
  why_it_matters: 'Because.',
  owner: 'merchant',
  resolution: 'guided',
  state: 'open',
  source: 'readiness',
  source_ref: 'sku-1',
  ...over,
})

function cardClasses(over: Partial<Task> = {}) {
  return mount(TaskCard, { global: { plugins: [i18n] }, props: { task: task(over) } }).find('.task-card').classes()
}

describe('card treatment follows state', () => {
  it.each([
    ['open', [], ['quiet-card', 'wait-card', 'is-done']],
    ['in_progress', ['quiet-card'], ['wait-card', 'is-done']],
    ['blocked', ['wait-card'], ['quiet-card', 'is-done']],
    ['done', ['wait-card', 'is-done'], ['quiet-card']],
  ] as Array<[State, string[], string[]]>)('%s', (state, present, absent) => {
    const classes = cardClasses({ state })
    for (const c of present) expect(classes).toContain(c)
    for (const c of absent) expect(classes).not.toContain(c)
  })

  // The same state looks the same whoever owns it — owner only colours the
  // leading dot.
  it.each(['merchant', 'sarvex', 'external'] as const)(
    'renders a blocked task the same way for owner %s',
    (owner) => {
      expect(cardClasses({ owner, state: 'blocked' })).toContain('wait-card')
    },
  )

  it('renders an open task as a solid card whoever owns it', () => {
    for (const owner of ['merchant', 'sarvex', 'external'] as const) {
      const classes = cardClasses({ owner, state: 'open' })
      expect(classes).not.toContain('wait-card')
      expect(classes).not.toContain('quiet-card')
    }
  })
})

describe('status chip follows state', () => {
  const chipText = (over: Partial<Task> = {}) =>
    mount(TaskCard, { global: { plugins: [i18n] }, props: { task: task(over) } })
      .find('.task-side')
      .text()

  it('says Waiting when blocked', () => {
    expect(chipText({ state: 'blocked' })).toContain('Waiting')
  })

  // Previously blank, which made work already underway look like work
  // waiting on the merchant.
  it('says Working when in progress', () => {
    expect(chipText({ state: 'in_progress' })).toContain('Working')
  })

  it('offers 1-click only for an open one-click task', () => {
    expect(chipText({ state: 'open', resolution: 'one_click' })).toContain('1-click')
    expect(chipText({ state: 'in_progress', resolution: 'one_click' })).not.toContain('1-click')
  })

  it('says nothing for a finished task', () => {
    const text = chipText({ state: 'done' })
    expect(text).not.toContain('Waiting')
    expect(text).not.toContain('Working')
  })
})

describe('interaction follows state', () => {
  it.each(['open', 'in_progress', 'blocked'] as State[])('opens the drawer when %s', async (state) => {
    const w = mount(TaskCard, { global: { plugins: [i18n] }, props: { task: task({ state }) } })
    await w.find('.task-card').trigger('click')
    expect(w.emitted('open')).toHaveLength(1)
  })

  it('does not open a finished task', async () => {
    const w = mount(TaskCard, { global: { plugins: [i18n] }, props: { task: task({ state: 'done' }) } })
    await w.find('.task-card').trigger('click')
    expect(w.emitted('open')).toBeUndefined()
  })
})
