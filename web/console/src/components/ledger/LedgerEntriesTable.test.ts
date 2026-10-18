// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Row expand (R13): checkout_id, failure_code, currency, status_changed_at,
// and a deep link to the session detail route when it exists — a plain id
// (no dead link) when it doesn't.

import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '@/i18n'
import LedgerEntriesTable from './LedgerEntriesTable.vue'
import type { LedgerEntry } from '@/api/ledger'

const entry = (over: Partial<LedgerEntry>): LedgerEntry => ({
  entry_id: 'led-1',
  occurred_at: '2026-08-05T10:00:00Z',
  checkout_id: 'chk-1',
  store_id: 'str-1',
  surface: 'ucp_checkout',
  stage: 'payment',
  failure_code: 'CONNECTOR_CREATE_FAILED',
  cause: 'payment_handler',
  recommended_action: 'Check your payment handler config',
  lost_revenue_minor: 1000,
  currency: 'USD',
  revenue_unknown: false,
  status: 'open',
  status_changed_at: '2026-08-05T09:00:00Z',
  created_at: '2026-08-05T10:00:00Z',
  ...over,
})

async function mountTable(entries: LedgerEntry[], routeNames: string[] = ['checkout-session-detail']) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div/>' } },
      ...routeNames.map((name) => ({
        path: `/checkout-sessions/:checkoutId`,
        name,
        component: { template: '<div/>' },
      })),
    ],
  })
  await router.push('/')
  await router.isReady()
  return mount(LedgerEntriesTable, {
    props: { entries, statusPending: {} },
    global: { plugins: [router, i18n] },
  })
}

describe('LedgerEntriesTable — row expand (R13)', () => {
  it('shows checkout_id/failure_code/currency/status_changed_at and a deep link when the route exists', async () => {
    const w = await mountTable([entry({})])
    await w.find('.expand-btn').trigger('click')

    const grid = w.find('.expand-grid')
    expect(grid.text()).toContain('CONNECTOR_CREATE_FAILED')
    expect(grid.text()).toContain('USD')

    const link = grid.find('a.link.mono')
    expect(link.exists()).toBe(true)
    expect(link.text()).toBe('chk-1')
    expect(link.attributes('href')).toBe('/checkout-sessions/chk-1')
  })

  it('renders a plain id (no link) when the session detail route does not exist', async () => {
    const w = await mountTable([entry({})], [])
    await w.find('.expand-btn').trigger('click')

    const grid = w.find('.expand-grid')
    expect(grid.find('a').exists()).toBe(false)
    expect(grid.text()).toContain('chk-1')
  })

  it('collapses again on a second click', async () => {
    const w = await mountTable([entry({})])
    await w.find('.expand-btn').trigger('click')
    expect(w.find('.expand-grid').exists()).toBe(true)
    await w.find('.expand-btn').trigger('click')
    expect(w.find('.expand-grid').exists()).toBe(false)
  })
})

describe('LedgerEntriesTable — not-priced rendering (R11)', () => {
  it('never renders $0.00 for a revenue_unknown entry', async () => {
    const w = await mountTable([entry({ revenue_unknown: true, lost_revenue_minor: 0 })])
    expect(w.find('.not-priced').text()).toBe('Not priced')
    expect(w.text()).not.toContain('$0.00')
  })
})

describe('LedgerEntriesTable — forward-only status actions', () => {
  it('open entries offer both Acknowledge and Resolve', async () => {
    const w = await mountTable([entry({ status: 'open' })])
    const labels = w.findAll('.status-btn').map((b) => b.text())
    expect(labels).toEqual(['Acknowledge', 'Resolve'])
  })

  it('acknowledged entries offer only Resolve', async () => {
    const w = await mountTable([entry({ status: 'acknowledged' })])
    const labels = w.findAll('.status-btn').map((b) => b.text())
    expect(labels).toEqual(['Resolve'])
  })

  it('resolved entries offer no status action', async () => {
    const w = await mountTable([entry({ status: 'resolved' })])
    expect(w.findAll('.status-btn')).toHaveLength(0)
  })

  it('clicking a status button emits change-status with the entry id and target status', async () => {
    const w = await mountTable([entry({ entry_id: 'led-9', status: 'open' })])
    await w.find('.status-btn').trigger('click')
    expect(w.emitted('change-status')?.[0]).toEqual(['led-9', 'acknowledged'])
  })
})
