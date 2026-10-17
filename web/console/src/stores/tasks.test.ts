// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// useTasksStore.resolve replaces the task in-place so the Path's owner-
// grouped feed re-renders without a full refetch. These tests pin that
// optimistic update + the grouping getters, plus the load()/loadMore()
// pagination contract (the server returns a page with a cursor + has_more,
// and the store is responsible for stitching pages together).

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useTasksStore } from './tasks'
import type { components } from '@/api/schema'
import type { TaskPage } from '@/api/tasks'

type Task = components['schemas']['task']

vi.mock('@/api/tasks', () => ({
  listTasks: vi.fn(),
  resolveTask: vi.fn(),
  getTask: vi.fn(),
}))

import { listTasks, resolveTask, getTask } from '@/api/tasks'

const t = (over: Partial<Task>): Task => ({
  task_id: 't-1',
  title: 'Task',
  why_it_matters: '',
  owner: 'merchant',
  resolution: 'guided',
  state: 'open',
  blocks_checkout: false,
  source: 'checklist',
  source_ref: null,
  created_at: '2026-06-23T10:00:00Z',
  updated_at: '2026-06-23T10:00:00Z',
  ...over,
})

const page = (tasks: Task[], over: Partial<Omit<TaskPage, 'tasks'>> = {}): TaskPage => ({
  tasks,
  cursor: null,
  hasMore: false,
  ...over,
})

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(listTasks).mockReset()
  vi.mocked(resolveTask).mockReset()
  vi.mocked(getTask).mockReset()
})

