// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Mock mode is the console's dev experience (`make console`), so its tasks
// have to be tasks the backend could actually produce. A fixture whose state
// disagrees with its owner and resolution would render a card that cannot
// occur in production — which is exactly the inconsistency this rule was
// introduced to remove.
//
// Mirrors domain.DeriveTaskState in
// internal/planes/control/domain/task.go. If that rule changes, this fails.

import { describe, it, expect } from 'vitest'
import { fixtures } from './fixtures'
import type { components } from './schema'

type Task = components['schemas']['task']

function expectedState(task: Task): NonNullable<Task['state']> {
  if (task.resolution === 'external_wait') return 'blocked'
  if (task.owner === 'sarvex' && task.resolution === 'automatic') return 'in_progress'
  return 'open'
}

describe('mock task fixtures', () => {
  it('is not empty', () => {
    expect(fixtures.tasks.length).toBeGreaterThan(0)
  })

  it.each(fixtures.tasks.map((t) => [t.task_id ?? '(unnamed)', t] as const))(
    '%s has a state its owner and resolution could produce',
    (_id, task) => {
      // done is reached through Resolve/Complete, never at raise time, so any
      // owner/resolution pair may legitimately be done.
      if (task.state === 'done') return
      expect(task.state).toBe(expectedState(task))
    },
  )
})
