// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Dev-only mock layer with dual-mode support.
//
// The Axios adapter routes each request to either a mock handler or the real
// backend based on env config. Two configurations:
//
//   1. Mock-by-default (default): every route hits a mock unless its path
//      prefix is listed in VITE_REAL_ROUTES.
//      Example: VITE_REAL_ROUTES=/auth/me,/journey
//        → /auth/me and /journey hit the real backend; everything else mock.
//
//   2. Real-by-default: set VITE_USE_MOCKS=0. Every route hits the real
//      backend unless its path prefix is listed in VITE_MOCK_ROUTES.
//      Example: VITE_USE_MOCKS=0  VITE_MOCK_ROUTES=/operator,/tasks
//        → /operator/* and /tasks/* mock; everything else real.
//
// Set VITE_API_DEBUG=1 to log the resolved mode + path per request.

import axios, { type AxiosAdapter, type AxiosRequestConfig, type AxiosResponse } from 'axios'
import { api, type Principal } from './client'
import type { components } from './schema'
import { fixtures, schedulePairingHealth } from './fixtures'
import { surfaceMock } from './surfaceMock'
import { canTransition, monthRange } from '@/lib/ledger'
import type { StorePlatform } from '@/platforms'

type Method = 'GET' | 'POST' | 'PATCH' | 'PUT' | 'DELETE'
type Handler = (cfg: AxiosRequestConfig, match: RegExpExecArray) => unknown | Promise<unknown>
type Platform = components['schemas']['platform']
type Connector = components['schemas']['connector']
type LedgerEntry = components['schemas']['ledger_entry']
type LedgerEntryStatus = components['schemas']['ledger_entry_status']

const routes: Array<{ method: Method; pattern: RegExp; handler: Handler }> = []
const on = (method: Method, pattern: RegExp, handler: Handler) =>
  routes.push({ method, pattern, handler })

function decodeSimToken(token: string | undefined): {
  email: string
  role: 'owner' | 'staff' | 'operator'
  merchant_id: string
  store_ids: string[]
  sub: string
} | null {
  if (!token) return null
  const parts = token.split('.')
  if (parts.length !== 3) return null
  try {
    return JSON.parse(atob(parts[1].replace(/-/g, '+').replace(/_/g, '/')))
  } catch {
    return null
  }
}

function requireAuth(cfg: AxiosRequestConfig) {
  const auth = (cfg.headers?.Authorization as string | undefined) ?? ''
  const claims = decodeSimToken(auth.replace(/^Bearer\s+/i, ''))
  if (!claims) {
    const err = new Error('unauthorized') as Error & { status?: number }
    err.status = 401
    throw err
  }
  // X-Operate-As: an operator is acting on behalf of a merchant. For the
  // merchant-scoped endpoints this swaps the effective tenant so the same
  // fixtures answer the request. The seed dataset has a single full
  // merchant (mer-acme-tea); impersonating other merchants on the board
  // still maps onto that fixture for V1.
  const operateAs = (cfg.headers?.['X-Operate-As'] as string | undefined) ?? ''
  if (operateAs && claims.role === 'operator') {
    return { ...claims, merchant_id: operateAs, store_ids: ['str-acme-au'] }
  }
  return claims
}

// ── Auth ──────────────────────────────────────────────────────────────────
on('GET', /^\/auth\/me$/, (cfg) => {
  const claims = requireAuth(cfg)
  return {
    user_id: claims.sub,
    email: claims.email,
    role: claims.role,
    merchant_id: claims.merchant_id,
    store_ids: claims.store_ids,
  } satisfies Principal
})

// ── Stores ────────────────────────────────────────────────────────────────
on('GET', /^\/stores$/, (cfg) => {
  requireAuth(cfg)
  return { data: fixtures.stores, page: { cursor: null, has_more: false } }
})

// ── Journey ───────────────────────────────────────────────────────────────
on('GET', /^\/journey$/, (cfg) => {
  requireAuth(cfg)
  return fixtures.journey
})

on('GET', /^\/stores\/([^/]+)\/journey$/, (cfg) => {
  requireAuth(cfg)
  return fixtures.journey
})

// ── Store UCP activation ──────────────────────────────────────────────────
on('GET', /^\/stores\/([^/]+)\/activation$/, (cfg) => {
  requireAuth(cfg)
  return fixtures.activation
})

on('POST', /^\/stores\/([^/]+)\/activation\/verify$/, (cfg) => {
  requireAuth(cfg)
  return {
    passed: true,
    ucp_state: fixtures.activation.ucp_state,
    ran_at: new Date().toISOString(),
    checks: [
      { check_id: 'profile_reachable', passed: true },
      { check_id: 'signature_roundtrip', passed: true },
      { check_id: 'negotiation_handshake', passed: true },
      { check_id: 'catalog_lookup', passed: true },
    ],
  }
})

// ── Agent surfaces ────────────────────────────────────────────────────────
// Generic: the registry, a store's enrollment per surface, and the enrollment
// actions. Behaviour per surface lives in surfaceMock.
on('GET', /^\/surfaces$/, (cfg) => {
  requireAuth(cfg)
  return surfaceMock.registry()
})
on('GET', /^\/stores\/([^/]+)\/surfaces$/, (cfg, m) => {
  requireAuth(cfg)
  return surfaceMock.list(decodeURIComponent(m[1]))
})
const surfaceAction =
  (act: (store: string, surface: string, cfg: AxiosRequestConfig) => unknown): Handler =>
  (cfg, m) => {
    requireAuth(cfg)
    const out = act(decodeURIComponent(m[1]), decodeURIComponent(m[2]), cfg)
    if (!out) {
      const err = new Error('not found') as Error & { status?: number }
      err.status = 404
      throw err
    }
    return out
  }
on('POST', /^\/stores\/([^/]+)\/surfaces\/([^/]+)\/enroll$/, surfaceAction((st, sf) => surfaceMock.enroll(st, sf)))
on('POST', /^\/stores\/([^/]+)\/surfaces\/([^/]+)\/prerequisites\/evaluate$/, surfaceAction((st, sf) => surfaceMock.evaluate(st, sf)))
on('POST', /^\/stores\/([^/]+)\/surfaces\/([^/]+)\/submit$/, surfaceAction((st, sf) => surfaceMock.submit(st, sf)))
on('PUT', /^\/stores\/([^/]+)\/surfaces\/([^/]+)\/config$/, surfaceAction((st, sf, cfg) => {
  const body = typeof cfg.data === 'string' ? JSON.parse(cfg.data) : (cfg.data ?? {})
  return surfaceMock.setConfig(st, sf, (body as { config?: Record<string, string> }).config ?? {})
}))
on('DELETE', /^\/stores\/([^/]+)\/surfaces\/([^/]+)$/, (cfg, m) => {
  requireAuth(cfg)
  surfaceMock.withdraw(decodeURIComponent(m[1]), decodeURIComponent(m[2]))
  return null
})

