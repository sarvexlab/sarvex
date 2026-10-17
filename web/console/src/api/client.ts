// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Typed Axios instance for the control plane.
// All request/response shapes come from src/api/schema.d.ts, which is generated
// from sarvex-v1-control-plane.yaml by `npm run gen:api`. Do not hand-write
// request/response types — keep them in sync via regeneration.
//
// The Bearer token is read from the auth store at send time, so token rotation
// and sign-out flow through naturally without needing to rebind interceptors.

import axios, { type AxiosError, type InternalAxiosRequestConfig } from 'axios'
import type { components } from './schema'

export type Principal = components['schemas']['principal']
export type Account = components['schemas']['account']
export type Store = components['schemas']['store']
export type Task = components['schemas']['task']
export type Connector = components['schemas']['connector']
export type JourneyState = components['schemas']['journey_state']
export type ErrorEnvelope = components['schemas']['error_envelope']

type TokenReader = () => string | null
type UnauthorizedHandler = () => void
type OperateAsReader = () => string | null

let getToken: TokenReader = () => null
let onUnauthorized: UnauthorizedHandler = () => {}
let getOperateAs: OperateAsReader = () => null

export function configureClient(opts: {
  getToken: TokenReader
  onUnauthorized: UnauthorizedHandler
  getOperateAs?: OperateAsReader
}) {
  getToken = opts.getToken
  onUnauthorized = opts.onUnauthorized
  if (opts.getOperateAs) getOperateAs = opts.getOperateAs
}

const baseURL =
  (import.meta.env.VITE_API_BASE as string | undefined)?.replace(/\/+$/, '') ||
  '/api'

/** The current bearer token, for callers that can't go through the axios
 * interceptor above — notably the journey WebSocket stream, which must send
 * the token as a Sec-WebSocket-Protocol entry (browsers can't set an
 * Authorization header on a WebSocket handshake). */
export function getAuthToken(): string | null {
  return getToken()
}

/** baseURL resolved to an absolute ws(s):// origin, for opening WebSocket
 * connections alongside the axios client's http(s) base. */
export function wsBaseURL(): string {
  const absolute = new URL(baseURL, window.location.origin)
  absolute.protocol = absolute.protocol === 'https:' ? 'wss:' : 'ws:'
  return absolute.toString().replace(/\/+$/, '')
}

export const api = axios.create({
  baseURL,
  headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
  timeout: 15_000,
})

api.interceptors.request.use((config: InternalAxiosRequestConfig) => {
  // A caller-supplied Authorization wins over the session token — e.g. the
  // simulator's operator-scoped merchant lookup on the login page, which must
  // not be replaced by a stale signed-in owner token.
  const t = getToken()
  if (t && !config.headers?.Authorization) {
    config.headers = config.headers ?? {}
    ;(config.headers as Record<string, string>).Authorization = `Bearer ${t}`
  }
  // Operators acting on behalf of a merchant attach X-Operate-As. The OpenAPI's
  // operator section uses this for audit-logging the impersonation context.
  const operateAs = getOperateAs()
  if (operateAs) {
    config.headers = config.headers ?? {}
    ;(config.headers as Record<string, string>)['X-Operate-As'] = operateAs
  }
  return config
})

api.interceptors.response.use(
  (r) => r,
  (err: AxiosError<ErrorEnvelope>) => {
    if (err.response?.status === 401) onUnauthorized()
    return Promise.reject(err)
  },
)
