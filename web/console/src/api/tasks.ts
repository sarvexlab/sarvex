// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { api } from './client'
import type { components } from './schema'

type Task = components['schemas']['task']
type Owner = components['schemas']['task_owner']
type State = components['schemas']['task_state']
type ListEnvelope<T> = { data: T[]; page: { cursor: string | null; has_more: boolean } }

export type TaskFilter = { owner?: Owner; state?: State; store_id?: string }
export type TaskPage = { tasks: Task[]; cursor: string | null; hasMore: boolean }

export async function listTasks(filter: TaskFilter & { cursor?: string } = {}): Promise<TaskPage> {
  const { data } = await api.get<ListEnvelope<Task>>('/tasks', { params: filter })
  return { tasks: data.data, cursor: data.page.cursor, hasMore: data.page.has_more }
}

export async function getTask(taskId: string): Promise<Task> {
  const { data } = await api.get<Task>(`/tasks/${encodeURIComponent(taskId)}`)
  return data
}

export async function resolveTask(taskId: string, note?: string): Promise<Task> {
  const { data } = await api.post<Task>(
    `/tasks/${encodeURIComponent(taskId)}/resolve`,
    note ? { note } : undefined,
  )
  return data
}