// ── Tasks ─────────────────────────────────────────────────────────────────
on('GET', /^\/tasks(\?|$)/, (cfg) => {
  requireAuth(cfg)
  const params = (cfg.params ?? {}) as Record<string, string | undefined>
  let list = fixtures.tasks
  if (params.owner) list = list.filter((t) => t.owner === params.owner)
  if (params.state) list = list.filter((t) => t.state === params.state)
  return { data: list, page: { cursor: null, has_more: false } }
})

on('POST', /^\/tasks\/([^/]+)\/resolve$/, (cfg, m) => {
  requireAuth(cfg)
  const id = decodeURIComponent(m[1])
  const t = fixtures.tasks.find((x) => x.task_id === id)
  if (!t) {
    const err = new Error('not found') as Error & { status?: number }
    err.status = 404
    throw err
  }
  t.state = 'done'
  t.updated_at = new Date().toISOString()
  return t
})

on('GET', /^\/tasks\/([^/]+)$/, (cfg, m) => {
  requireAuth(cfg)
  const id = decodeURIComponent(m[1])
  const t = fixtures.tasks.find((x) => x.task_id === id)
  if (!t) {
    const err = new Error('not found') as Error & { status?: number }
    err.status = 404
    throw err
  }
  return t
})

// ── Connectors ────────────────────────────────────────────────────────────
on('GET', /^\/connectors$/, (cfg) => {
  requireAuth(cfg)
  return { data: fixtures.connectors, page: { cursor: null, has_more: false } }
})

on('POST', /^\/stores\/([^/]+)\/connectors\/pairing$/, (cfg, m) => {
  requireAuth(cfg)
  const body = (typeof cfg.data === 'string' ? JSON.parse(cfg.data) : cfg.data) as {
    platform: Platform
  }
  // Pair onto the store named in the path — a merchant may have several.
  const store = fixtures.stores.find((s) => s.store_id === decodeURIComponent(m[1]))
  if (!store) {
    const err = new Error('not found') as Error & { status?: number }
    err.status = 404
    throw err
  }
  const connector: Connector & { pairing_code?: string } = {
    connector_id: 'con-' + Math.random().toString(36).slice(2, 8),
    store_id: store.store_id ?? '',
    platform: body.platform,
    status: 'pairing',
    sync_model: 'realtime',
    last_signal_at: null,
    last_full_sync_at: null,
    freshness_warning: false,
    plugin_version: null,
  }
  ;(connector as unknown as { pairing_code: string }).pairing_code =
    'cnd-' + Math.random().toString(36).slice(2, 6).toUpperCase() +
    '-' + Math.random().toString(36).slice(2, 6).toUpperCase() +
    '-' + Math.random().toString(36).slice(2, 6).toUpperCase()
  fixtures.connectors.push(connector)
  // The connector is associated with its store the moment it's created
  // (in `pairing` status). The status transition to `healthy` runs after
  // a delay to exercise the polling UI; the store ↔ connector linkage
  // doesn't wait on that.
  const linkedStore = fixtures.stores.find((s) => s.store_id === connector.store_id)
  if (linkedStore) linkedStore.connector_id = connector.connector_id ?? null
  schedulePairingHealth(connector.connector_id!, 1800)
  return connector
})

on('POST', /^\/connectors\/([^/]+)\/resync$/, (cfg) => {
  requireAuth(cfg)
  return { job_id: 'job-' + Math.random().toString(36).slice(2, 8), status: 'queued' }
})

on('GET', /^\/connectors\/([^/]+)$/, (cfg, m) => {
  requireAuth(cfg)
  const id = decodeURIComponent(m[1])
  const c = fixtures.connectors.find((x) => x.connector_id === id)
  if (!c) {
    const err = new Error('not found') as Error & { status?: number }
    err.status = 404
    throw err
  }
  return c
})

// ── Presence ──────────────────────────────────────────────────────────────
on('GET', /^\/presence$/, (cfg) => {
  requireAuth(cfg)
  return fixtures.presence
})

on('GET', /^\/presence\/manifest$/, (cfg) => {
  requireAuth(cfg)
  return fixtures.manifest
})

// ── Catalog ───────────────────────────────────────────────────────────────
on('GET', /^\/catalog\/readiness$/, (cfg) => {
  requireAuth(cfg)
  return fixtures.readiness
})

on('GET', /^\/catalog\/products(\?|$)/, (cfg) => {
  requireAuth(cfg)
  const params = (cfg.params ?? {}) as Record<string, unknown>
  let list = fixtures.products
  if (typeof params.min_score === 'number') {
    list = list.filter((p) => (p.score ?? 0) >= (params.min_score as number))
  }
  if (typeof params.has_blocking_issue === 'boolean') {
    list = list.filter((p) => p.has_blocking_issue === params.has_blocking_issue)
  }
  return { data: list, page: { cursor: null, has_more: false } }
})

on('GET', /^\/catalog\/products\/([^/]+)$/, (cfg, m) => {
  requireAuth(cfg)
  const sku = decodeURIComponent(m[1])
  const detail = fixtures.productDetails[sku]
  if (detail) return detail
  // Synthesise a minimal detail for SKUs without a hand-authored fixture
  // so the route is navigable for every row in the table.
  const summary = fixtures.products.find((p) => p.sku === sku)
  if (!summary) {
    const err = new Error('not found') as Error & { status?: number }
    err.status = 404
    throw err
  }
  return {
    ...summary,
    dimensions: { completeness: 80, consistency: 80, attribute_quality: 80, delivery_data: 80 },
    issues: [],
    attribute_mappings: [],
  }
})

// ── Orders ────────────────────────────────────────────────────────────────
on('GET', /^\/orders(\?|$)/, (cfg) => {
  requireAuth(cfg)
  const params = (cfg.params ?? {}) as Record<string, string | undefined>
  let list = fixtures.orders
  if (params.status) list = list.filter((o) => o.status === params.status)
  return { data: list, page: { cursor: null, has_more: false } }
})

