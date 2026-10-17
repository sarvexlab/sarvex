// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// openJourneyStream is a thin freshness optimization over polling — these
// tests focus on the parts that can silently break that contract: no token
// means no connection (never a hard failure), a frame dispatches to the
// right handler based on its `type` (falling back to onJourneyUpdate for
// anything unrecognized, per "thin event, fat pull"), and the returned
// cleanup function actually stops reconnection rather than just closing the
// current socket.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'

vi.mock('./client', () => ({
  getAuthToken: vi.fn(),
  wsBaseURL: vi.fn(() => 'ws://api.test'),
  api: {},
}))

import { getAuthToken } from './client'
import { openJourneyStream, type JourneyStreamHandlers } from './journey'

class FakeWebSocket {
  static instances: FakeWebSocket[] = []
  url: string
  protocols: string[]
  onmessage: ((event: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  closed = false

  constructor(url: string, protocols: string[]) {
    this.url = url
    this.protocols = protocols
    FakeWebSocket.instances.push(this)
  }

  close() {
    this.closed = true
  }

  emitMessage(data: string) {
    this.onmessage?.({ data })
  }

  emitClose() {
    this.onclose?.()
  }
}

function fakeHandlers(): JourneyStreamHandlers {
  return {
    onJourneyUpdate: vi.fn(),
    onTaskUpdate: vi.fn(),
    onActivationUpdate: vi.fn(),
    onCatalogUpdate: vi.fn(),
    onHealthUpdate: vi.fn(),
    onOrdersUpdate: vi.fn(),
  }
}

beforeEach(() => {
  FakeWebSocket.instances = []
  vi.stubGlobal('WebSocket', FakeWebSocket)
  vi.mocked(getAuthToken).mockReturnValue('tok-abc')
  vi.useFakeTimers()
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('openJourneyStream', () => {
  it('does not connect when there is no auth token', () => {
    vi.mocked(getAuthToken).mockReturnValue(null)
    const close = openJourneyStream('store-1', fakeHandlers())
    expect(FakeWebSocket.instances).toHaveLength(0)
    close() // must not throw even though nothing was opened
  })

  it('connects with the bearer subprotocol and the store-scoped path', () => {
    openJourneyStream('store-1', fakeHandlers())
    expect(FakeWebSocket.instances).toHaveLength(1)
    const ws = FakeWebSocket.instances[0]
    expect(ws.url).toBe('ws://api.test/stores/store-1/journey/stream')
    expect(ws.protocols).toEqual(['bearer', 'tok-abc'])
  })

  it('dispatches a journey.updated frame to onJourneyUpdate', () => {
    const handlers = fakeHandlers()
    openJourneyStream('store-1', handlers)
    const ws = FakeWebSocket.instances[0]

    ws.emitMessage(JSON.stringify({ type: 'journey.updated', store_id: 'store-1', state: 'connected' }))

    expect(handlers.onJourneyUpdate).toHaveBeenCalledTimes(1)
    expect(handlers.onTaskUpdate).not.toHaveBeenCalled()
  })

  it('dispatches a task.updated frame to onTaskUpdate with the task id', () => {
    const handlers = fakeHandlers()
    openJourneyStream('store-1', handlers)
    const ws = FakeWebSocket.instances[0]

    ws.emitMessage(
      JSON.stringify({ type: 'task.updated', store_id: 'store-1', task_id: 'task-1', state: 'done' }),
    )

    expect(handlers.onTaskUpdate).toHaveBeenCalledTimes(1)
    expect(handlers.onTaskUpdate).toHaveBeenCalledWith('task-1')
    expect(handlers.onJourneyUpdate).not.toHaveBeenCalled()
  })

  it('falls back to onJourneyUpdate for a task.updated frame missing task_id', () => {
    const handlers = fakeHandlers()
    openJourneyStream('store-1', handlers)
    const ws = FakeWebSocket.instances[0]

    ws.emitMessage(JSON.stringify({ type: 'task.updated', store_id: 'store-1' }))

    expect(handlers.onJourneyUpdate).toHaveBeenCalledTimes(1)
    expect(handlers.onTaskUpdate).not.toHaveBeenCalled()
  })

  it('dispatches an activation.updated frame to onActivationUpdate', () => {
    const handlers = fakeHandlers()
    openJourneyStream('store-1', handlers)
    const ws = FakeWebSocket.instances[0]

    ws.emitMessage(
      JSON.stringify({ type: 'activation.updated', store_id: 'store-1', state: 'verified' }),
    )

    expect(handlers.onActivationUpdate).toHaveBeenCalledTimes(1)
    expect(handlers.onJourneyUpdate).not.toHaveBeenCalled()
    expect(handlers.onTaskUpdate).not.toHaveBeenCalled()
  })

  it('dispatches a health.updated frame to onHealthUpdate', () => {
    const handlers = fakeHandlers()
    openJourneyStream('store-1', handlers)
    const ws = FakeWebSocket.instances[0]

    ws.emitMessage(JSON.stringify({ type: 'health.updated', store_id: 'store-1' }))

    expect(handlers.onHealthUpdate).toHaveBeenCalledTimes(1)
    expect(handlers.onJourneyUpdate).not.toHaveBeenCalled()
    expect(handlers.onCatalogUpdate).not.toHaveBeenCalled()
  })

  it('dispatches an orders.updated frame to onOrdersUpdate', () => {
    const handlers = fakeHandlers()
    openJourneyStream('store-1', handlers)
    const ws = FakeWebSocket.instances[0]

    ws.emitMessage(JSON.stringify({ type: 'orders.updated', store_id: 'store-1' }))

    expect(handlers.onOrdersUpdate).toHaveBeenCalledTimes(1)
    expect(handlers.onJourneyUpdate).not.toHaveBeenCalled()
    expect(handlers.onHealthUpdate).not.toHaveBeenCalled()
  })

  it('dispatches a catalog.updated frame to onCatalogUpdate', () => {
    const handlers = fakeHandlers()
    openJourneyStream('store-1', handlers)
    const ws = FakeWebSocket.instances[0]

    ws.emitMessage(JSON.stringify({ type: 'catalog.updated', store_id: 'store-1' }))

    expect(handlers.onCatalogUpdate).toHaveBeenCalledTimes(1)
    expect(handlers.onJourneyUpdate).not.toHaveBeenCalled()
    expect(handlers.onTaskUpdate).not.toHaveBeenCalled()
  })

  it('falls back to refetching every store-scoped resource for an unrecognized type', () => {
    const handlers = fakeHandlers()
    openJourneyStream('store-1', handlers)
    const ws = FakeWebSocket.instances[0]

    ws.emitMessage(JSON.stringify({ type: 'something.else', store_id: 'store-1' }))

    expect(handlers.onJourneyUpdate).toHaveBeenCalledTimes(1)
    expect(handlers.onActivationUpdate).toHaveBeenCalledTimes(1)
    expect(handlers.onCatalogUpdate).toHaveBeenCalledTimes(1)
    expect(handlers.onHealthUpdate).toHaveBeenCalledTimes(1)
    expect(handlers.onOrdersUpdate).toHaveBeenCalledTimes(1)
  })

  it('falls back to refetching every store-scoped resource for an unparsable frame', () => {
    const handlers = fakeHandlers()
    openJourneyStream('store-1', handlers)
    const ws = FakeWebSocket.instances[0]

    ws.emitMessage('not json')

    expect(handlers.onJourneyUpdate).toHaveBeenCalledTimes(1)
    expect(handlers.onActivationUpdate).toHaveBeenCalledTimes(1)
    expect(handlers.onCatalogUpdate).toHaveBeenCalledTimes(1)
    expect(handlers.onHealthUpdate).toHaveBeenCalledTimes(1)
    expect(handlers.onOrdersUpdate).toHaveBeenCalledTimes(1)
  })

  it('reconnects after a drop, unless the cleanup function already ran', async () => {
    openJourneyStream('store-1', fakeHandlers())
    const first = FakeWebSocket.instances[0]

    first.emitClose()
    await vi.advanceTimersByTimeAsync(3_000)

    expect(FakeWebSocket.instances).toHaveLength(2)
  })

  it('cleanup stops reconnection and closes the live socket', async () => {
    const close = openJourneyStream('store-1', fakeHandlers())
    const first = FakeWebSocket.instances[0]

    close()
    expect(first.closed).toBe(true)

    // A close event arriving after cleanup (e.g. in flight when close() was
    // called) must not schedule a reconnect.
    first.emitClose()
    await vi.advanceTimersByTimeAsync(10_000)

    expect(FakeWebSocket.instances).toHaveLength(1)
  })
})
