// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// The task spine — everything actionable on every surface goes through here.
// `load` fetches the first page (filtering happens client-side for the
// Path's owner-grouped sections; the API filter is reserved for surfaces
// that only need a slice). The task list can exceed the server's page size,
// so `loadMore` appends subsequent pages using the cursor the server hands
// back rather than trying to fetch everything in one shot.

import { defineStore } from 'pinia'
import { listTasks, resolveTask as apiResolve, getTask, type TaskFilter } from '@/api/tasks'
import { doneTasks, isOpen, openFor } from '@/lib/task'
import type { components } from '@/api/schema'

type Task = components['schemas']['task']
type Owner = components['schemas']['task_owner']

export const useTasksStore = defineStore('tasks', {
  state: () => ({
    tasks: [] as Task[],
    cursor: null as string | null,
    hasMore: false,
    filter: {} as TaskFilter,
    loading: false,
    loadingMore: false,
    loaded: false,
    error: null as string | null,
  }),

  // All grouping goes through @/lib/task so the store, the feed, and the
  // backend's counts answer "how much is left" the same way.
  getters: {
    byOwner: (s) => (owner: Owner) => s.tasks.filter((t) => t.owner === owner),
    openYou: (s) => openFor(s.tasks, 'merchant'),
    openUs: (s) => openFor(s.tasks, 'sarvex'),
    waitingExternal: (s) => openFor(s.tasks, 'external'),
    done: (s) => doneTasks(s.tasks),
    totalOpen: (s) => s.tasks.filter(isOpen).length,
  },

  actions: {
    async load(filter: TaskFilter = {}) {
      this.loading = true
      this.error = null
      this.filter = filter
      try {
        const page = await listTasks(filter)
        this.tasks = page.tasks
        this.cursor = page.cursor
        this.hasMore = page.hasMore
        this.loaded = true
      } catch (e: unknown) {
        this.error = e instanceof Error ? e.message : 'Failed to load tasks'
      } finally {
        this.loading = false
      }
    },

    async loadMore() {
      if (!this.hasMore || this.loadingMore || !this.cursor) return
      this.loadingMore = true
      this.error = null
      try {
        const page = await listTasks({ ...this.filter, cursor: this.cursor })
        this.tasks = [...this.tasks, ...page.tasks]
        this.cursor = page.cursor
        this.hasMore = page.hasMore
      } catch (e: unknown) {
        this.error = e instanceof Error ? e.message : 'Failed to load more tasks'
      } finally {
        this.loadingMore = false
      }
    },

    async resolve(taskId: string) {
      const updated = await apiResolve(taskId)
      const i = this.tasks.findIndex((t) => t.task_id === taskId)
      if (i >= 0) this.tasks.splice(i, 1, updated)
      else this.tasks.push(updated)
    },

    // Applies a live task.updated push (see stores/journey.ts connectStream)
    // by re-fetching just that task and splicing it into place — the same
    // pattern resolve() uses, so TaskFeed/TaskCard/TaskDrawer pick up the
    // change via normal store reactivity with no full-list reload. Called
    // fire-and-forget from the websocket handler, so failures are swallowed:
    // a missed push is a freshness optimization lost, not a lost mutation —
    // the next full load() still picks it up.
    async refreshTask(taskId: string) {
      try {
        const updated = await getTask(taskId)
        const i = this.tasks.findIndex((t) => t.task_id === taskId)
        if (i >= 0) this.tasks.splice(i, 1, updated)
        else this.tasks.push(updated)
      } catch {
        // Best-effort; see comment above.
      }
    },

    clear() {
      this.tasks = []
      this.cursor = null
      this.hasMore = false
      this.loaded = false
    },
  },
})