// ── Account ───────────────────────────────────────────────────────────────
on('GET', /^\/account$/, (cfg) => {
  requireAuth(cfg)
  return fixtures.account
})

on('PATCH', /^\/account$/, (cfg) => {
  requireAuth(cfg)
  const body = (typeof cfg.data === 'string' ? JSON.parse(cfg.data) : cfg.data) as Record<string, unknown>
  Object.assign(fixtures.account, body)
  return fixtures.account
})

on('GET', /^\/account\/members$/, (cfg) => {
  requireAuth(cfg)
  return { data: fixtures.members, page: { cursor: null, has_more: false } }
})

// ── Billing ───────────────────────────────────────────────────────────────
on('GET', /^\/billing$/, (cfg) => {
  requireAuth(cfg)
  return fixtures.billing
})

on('POST', /^\/billing\/checkout-link$/, (cfg) => {
  requireAuth(cfg)
  return { url: 'https://checkout.stripe.com/c/pay/cs_test_mock' }
})

on('POST', /^\/billing\/portal-link$/, (cfg) => {
  requireAuth(cfg)
  return { url: 'https://billing.stripe.com/p/session/mock' }
})

// ── Notifications ─────────────────────────────────────────────────────────
on('GET', /^\/notifications\/preferences$/, (cfg) => {
  requireAuth(cfg)
  return fixtures.notificationPrefs
})

on('PUT', /^\/notifications\/preferences$/, (cfg) => {
  requireAuth(cfg)
  const body = (typeof cfg.data === 'string' ? JSON.parse(cfg.data) : cfg.data) as Record<string, unknown>
  Object.assign(fixtures.notificationPrefs, body)
  return fixtures.notificationPrefs
})

// ── Payment handlers ──────────────────────────────────────────────────────
on('GET', /^\/stores\/([^/]+)\/payment-config$/, (cfg) => {
  requireAuth(cfg)
  return fixtures.paymentConfig
})

on('PUT', /^\/stores\/([^/]+)\/payment-config$/, (cfg) => {
  requireAuth(cfg)
  const body = (typeof cfg.data === 'string' ? JSON.parse(cfg.data) : cfg.data) as {
    handlers?: Array<{ handler_id: string; enabled: boolean }>
  }
  if (body.handlers && fixtures.paymentConfig.handlers) {
    for (const patch of body.handlers) {
      const target = fixtures.paymentConfig.handlers.find((h) => h.handler_id === patch.handler_id)
      if (target) target.enabled = patch.enabled
    }
  }
  return fixtures.paymentConfig
})

on('GET', /^\/stores\/([^/]+)\/profile$/, (cfg) => {
  requireAuth(cfg)
  return fixtures.storeProfile
})

on('PUT', /^\/stores\/([^/]+)\/profile$/, (cfg) => {
  requireAuth(cfg)
  const body = (typeof cfg.data === 'string' ? JSON.parse(cfg.data) : cfg.data) as Partial<
    typeof fixtures.storeProfile
  >
  Object.assign(fixtures.storeProfile, body)
  return fixtures.storeProfile
})

// ── Health ────────────────────────────────────────────────────────────────
// Pre-existing note: these patterns previously lacked the /stores/{id}/
// prefix that api/health.ts's real client actually sends, so they never
// matched — fixed here while adding the endpoint-health routes.
on('GET', /^\/stores\/([^/]+)\/health$/, (cfg) => {
  requireAuth(cfg)
  return fixtures.health
})

on('GET', /^\/stores\/([^/]+)\/health\/consistency(\?|$)/, (cfg) => {
  requireAuth(cfg)
  return { data: fixtures.consistency, page: { cursor: null, has_more: false } }
})

on('GET', /^\/stores\/([^/]+)\/health\/history(\?|$)/, (cfg) => {
  requireAuth(cfg)
  return { data: fixtures.healthHistory, page: { cursor: null, has_more: false } }
})

on('GET', /^\/alerts(\?|$)/, (cfg) => {
  requireAuth(cfg)
  return { data: fixtures.alerts, page: { cursor: null, has_more: false } }
})

// ── Checkout Escalation Console ─────────────────────────────────────────
const TERMINAL_STATES = new Set(['completed', 'failed', 'abandoned'])

function filterCheckoutSessions(
  params: Record<string, string | boolean | undefined>,
) {
  return fixtures.checkoutSessions.filter((s) => {
    if (params.state && s.state !== params.state) return false
    if (params.surface && s.surface !== params.surface) return false
    if (params.store_id && s.store_id !== params.store_id) return false
    if (params.cause && s.diagnosis?.cause !== params.cause) return false
    if (
      (params.escalated_only === true || params.escalated_only === 'true') &&
      s.escalation == null
    ) {
      return false
    }
    if (params.from && s.last_activity_at && s.last_activity_at < String(params.from)) return false
    if (params.to && s.last_activity_at && s.last_activity_at > String(params.to)) return false
    return true
  })
}

on('GET', /^\/checkout-sessions\/live(\?|$)/, (cfg) => {
  requireAuth(cfg)
  const windowMinutes = Number((cfg.params as Record<string, unknown>)?.window_minutes ?? 30)
  const cutoff = Date.now() - windowMinutes * 60_000
  const live = fixtures.checkoutSessions.filter(
    (s) => !TERMINAL_STATES.has(s.state ?? '') && new Date(s.last_activity_at ?? 0).getTime() >= cutoff,
  )
  return { data: live, page: { cursor: null, has_more: false } }
})

on('GET', /^\/checkout-sessions(\?|$)/, (cfg) => {
  requireAuth(cfg)
  const filtered = filterCheckoutSessions((cfg.params ?? {}) as Record<string, string | boolean | undefined>)
  return { data: filtered, page: { cursor: null, has_more: false } }
})

on('GET', /^\/checkout-sessions\/([^/]+)\/timeline(\?|$)/, (cfg, m) => {
  requireAuth(cfg)
  const id = decodeURIComponent(m[1])
  const all = fixtures.checkoutSessionTimelines[id] ?? []
  const params = (cfg.params ?? {}) as Record<string, string | number | undefined>
  const afterSeq = Number(params.cursor ?? 0)
  const limit = Number(params.limit ?? 100)
  const page = all.filter((e) => (e.seq ?? 0) > afterSeq).slice(0, limit)
  const hasMore = all.some((e) => (e.seq ?? 0) > (page[page.length - 1]?.seq ?? afterSeq))
  return {
    data: page,
    page: { cursor: hasMore ? String(page[page.length - 1]?.seq ?? afterSeq) : null, has_more: hasMore },
  }
})

