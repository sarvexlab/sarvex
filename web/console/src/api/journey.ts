// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { api, getAuthToken, wsBaseURL } from './client'
import type { components } from './schema'

type Journey = components['schemas']['journey']
type StreamMessage = components['schemas']['journey_stream_message']

export async function getJourney(storeId: string): Promise<Journey> {
  const { data } = await api.get<Journey>(`/stores/${encodeURIComponent(storeId)}/journey`)
  return data
}

const RECONNECT_DELAY_MS = 3_000

export type JourneyStreamHandlers = {
  /** A journey.updated frame arrived — re-fetch via getJourney. */
  onJourneyUpdate: () => void
  /** A task.updated frame arrived for taskId — re-fetch just that task. */
  onTaskUpdate: (taskId: string) => void
  /** An activation.updated frame arrived — re-fetch via getActivation. */
  onActivationUpdate: () => void
  /** A catalog.updated frame arrived — re-fetch readiness and products. */
  onCatalogUpdate: () => void
  /** A health.updated frame arrived — re-fetch health and findings. */
  onHealthUpdate: () => void
  /** An orders.updated frame arrived — re-fetch the order list. */
  onOrdersUpdate: () => void
}

/**
 * Opens the live journey stream for a store (GET .../journey/stream,
 * upgraded to a WebSocket). Every push is a thin "something changed" signal
 * — see sarvex-v1-control-plane.yaml's journey_stream_message — so handlers
 * are invoked with no more than an id and are expected to re-fetch the full
 * resource (getJourney, getActivation, the catalog, or GET /tasks/{id}). A
 * frame with a `type` we don't recognize, or no `task_id` on a task.updated
 * frame, falls back to re-fetching every store-scoped resource — the safe
 * default under "thin event, fat pull", and what keeps an added frame type
 * from silently leaving the console stale on older builds.
 *
 * This is a freshness optimization, not a dependency: if the token is
 * missing, or the socket can't connect, or drops and won't reconnect, the
 * console simply falls back to fetching on load/navigation as it always has.
 *
 * Returns a cleanup function that closes the connection and stops
 * reconnecting; call it on unmount or when the watched store changes.
 */
export function openJourneyStream(storeId: string, handlers: JourneyStreamHandlers): () => void {
  const token = getAuthToken()
  if (!token) return () => {}

  let closed = false
  let socket: WebSocket | null = null
  let reconnectHandle: ReturnType<typeof setTimeout> | null = null

  // The fallback for a frame we can't read or don't know: re-read every
  // store-scoped resource the stream feeds. Tasks are excluded — they are
  // fetched by id, and there is no id to fetch on an unreadable frame.
  function refetchAll() {
    handlers.onJourneyUpdate()
    handlers.onActivationUpdate()
    handlers.onCatalogUpdate()
    handlers.onHealthUpdate()
    handlers.onOrdersUpdate()
  }

  function connect() {
    if (closed) return
    socket = new WebSocket(
      `${wsBaseURL()}/stores/${encodeURIComponent(storeId)}/journey/stream`,
      ['bearer', token as string],
    )
    socket.onmessage = (event) => {
      let msg: StreamMessage
      try {
        msg = JSON.parse(event.data)
      } catch {
        refetchAll()
        return
      }
      if (msg.type === 'task.updated' && msg.task_id) {
        handlers.onTaskUpdate(msg.task_id)
      } else if (msg.type === 'activation.updated') {
        handlers.onActivationUpdate()
      } else if (msg.type === 'catalog.updated') {
        handlers.onCatalogUpdate()
      } else if (msg.type === 'health.updated') {
        handlers.onHealthUpdate()
      } else if (msg.type === 'orders.updated') {
        handlers.onOrdersUpdate()
      } else if (msg.type === 'journey.updated') {
        handlers.onJourneyUpdate()
      } else {
        refetchAll()
      }
    }
    socket.onclose = () => {
      if (closed) return
      reconnectHandle = setTimeout(connect, RECONNECT_DELAY_MS)
    }
    // onerror is followed by a close event; no separate handling needed.
  }
  connect()

  return () => {
    closed = true
    if (reconnectHandle !== null) clearTimeout(reconnectHandle)
    socket?.close()
  }
}
