// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest'
import { isOpen, openFor, doneTasks } from './task'
import type { components } from '@/api/schema'

type Task = components['schemas']['task']

const t = (over: Partial<Task>): Task => ({
  task_id: 'x',
  store_id: 's1',
  title: 'A task',
  owner: 'merchant',
  resolution: 'guided',
  state: 'open',
  source: 'readiness',
  ...over,
})

describe('isOpen', () => {
  // Every non-done state is outstanding. in_progress is work underway and
  // blocked is work waiting on someone else — a merchant looking at either
  // still has it ahead of them.
  it.each(['open', 'in_progress', 'blocked'] as const)('counts %s as open', (state) => {
    expect(isOpen(t({ state }))).toBe(true)
  })

  it('counts done as closed', () => {
    expect(isOpen(t({ state: 'done' }))).toBe(false)
  })
})

describe('openFor', () => {
  it('keeps only that owner and only open tasks', () => {
    const tasks = [
      t({ task_id: 'a', owner: 'merchant', state: 'open' }),
      t({ task_id: 'b', owner: 'merchant', state: 'done' }),
      t({ task_id: 'c', owner: 'sarvex', state: 'in_progress' }),
    ]
    expect(openFor(tasks, 'merchant').map((x) => x.task_id)).toEqual(['a'])
    expect(openFor(tasks, 'sarvex').map((x) => x.task_id)).toEqual(['c'])
  })

  // A blocked merchant task is still the merchant's to deal with. The store
  // getter used to drop it while the feed kept it, so the Path's "Needs you
  // now" count disagreed with every other count of the same tasks.
  it('keeps a blocked merchant task in their section', () => {
    const tasks = [t({ task_id: 'a', owner: 'merchant', state: 'blocked' })]
    expect(openFor(tasks, 'merchant').map((x) => x.task_id)).toEqual(['a'])
  })

  it('preserves the order the server sent', () => {
    const tasks = [
      t({ task_id: 'first', owner: 'merchant' }),
      t({ task_id: 'second', owner: 'merchant' }),
    ]
    expect(openFor(tasks, 'merchant').map((x) => x.task_id)).toEqual(['first', 'second'])
  })
})

describe('doneTasks', () => {
  it('collects finished tasks regardless of owner', () => {
    const tasks = [
      t({ task_id: 'a', owner: 'merchant', state: 'done' }),
      t({ task_id: 'b', owner: 'external', state: 'done' }),
      t({ task_id: 'c', owner: 'sarvex', state: 'in_progress' }),
    ]
    expect(doneTasks(tasks).map((x) => x.task_id)).toEqual(['a', 'b'])
  })
})