on('GET', /^\/checkout-sessions\/([^/]+)$/, (cfg, m) => {
  requireAuth(cfg)
  const id = decodeURIComponent(m[1])
  const session = fixtures.checkoutSessions.find((s) => s.checkout_id === id)
  if (!session) {
    const err = new Error('not found') as Error & { status?: number }
    err.status = 404
    throw err
  }
  return session
})

on('PATCH', /^\/alerts\/([^/]+)$/, (cfg, m) => {
  requireAuth(cfg)
  const id = decodeURIComponent(m[1])
  const body = (typeof cfg.data === 'string' ? JSON.parse(cfg.data) : cfg.data) as {
    status: 'acknowledged' | 'resolved'
  }
  const alert = fixtures.alerts.find((a) => a.alert_id === id) ?? fixtures.health.active_alerts?.find((a) => a.alert_id === id)
  if (!alert) {
    const err = new Error('not found') as Error & { status?: number }
    err.status = 404
    throw err
  }
  const now = new Date().toISOString()
  alert.status = body.status
  if (body.status === 'acknowledged') alert.acknowledged_at = now
  if (body.status === 'resolved') alert.resolved_at = now
  return alert
})

// ── Lost Revenue Ledger ───────────────────────────────────────────────────
// Real filtering/sorting/pagination against fixtures.ledgerEntries so the
// mock behaves like the real read API (R9/R12/R14) rather than always
// returning everything. requireAnalyticsTier isn't re-implemented here —
// fixtures.billing.plan defaults to 'concierge' (unlocked); flip it to
// 'self_serve' to exercise the locked state in mock mode.

function filterLedgerEntries(params: Record<string, string | undefined>): LedgerEntry[] {
  return fixtures.ledgerEntries.filter((e) => {
    if (params.cause && e.cause !== params.cause) return false
    if (params.status && e.status !== params.status) return false
    if (params.store_id && e.store_id !== params.store_id) return false
    if (params.surface && e.surface !== params.surface) return false
    if (params.from && e.occurred_at && e.occurred_at < params.from) return false
    if (params.to && e.occurred_at && e.occurred_at > params.to) return false
    return true
  })
}

function sortLedgerEntries(entries: LedgerEntry[], sort: string | undefined): LedgerEntry[] {
  if (sort === 'occurred_at') {
    return [...entries].sort((a, b) => (b.occurred_at ?? '').localeCompare(a.occurred_at ?? ''))
  }
  // Default: lost_revenue_minor descending, occurred_at descending tiebreak.
  return [...entries].sort((a, b) => {
    const rev = (b.lost_revenue_minor ?? 0) - (a.lost_revenue_minor ?? 0)
    return rev !== 0 ? rev : (b.occurred_at ?? '').localeCompare(a.occurred_at ?? '')
  })
}

on('GET', /^\/ledger\/entries(\?|$)/, (cfg) => {
  requireAuth(cfg)
  const params = (cfg.params ?? {}) as Record<string, string | undefined>
  const sorted = sortLedgerEntries(filterLedgerEntries(params), params.sort)
  const limit = Number(params.limit ?? 20)
  const start = Number(params.cursor ?? 0)
  const page = sorted.slice(start, start + limit)
  const hasMore = start + limit < sorted.length
  return { data: page, page: { cursor: hasMore ? String(start + limit) : null, has_more: hasMore } }
})

function buildLedgerSummary(month: string, currency: string) {
  const { from, to } = monthRange(month)
  const inMonth = filterLedgerEntries({ from, to }).filter((e) => e.currency === currency)

  const byCause = new Map<string, { count: number; lost_revenue_minor: number }>()
  let totalRevenue = 0
  let unknownCount = 0
  for (const e of inMonth) {
    const cause = e.cause ?? 'unclassified'
    const row = byCause.get(cause) ?? { count: 0, lost_revenue_minor: 0 }
    row.count += 1
    row.lost_revenue_minor += e.lost_revenue_minor ?? 0
    byCause.set(cause, row)
    totalRevenue += e.lost_revenue_minor ?? 0
    if (e.revenue_unknown) unknownCount += 1
  }
  const causes = [...byCause.entries()]
    .map(([cause, v]) => ({ cause: cause as LedgerEntry['cause'], ...v }))
    .sort((a, b) => b.lost_revenue_minor - a.lost_revenue_minor)

  const prevMonth = new Date(Date.UTC(Number(month.slice(0, 4)), Number(month.slice(5, 7)) - 2, 1))
    .toISOString()
    .slice(0, 7)
  const prevRange = monthRange(prevMonth)
  const priorTotal = filterLedgerEntries({ from: prevRange.from, to: prevRange.to })
    .filter((e) => e.currency === currency)
    .reduce((acc, e) => acc + (e.lost_revenue_minor ?? 0), 0)

  return {
    month,
    currency,
    causes,
    total_count: inMonth.length,
    total_lost_revenue_minor: totalRevenue,
    entries_with_unknown_revenue: unknownCount,
    prior_month_delta_minor: totalRevenue - priorTotal,
  }
}

on('GET', /^\/ledger\/summary(\?|$)/, (cfg) => {
  requireAuth(cfg)
  const params = (cfg.params ?? {}) as Record<string, string | undefined>
  const month = params.month ?? new Date().toISOString().slice(0, 7)

  // currency given → one-element list scoped to it; omitted → one element
  // per currency the ledger actually has entries for that month, read from
  // the entries themselves (never derived from store configuration).
  if (params.currency) {
    return { data: [buildLedgerSummary(month, params.currency)] }
  }
  const { from, to } = monthRange(month)
  const currencies = [...new Set(filterLedgerEntries({ from, to }).map((e) => e.currency).filter((c): c is string => !!c))].sort()
  return { data: currencies.map((currency) => buildLedgerSummary(month, currency)) }
})

// ── Agent Behavior Insights ────────────────────────────────────────────
// Static fixtures (fixtures.capabilityGaps/productDemand/negotiation) —
// month/from/to/surface/limit are accepted (so the console's controls have
// something to round-trip) but don't reshape the fixture data, matching the
// "not modeling the backend's fold logic" note on those fixtures.

