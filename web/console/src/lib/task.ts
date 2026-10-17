// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// The one definition of "open", and the one owner grouping the Path is
// built from. Mirrors domain.Task.IsOpen on the backend
// (internal/planes/control/domain/task.go).
//
// These lived in three places before — the tasks store, the feed component,
// and the backend — and no two agreed: the feed counted a blocked merchant
// task as needing you, the store's getter did not, and the backend's journey
// counts included tasks that were already done. Whatever the rule is, it has
// to be the same rule everywhere the merchant sees a number.

import type { components } from '@/api/schema'

type Task = components['schemas']['task']
type Owner = components['schemas']['task_owner']

/**
 * Whether a task is still outstanding work.
 *
 * Every non-done state counts: `in_progress` is work underway and `blocked`
 * is work waiting on someone else, and both are still ahead of the store.
 */
export function isOpen(task: Task): boolean {
  return task.state !== 'done'
}

/** The open tasks belonging to one owner, in the order the server sent them. */
export function openFor(tasks: Task[], owner: Owner): Task[] {
  return tasks.filter((t) => t.owner === owner && isOpen(t))
}

/** The finished tasks, whoever owned them. */
export function doneTasks(tasks: Task[]): Task[] {
  return tasks.filter((t) => !isOpen(t))
}
