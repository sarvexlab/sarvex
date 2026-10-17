// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useJourneyStore } from './journey'
import { useStoresStore } from './stores'
import { useTasksStore } from './tasks'
import { useHealthStore } from './health'
import { useOrdersStore } from './orders'
import type { components } from '@/api/schema'

type Journey = components['schemas']['journey']

vi.mock('@/api/journey', () => ({
  getJourney: vi.fn(),
  openJourneyStream: vi.fn(),
}))

import { getJourney, openJourneyStream } from '@/api/journey'

const journeyFixture: Journey = {
  state: 'connected',
  history: [],
  next_action: null,
  open_task_counts: { merchant: 0, sarvex: 0, external: 0 },
  entered_state_at: '2026-07-11T10:00:00Z',
}

beforeEach(() => {
  setActivePinia(createPinia())
  useStoresStore().currentStoreId = 'store-1'
  vi.mocked(getJourney).mockReset().mockResolvedValue(journeyFixture)
  vi.mocked(openJourneyStream).mockReset()
})

describe('useJourneyStore connectStream/disconnectStream', () => {
  it('opens the stream for the current store', () => {
    const journey = useJourneyStore()
    journey.connectStream()
    expect(openJourneyStream).toHaveBeenCalledTimes(1)
    expect(vi.mocked(openJourneyStream).mock.calls[0][0]).toBe('store-1')
  })

  it('does nothing when there is no current store', () => {
    useStoresStore().currentStoreId = null
    const journey = useJourneyStore()
    journey.connectStream()
    expect(openJourneyStream).not.toHaveBeenCalled()
  })

  it('a journey.updated push reloads the journey via onJourneyUpdate', async () => {
    const journey = useJourneyStore()
    journey.connectStream()

    const handlers = vi.mocked(openJourneyStream).mock.calls[0][1]
    handlers.onJourneyUpdate()
    await Promise.resolve()
    await Promise.resolve()

    expect(getJourney).toHaveBeenCalledWith('store-1')
    expect(journey.journey).toEqual(journeyFixture)
  })

  it('a task.updated push forwards to the tasks store via onTaskUpdate', () => {
    const journey = useJourneyStore()
    journey.connectStream()

    const tasks = useTasksStore()
    const refreshTask = vi.spyOn(tasks, 'refreshTask').mockResolvedValue()

    const handlers = vi.mocked(openJourneyStream).mock.calls[0][1]
    handlers.onTaskUpdate('task-1')

    expect(refreshTask).toHaveBeenCalledWith('task-1')
  })

  it('a health.updated push reloads health via onHealthUpdate — the WebSocket path that lands a new order/task without a page refresh', () => {
    const journey = useJourneyStore()
    journey.connectStream()

    const health = useHealthStore()
    const load = vi.spyOn(health, 'load').mockResolvedValue()

    const handlers = vi.mocked(openJourneyStream).mock.calls[0][1]
    handlers.onHealthUpdate()

    expect(load).toHaveBeenCalledTimes(1)
  })

  it('an orders.updated push reloads orders via onOrdersUpdate — lands a new order on "Agent orders this week" without a refresh', () => {
    const journey = useJourneyStore()
    journey.connectStream()

    const orders = useOrdersStore()
    const load = vi.spyOn(orders, 'load').mockResolvedValue()

    const handlers = vi.mocked(openJourneyStream).mock.calls[0][1]
    handlers.onOrdersUpdate()

    expect(load).toHaveBeenCalledTimes(1)
  })

  it('connectStream tears down a prior connection before opening a new one', () => {
    const close = vi.fn()
    vi.mocked(openJourneyStream).mockReturnValue(close)

    const journey = useJourneyStore()
    journey.connectStream()
    journey.connectStream()

    expect(close).toHaveBeenCalledTimes(1)
    expect(openJourneyStream).toHaveBeenCalledTimes(2)
  })

  it('disconnectStream closes the connection and is safe to call twice', () => {
    const close = vi.fn()
    vi.mocked(openJourneyStream).mockReturnValue(close)

    const journey = useJourneyStore()
    journey.connectStream()
    journey.disconnectStream()
    journey.disconnectStream()

    expect(close).toHaveBeenCalledTimes(1)
  })
})