on('GET', /^\/insights\/capability_gaps(\?|$)/, (cfg) => {
  requireAuth(cfg)
  return { data: fixtures.capabilityGaps, page: { cursor: null, has_more: false } }
})

on('GET', /^\/insights\/product_demand(\?|$)/, (cfg) => {
  requireAuth(cfg)
  const params = (cfg.params ?? {}) as Record<string, string | undefined>
  let rows = fixtures.productDemand
  if (params.surface) {
    const surface = params.surface
    rows = rows
      .filter((r) => surface in (r.surface_breakdown ?? {}))
      .map((r) => {
        const count = (r.surface_breakdown ?? {})[surface] ?? 0
        return { ...r, request_count: count, surface_breakdown: { [surface]: count } }
      })
  }
  const limit = params.limit ? Number(params.limit) : rows.length
  return { data: rows.slice(0, limit), page: { cursor: null, has_more: false } }
})

on('GET', /^\/insights\/negotiation(\?|$)/, (cfg) => {
  requireAuth(cfg)
  return fixtures.negotiation
})

on('PATCH', /^\/ledger\/entries\/([^/]+)$/, (cfg, m) => {
  requireAuth(cfg)
  const id = decodeURIComponent(m[1])
  const body = (typeof cfg.data === 'string' ? JSON.parse(cfg.data) : cfg.data) as {
    status: LedgerEntryStatus
  }
  const entry = fixtures.ledgerEntries.find((e) => e.entry_id === id)
  if (!entry) {
    const err = new Error('not found') as Error & { status?: number }
    err.status = 404
    throw err
  }
  if (!entry.status || !canTransition(entry.status, body.status)) {
    const err = new Error('invalid status transition') as Error & { status?: number }
    err.status = 409
    throw err
  }
  entry.status = body.status
  entry.status_changed_at = new Date().toISOString()
  return entry
})

// ── Channel Dashboard ────────────────────────────────────────────────────
// Aggregates fixtures.channelDaily/channelWebBaseline the same way
// internal/planes/control/api/channel.go's handlers aggregate their Mongo
// rollups — sums of raw counts, rates/currency-agreement/confidence derived
// at read/request time, never stored. Mirrors channel.go's sumAggregates/
// aggregateCurrency/sessionsConfirmed/rollupBaseline post the "Close
// Channel Dashboard API-contract gaps" fix on main. requireAnalyticsTier
// isn't re-implemented here either (see the Ledger section's note above —
// same fixtures.billing.plan gate).

type ChannelDailyFixtureRow = (typeof fixtures.channelDaily)[number]
type ChannelBaselineFixtureRow = (typeof fixtures.channelWebBaseline)[number]

function resolveChannelRangeParams(params: Record<string, string | undefined>): { from: string; to: string } {
  const to = params.to ?? new Date().toISOString().slice(0, 10)
  const from = params.from ?? new Date(new Date(`${to}T00:00:00Z`).getTime() - 29 * 86_400_000).toISOString().slice(0, 10)
  return { from, to }
}

function filterChannelDaily(
  params: { store_id?: string; surface?: string },
  from: string,
  to: string,
): ChannelDailyFixtureRow[] {
  return fixtures.channelDaily.filter((r) => {
    if (r.date < from || r.date > to) return false
    if (params.store_id && r.store_id !== params.store_id) return false
    if (params.surface && r.surface !== params.surface) return false
    return true
  })
}

function safeRate(n: number, d: number): number {
  return d === 0 ? 0 : n / d
}

// Mirrors channel.go's aggregateCurrency: "" (mixed=false) when nothing
// revenue-bearing is in range, "" (mixed=true) when the summed rows
// genuinely disagree, otherwise the one currency they agree on.
function aggregateCurrency(rows: Array<{ currency?: string }>): { currency: string; mixed: boolean } {
  const seen = new Set(rows.map((r) => r.currency).filter((c): c is string => !!c))
  if (seen.size === 0) return { currency: '', mixed: false }
  if (seen.size > 1) return { currency: '', mixed: true }
  return { currency: [...seen][0], mixed: false }
}

function sumChannelRows(rows: ChannelDailyFixtureRow[]) {
  const sum = {
    sessions: 0,
    sessions_inferred: 0,
    sessions_reached_cart: 0,
    sessions_reached_checkout: 0,
    checkouts_completed: 0,
    revenue_minor: 0,
    discovery_requests: 0,
  }
  for (const r of rows) {
    sum.sessions += r.sessions
    sum.sessions_inferred += r.sessions_inferred
    sum.sessions_reached_cart += r.sessions_reached_cart
    sum.sessions_reached_checkout += r.sessions_reached_checkout
    sum.checkouts_completed += r.checkouts_completed
    sum.revenue_minor += r.revenue_minor
    sum.discovery_requests += r.discovery_requests
  }
  const { currency, mixed } = aggregateCurrency(rows)
  return { ...sum, currency, currency_mixed: mixed }
}

// sessions minus sessions_inferred, zero-safe — mirrors channel.go's
// sessionsConfirmed.
function sessionsConfirmed(sum: { sessions: number; sessions_inferred: number }): number {
  return Math.max(0, sum.sessions - sum.sessions_inferred)
}

// checkouts_initiated always equals sessions by construction (every
// fixture session here is checkout-anchored, same invariant
// channel/model.go documents) — derived here, never stored on the fixture.
function channelRatesFor(sum: ReturnType<typeof sumChannelRows>) {
  return {
    cart_rate: safeRate(sum.sessions_reached_cart, sum.sessions),
    checkout_rate: safeRate(sum.sessions_reached_checkout, sum.sessions_reached_cart),
    completion_rate: safeRate(sum.checkouts_completed, sum.sessions),
    aov_minor: sum.checkouts_completed > 0 ? Math.round(sum.revenue_minor / sum.checkouts_completed) : 0,
  }
}

function monthBounds(month: string): { from: string; to: string } {
  const [y, m] = month.split('-').map(Number)
  const from = `${month}-01`
  const to = new Date(Date.UTC(y, m, 0)).toISOString().slice(0, 10)
  return { from, to }
}