describe('useTasksStore', () => {
  it('groups tasks by owner via getters', async () => {
    vi.mocked(listTasks).mockResolvedValue(page([
      t({ task_id: 'a', owner: 'merchant' }),
      t({ task_id: 'b', owner: 'sarvex' }),
      t({ task_id: 'c', owner: 'external',   state: 'blocked' }),
      t({ task_id: 'd', owner: 'merchant', state: 'done' }),
    ]))
    const tasks = useTasksStore()
    await tasks.load()
    expect(tasks.openYou.map((x) => x.task_id)).toEqual(['a'])
    expect(tasks.openUs.map((x) => x.task_id)).toEqual(['b'])
    expect(tasks.waitingExternal.map((x) => x.task_id)).toEqual(['c'])
    expect(tasks.done.map((x) => x.task_id)).toEqual(['d'])
    expect(tasks.totalOpen).toBe(3)
  })

  // The store's sections and TaskFeed's sections are the same sections, so
  // they have to be computed the same way — a blocked merchant task belongs
  // in "needs you now" in both, and in the open total.
  it('counts a blocked merchant task as open, matching the feed', async () => {
    vi.mocked(listTasks).mockResolvedValue(page([
      t({ task_id: 'a', owner: 'merchant', state: 'blocked' }),
      t({ task_id: 'b', owner: 'sarvex', state: 'in_progress' }),
    ]))
    const tasks = useTasksStore()
    await tasks.load()
    expect(tasks.openYou.map((x) => x.task_id)).toEqual(['a'])
    expect(tasks.openUs.map((x) => x.task_id)).toEqual(['b'])
    expect(tasks.totalOpen).toBe(2)
  })

  it('resolve() updates the task in-place so groupings recompute', async () => {
    vi.mocked(listTasks).mockResolvedValue(page([
      t({ task_id: 'a', owner: 'merchant', state: 'open' }),
      t({ task_id: 'b', owner: 'merchant', state: 'open' }),
    ]))
    const tasks = useTasksStore()
    await tasks.load()
    expect(tasks.openYou).toHaveLength(2)

    vi.mocked(resolveTask).mockResolvedValue(t({ task_id: 'a', owner: 'merchant', state: 'done' }))
    await tasks.resolve('a')

    expect(tasks.openYou).toHaveLength(1)
    expect(tasks.done.map((x) => x.task_id)).toEqual(['a'])
    expect(tasks.totalOpen).toBe(1)
  })

  it('load() forwards store_id to the API so tasks are scoped per store', async () => {
    vi.mocked(listTasks).mockResolvedValue(page([]))
    const tasks = useTasksStore()
    await tasks.load({ store_id: 'store-2' })
    expect(listTasks).toHaveBeenCalledWith({ store_id: 'store-2' })
  })

  it('captures load() errors as a recoverable error string', async () => {
    vi.mocked(listTasks).mockRejectedValue(new Error('network down'))
    const tasks = useTasksStore()
    await tasks.load()
    expect(tasks.error).toBe('network down')
    expect(tasks.tasks).toEqual([])
    expect(tasks.loaded).toBe(false)
  })

  it('load() records hasMore/cursor from the page envelope', async () => {
    vi.mocked(listTasks).mockResolvedValue(
      page([t({ task_id: 'a' })], { cursor: 'a', hasMore: true }),
    )
    const tasks = useTasksStore()
    await tasks.load()
    expect(tasks.hasMore).toBe(true)
    expect(tasks.cursor).toBe('a')
  })

  it('loadMore() appends the next page using the stored cursor and filter', async () => {
    vi.mocked(listTasks).mockResolvedValueOnce(
      page([t({ task_id: 'a' })], { cursor: 'a', hasMore: true }),
    )
    const tasks = useTasksStore()
    await tasks.load({ store_id: 'store-1' })

    vi.mocked(listTasks).mockResolvedValueOnce(
      page([t({ task_id: 'b' })], { cursor: 'b', hasMore: false }),
    )
    await tasks.loadMore()

    expect(listTasks).toHaveBeenLastCalledWith({ store_id: 'store-1', cursor: 'a' })
    expect(tasks.tasks.map((x) => x.task_id)).toEqual(['a', 'b'])
    expect(tasks.hasMore).toBe(false)
    expect(tasks.cursor).toBe('b')
  })

  it('loadMore() is a no-op when there is no next page', async () => {
    vi.mocked(listTasks).mockResolvedValue(page([t({ task_id: 'a' })]))
    const tasks = useTasksStore()
    await tasks.load()

    await tasks.loadMore()

    expect(listTasks).toHaveBeenCalledTimes(1)
    expect(tasks.tasks).toHaveLength(1)
  })

  // refreshTask backs the task.updated websocket push (see
  // stores/journey.ts connectStream) — it applies the same in-place splice
  // as resolve(), just sourced from a plain re-fetch instead of the resolve
  // mutation's response.
  describe('refreshTask', () => {
    it('updates an already-loaded task in place so groupings recompute', async () => {
      vi.mocked(listTasks).mockResolvedValue(page([
        t({ task_id: 'a', owner: 'merchant', state: 'open' }),
        t({ task_id: 'b', owner: 'merchant', state: 'open' }),
      ]))
      const tasks = useTasksStore()
      await tasks.load()
      expect(tasks.openYou).toHaveLength(2)

      vi.mocked(getTask).mockResolvedValue(t({ task_id: 'a', owner: 'merchant', state: 'done' }))
      await tasks.refreshTask('a')

      expect(getTask).toHaveBeenCalledWith('a')
      expect(tasks.openYou).toHaveLength(1)
      expect(tasks.done.map((x) => x.task_id)).toEqual(['a'])
    })

    it('appends a task not currently in the loaded list', async () => {
      vi.mocked(listTasks).mockResolvedValue(page([t({ task_id: 'a' })]))
      const tasks = useTasksStore()
      await tasks.load()

      vi.mocked(getTask).mockResolvedValue(t({ task_id: 'z', owner: 'sarvex' }))
      await tasks.refreshTask('z')

      expect(tasks.tasks.map((x) => x.task_id)).toEqual(['a', 'z'])
    })

    it('swallows a fetch failure rather than throwing', async () => {
      vi.mocked(listTasks).mockResolvedValue(page([t({ task_id: 'a' })]))
      const tasks = useTasksStore()
      await tasks.load()

      vi.mocked(getTask).mockRejectedValue(new Error('not found'))
      await expect(tasks.refreshTask('a')).resolves.toBeUndefined()
      expect(tasks.tasks).toHaveLength(1)
    })
  })
})