on('GET', /^\/channel\/summary(\?|$)/, (cfg) => {
  requireAuth(cfg)
  const params = (cfg.params ?? {}) as Record<string, string | undefined>
  const now = new Date()
  const month = params.month ?? `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}`
  const { from, to } = monthBounds(month)
  const priorAnchor = new Date(Date.UTC(Number(month.slice(0, 4)), Number(month.slice(5, 7)) - 2, 1))
  const priorMonth = `${priorAnchor.getUTCFullYear()}-${String(priorAnchor.getUTCMonth() + 1).padStart(2, '0')}`
  const prior = monthBounds(priorMonth)

  const current = filterChannelDaily({ store_id: params.store_id }, from, to)
  const priorRows = filterChannelDaily({ store_id: params.store_id }, prior.from, prior.to)

  function buildRow(surface: string, curRows: ChannelDailyFixtureRow[], priRows: ChannelDailyFixtureRow[]) {
    const cur = sumChannelRows(curRows)
    const pri = sumChannelRows(priRows)
    const curRates = channelRatesFor(cur)
    const priRates = channelRatesFor(pri)
    return {
      surface,
      month,
      sessions: cur.sessions,
      sessions_confirmed: sessionsConfirmed(cur),
      sessions_inferred: cur.sessions_inferred,
      checkouts_completed: cur.checkouts_completed,
      revenue_minor: cur.revenue_minor,
      currency: cur.currency || null,
      currency_mixed: cur.currency_mixed,
      ...curRates,
      prior_period: {
        sessions_delta: cur.sessions - pri.sessions,
        checkouts_completed_delta: cur.checkouts_completed - pri.checkouts_completed,
        revenue_minor_delta: cur.revenue_minor - pri.revenue_minor,
        aov_minor_delta: curRates.aov_minor - priRates.aov_minor,
        completion_rate_delta: curRates.completion_rate - priRates.completion_rate,
      },
    }
  }

  const surfaces = [...new Set(current.map((r) => r.surface))]
  const data = [
    buildRow('all', current, priorRows),
    ...surfaces.map((s) =>
      buildRow(s, current.filter((r) => r.surface === s), priorRows.filter((r) => r.surface === s)),
    ),
  ]
  return { data, page: { cursor: null, has_more: false } }
})

on('GET', /^\/channel\/funnel(\?|$)/, (cfg) => {
  requireAuth(cfg)
  const params = (cfg.params ?? {}) as Record<string, string | undefined>
  const { from, to } = resolveChannelRangeParams(params)
  const rows = filterChannelDaily(params, from, to)
  const sum = sumChannelRows(rows)
  return {
    from,
    to,
    surface: params.surface || 'all',
    sessions: sum.sessions,
    sessions_confirmed: sessionsConfirmed(sum),
    sessions_inferred: sum.sessions_inferred,
    sessions_reached_cart: sum.sessions_reached_cart,
    sessions_reached_checkout: sum.sessions_reached_checkout,
    checkouts_initiated: sum.sessions,
    checkouts_completed: sum.checkouts_completed,
    currency: sum.currency || null,
    currency_mixed: sum.currency_mixed,
    rates: channelRatesFor(sum),
  }
})

on('GET', /^\/channel\/daily(\?|$)/, (cfg) => {
  requireAuth(cfg)
  const params = (cfg.params ?? {}) as Record<string, string | undefined>
  const { from, to } = resolveChannelRangeParams(params)
  const rows = filterChannelDaily(params, from, to)
  const data = rows.map((r) => {
    const rowSum = sumChannelRows([r])
    return {
      date: r.date,
      surface: r.surface,
      store_id: r.store_id,
      sessions: r.sessions,
      sessions_confirmed: sessionsConfirmed(r),
      sessions_inferred: r.sessions_inferred,
      sessions_reached_cart: r.sessions_reached_cart,
      sessions_reached_checkout: r.sessions_reached_checkout,
      checkouts_initiated: r.sessions,
      checkouts_completed: r.checkouts_completed,
      revenue_minor: r.revenue_minor,
      currency: r.currency, // a single row is never ambiguous — no mixed flag
      discovery_requests: r.discovery_requests,
      rates: channelRatesFor(rowSum),
    }
  })
  return { data, page: { cursor: null, has_more: false } }
})

const CHANNEL_BASELINE_NO_DATA = 'no baseline data synced for this period'

// Mirrors channel.go's rollupBaseline: usable = every day with any
// non-unavailable confidence (fixture only ever has estimated/
// connector_reported, so "usable" here is just "present" — see
// fixtures.ts's channelWebBaseline doc comment). reported = every day
// that's fully connector_reported. confidence is connector_reported only
// when every expected day is; partial when some real data exists but not
// all of it is fully reported; unavailable only when there's truly nothing
// usable in range — R11/R12: partial still returns real (not estimated)
// numbers computed from what's there, with a reason, never a substitute.
function rollupBaseline(rows: ChannelBaselineFixtureRow[], expectedDays: number) {
  const usable = rows.length
  const reported = rows.filter((r) => r.confidence === 'connector_reported').length
  let orders = 0
  let revenueMinor = 0
  for (const r of rows) {
    orders += r.orders
    revenueMinor += r.revenue_minor
  }
  const { currency, mixed } = aggregateCurrency(rows)

  if (usable === 0) {
    return { confidence: 'unavailable' as const, reason: CHANNEL_BASELINE_NO_DATA, orders: 0, revenueMinor: 0, currency: '', mixed: false }
  }
  if (reported === expectedDays) {
    return { confidence: 'connector_reported' as const, reason: '', orders, revenueMinor, currency, mixed }
  }
  const incomplete = expectedDays - reported
  const reason = `${incomplete} of ${expectedDays} days in this period have incomplete or missing web-baseline data — figures below are computed from what was reported, not estimated for the gap`
  return { confidence: 'partial' as const, reason, orders, revenueMinor, currency, mixed }
}

on('GET', /^\/channel\/comparison(\?|$)/, (cfg) => {
  requireAuth(cfg)
  const params = (cfg.params ?? {}) as Record<string, string | undefined>
  const { from, to } = resolveChannelRangeParams(params)

  const agenticRows = filterChannelDaily({ store_id: params.store_id }, from, to)
  const agentic = sumChannelRows(agenticRows)
  const agenticRates = channelRatesFor(agentic)
  const base = {
    from,
    to,
    agentic: {
      revenue_minor: agentic.revenue_minor,
      checkouts_completed: agentic.checkouts_completed,
      aov_minor: agenticRates.aov_minor,
      currency: agentic.currency || null,
      currency_mixed: agentic.currency_mixed,
    },
  }

  const baselineRows = fixtures.channelWebBaseline.filter(
    (r) => r.date >= from && r.date <= to && (!params.store_id || r.store_id === params.store_id),
  )
  const expectedDays =
    Math.round((new Date(`${to}T00:00:00Z`).getTime() - new Date(`${from}T00:00:00Z`).getTime()) / 86_400_000) + 1
  const baseline = rollupBaseline(baselineRows, expectedDays)
  const baselineAov = baseline.orders > 0 ? Math.round(baseline.revenueMinor / baseline.orders) : 0

  const webBaseline = {
    orders: baseline.orders,
    revenue_minor: baseline.revenueMinor,
    aov_minor: baselineAov,
    currency: baseline.currency || null,
    currency_mixed: baseline.mixed,
    confidence: baseline.confidence,
  }

  if (baseline.confidence === 'unavailable') {
    const unavailable = { available: false, confidence: baseline.confidence, reason: baseline.reason }
    return { ...base, web_baseline: webBaseline, revenue: unavailable, aov: unavailable, agentic_share_of_total: unavailable }
  }

  return {
    ...base,
    web_baseline: webBaseline,
    revenue: {
      available: true,
      confidence: baseline.confidence,
      reason: baseline.reason || undefined,
      agentic_minor: agentic.revenue_minor,
      baseline_minor: baseline.revenueMinor,
      delta_minor: agentic.revenue_minor - baseline.revenueMinor,
    },
    aov: {
      available: true,
      confidence: baseline.confidence,
      reason: baseline.reason || undefined,
      agentic_minor: agenticRates.aov_minor,
      baseline_minor: baselineAov,
      delta_minor: agenticRates.aov_minor - baselineAov,
    },
    agentic_share_of_total:
      baseline.revenueMinor > 0
        ? {
            available: true,
            confidence: baseline.confidence,
            reason: baseline.reason || undefined,
            share: agentic.revenue_minor / baseline.revenueMinor,
          }
        : { available: false, confidence: 'unavailable' as const, reason: CHANNEL_BASELINE_NO_DATA },
  }
})

// ── Validation ────────────────────────────────────────────────────────────
on('GET', /^\/validation\/status$/, (cfg) => {
  requireAuth(cfg)
  return fixtures.validationStatus
})

on('GET', /^\/validation\/runs(\?|$)/, (cfg) => {
  requireAuth(cfg)
  return { data: fixtures.validationRuns, page: { cursor: null, has_more: false } }
})

on('POST', /^\/validation\/runs$/, (cfg) => {
  requireAuth(cfg)
  const newRun = {
    run_id: 'run-' + Math.random().toString(36).slice(2, 6),
    ran_at: new Date().toISOString(),
    spec_version: fixtures.validationStatus.target_spec_version ?? 'ucp-2026-05',
    result: 'fail' as const,
    passed: 142,
    failed: 2,
    warnings: 1,
  }
  fixtures.validationRuns.unshift(newRun)
  if (fixtures.validationStatus) {
    fixtures.validationStatus.last_run_at = newRun.ran_at
  }
  return { job_id: 'job-' + Math.random().toString(36).slice(2, 8), status: 'queued' }
})

// ── Operator ──────────────────────────────────────────────────────────────
function requireOperator(cfg: AxiosRequestConfig) {
  const claims = requireAuth(cfg)
  if (claims.role !== 'operator') {
    const err = new Error('operator role required') as Error & { status?: number }
    err.status = 403
    throw err
  }
  return claims
}

on('GET', /^\/operator\/board(\?|$)/, (cfg) => {
  requireOperator(cfg)
  const params = (cfg.params ?? {}) as Record<string, string | boolean | undefined>
  let list = fixtures.board
  if (params.state) list = list.filter((c) => c.state === params.state)
  if (params.attention === true || params.attention === 'true') {
    list = list.filter((c) => c.blocked || c.stale)
  }
  return { data: list, page: { cursor: null, has_more: false } }
})

on('GET', /^\/operator\/merchants$/, (cfg) => {
  requireOperator(cfg)
  return { data: fixtures.merchants, page: { cursor: null, has_more: false } }
})

on('POST', /^\/operator\/merchants$/, (cfg) => {
  requireOperator(cfg)
  const body = (typeof cfg.data === 'string' ? JSON.parse(cfg.data) : cfg.data) as {
    name: string
    owner_email?: string
    business_facts?: Record<string, unknown>
  }
  const merchant_id = 'mer-' + Math.random().toString(36).slice(2, 8)
  const account = {
    merchant_id,
    name: body.name,
    timezone: 'Australia/Sydney',
    business_facts: body.business_facts ?? {},
    created_at: new Date().toISOString(),
  }
  fixtures.merchants.unshift(account)
  // Brand-new merchants appear on the board immediately at "connected" with
  // zero tasks so the operator can add their first store.
  fixtures.board.unshift({
    merchant_id,
    name: body.name,
    state: 'connected',
    days_in_state: 0,
    open_task_counts: { merchant: 0, sarvex: 0, external: 0 },
    blocked: false,
    stale: false,
  })
  return account
})

on('POST', /^\/operator\/merchants\/([^/]+)\/members$/, (cfg, m) => {
  requireOperator(cfg)
  const merchantId = decodeURIComponent(m[1])
  const body = (typeof cfg.data === 'string' ? JSON.parse(cfg.data) : cfg.data) as {
    email: string
    role: 'owner' | 'staff'
  }
  // Audit-log the invite so the operator can see it in the popover.
  fixtures.audit.unshift({
    entry_id: 'a-' + Math.random().toString(36).slice(2, 8),
    operator_user_id: 'op-dana-001',
    target_merchant_id: merchantId,
    action: 'member.invite',
    detail: { email: body.email, role: body.role } as unknown as Record<string, never>,
    occurred_at: new Date().toISOString(),
  })
  return {
    user_id: 'usr-' + Math.random().toString(36).slice(2, 8),
    email: body.email,
    role: body.role,
    status: 'invited',
  }
})

on('GET', /^\/operator\/merchants\/([^/]+)\/stores$/, (cfg, m) => {
  requireOperator(cfg)
  const merchantId = decodeURIComponent(m[1])
  const stores = fixtures.stores.filter((s) => {
    const conn = fixtures.connectors.find((c) => c.connector_id === s.connector_id)
    return !conn || conn.store_id === s.store_id
  }).filter(() => merchantId === 'mer-acme-tea')
  return { data: stores, page: { cursor: null, has_more: false } }
})

on('POST', /^\/operator\/merchants\/([^/]+)\/stores$/, (cfg, m) => {
  requireOperator(cfg)
  const merchantId = decodeURIComponent(m[1])
  const body = (typeof cfg.data === 'string' ? JSON.parse(cfg.data) : cfg.data) as {
    platform: StorePlatform
    name: string
  }
  fixtures.audit.unshift({
    entry_id: 'a-' + Math.random().toString(36).slice(2, 8),
    operator_user_id: 'op-dana-001',
    target_merchant_id: merchantId,
    action: 'store.create',
    detail: { name: body.name, platform: body.platform } as unknown as Record<string, never>,
    occurred_at: new Date().toISOString(),
  })
  return {
    store_id: 'str-' + Math.random().toString(36).slice(2, 8),
    name: body.name,
    platform: body.platform,
    domain: '',
    connector_id: null,
    journey_state: 'connected',
    declared_capabilities: [],
  }
})

on('POST', /^\/operator\/manual-steps$/, (cfg) => {
  requireOperator(cfg)
  const body = (typeof cfg.data === 'string' ? JSON.parse(cfg.data) : cfg.data) as {
    description: string
    target_merchant_id?: string
    disposition?: 'unclassified' | 'automate' | 'templatise' | 'accept_manual'
  }
  const step = {
    manual_step_id: 'ms-' + Math.random().toString(36).slice(2, 8),
    target_merchant_id: body.target_merchant_id ?? '',
    description: body.description,
    disposition: body.disposition ?? 'unclassified',
    created_at: new Date().toISOString(),
  }
  fixtures.manualSteps.unshift(step)
  return step
})

on('GET', /^\/operator\/audit-log(\?|$)/, (cfg) => {
  requireOperator(cfg)
  const params = (cfg.params ?? {}) as Record<string, string | undefined>
  let list = fixtures.audit
  if (params.target_merchant_id) {
    list = list.filter((a) => a.target_merchant_id === params.target_merchant_id)
  }
  return { data: list, page: { cursor: null, has_more: false } }
})

// ── Adapter ───────────────────────────────────────────────────────────────
// In Axios 1.x, defaults.adapter is a string array ['xhr','http','fetch'], not
// a function. getAdapter resolves the environment-appropriate callable from it.
const passthrough = axios.getAdapter(axios.defaults.adapter!) as AxiosAdapter

function splitEnvList(value: string | undefined): string[] {
  return (value ?? '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
}

const REAL_ROUTES = splitEnvList(import.meta.env.VITE_REAL_ROUTES)
const MOCK_ROUTES = splitEnvList(import.meta.env.VITE_MOCK_ROUTES)
const MOCK_BY_DEFAULT = import.meta.env.VITE_USE_MOCKS !== '0'
const DEBUG = import.meta.env.VITE_API_DEBUG === '1'

/**
 * Resolve whether a given API path should be served by the mock layer or by
 * the real backend. Exported so a UI badge / debug surface can introspect
 * the same decision the adapter makes.
 */
export function apiMode(path: string): 'mock' | 'real' {
  if (MOCK_BY_DEFAULT) {
    return REAL_ROUTES.some((p) => path.startsWith(p)) ? 'real' : 'mock'
  }
  return MOCK_ROUTES.some((p) => path.startsWith(p)) ? 'mock' : 'real'
}

const mockAdapter: AxiosAdapter = async (cfg) => {
  const url = (cfg.url ?? '').replace(/^\/?api/, '') || '/'
  const path = url.split('?')[0]
  const method = (cfg.method ?? 'get').toUpperCase() as Method
  const mode = apiMode(path)

  if (DEBUG) {
    // eslint-disable-next-line no-console
    console.log(`[api ${mode}] ${method} ${path}`)
  }

  // Real-backend path: hand off to Axios's original adapter so the request
  // goes out over HTTP to VITE_API_BASE (or the default /api proxy).
  if (mode === 'real') {
    if (passthrough) return passthrough(cfg)
    return makeResponse(cfg, 500, {
      error: {
        code: 'no_real_adapter',
        message:
          'Route resolved to real backend but no passthrough adapter is available. ' +
          'This should not happen — check src/api/client.ts.',
      },
    })
  }

  let matched: { handler: Handler; m: RegExpExecArray } | null = null
  for (const r of routes) {
    if (r.method !== method) continue
    const m = r.pattern.exec(path)
    if (m) {
      matched = { handler: r.handler, m }
      break
    }
  }
  if (!matched) {
    // Unmocked route under mock-by-default — fall through to the real
    // backend rather than hard-404. This keeps "mock-by-default with a
    // gap" working when only some routes have fixtures.
    if (passthrough) return passthrough(cfg)
    return makeResponse(cfg, 404, {
      error: { code: 'not_found', message: `mock missing: ${method} ${path}` },
    })
  }
  try {
    const data = await matched.handler(cfg, matched.m)
    return makeResponse(cfg, 200, data)
  } catch (e: unknown) {
    const status = (e as { status?: number }).status ?? 500
    return makeResponse(cfg, status, {
      error: {
        code: status === 401 ? 'unauthorized' : status === 404 ? 'not_found' : 'internal',
        message: (e as Error).message,
      },
    })
  }
}

function makeResponse(cfg: AxiosRequestConfig, status: number, data: unknown): AxiosResponse {
  return {
    // Deep-clone the response so callers see plain JSON every time, the
    // way a real HTTP boundary would behave. Without this, handlers
    // returning fixtures by reference let direct fixture mutations (e.g.
    // the deferred status flip on a pairing connector) bypass Vue's
    // reactivity entirely, which silently desyncs Pinia state.
    data: status === 200 && data !== undefined ? structuredClone(data) : data,
    status,
    statusText: status === 200 ? 'OK' : 'ERR',
    headers: {},
    config: cfg as AxiosResponse['config'],
    request: null,
  }
}

/**
 * Install the dual-mode adapter. Always install it — it routes per-request
 * to mock or real based on env config. (Previously this was gated on
 * VITE_USE_MOCKS, but dual mode needs the adapter active even when
 * real-by-default so per-route mock exceptions still work.)
 */
export function installMocks() {
  api.defaults.adapter = mockAdapter
}
