// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// The Health page, read as a merchant would read it.
//
// Every assertion here corresponds to something the page used to show
// verbatim from the monitoring system's internals, where the merchant could
// not tell what it meant or whether it was theirs to fix:
//
//   "Endpoint health: degraded"                       — no reason given
//   "signature_roundtrip"                             — a probe id
//   "1 critical consistency finding(s) detected."     — jargon, and "(s)"
//   "Served data is stale (source sync 2h0m58.30s old)" — a Go duration
//   "system:plugin_gateway · field server_health"     — internal identifiers
//   every timestamp                                   — raw UTC ISO strings

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { routes } from '@/router'
import { i18n } from '@/i18n'
import HealthSurface from './HealthSurface.vue'
import { useHealthStore } from '@/stores/health'
import { useStoresStore } from '@/stores/stores'
import type { components } from '@/api/schema'

type Store = components['schemas']['store']

const store = { store_id: 's1', name: 'Acme', platform: 'woocommerce', ucp_state: 'active' } as Store

// Shaped from a real GET /stores/{id}/health response.
const health = {
  statements: [
    {
      kind: 'consistency',
      ok: false,
      message: '1 critical consistency finding(s) detected.',
      params: { critical_count: 1, warning_count: 0 },
      checked_at: '2026-09-06T11:18:14.126Z',
    },
    {
      kind: 'sync_freshness',
      ok: false,
      message: 'Served data is stale (source sync 2h0m58.304129489s old).',
      params: { age_seconds: 7597 },
      checked_at: '2026-09-06T11:18:14.126Z',
    },
  ],
  endpoint_status: {
    status: 'degraded',
    evaluated_at: '2026-09-06T11:14:41.553Z',
    checks: {
      catalog_lookup: { state: 'pass', last_pass_at: '2026-09-06T11:10:16.428Z' },
      signature_roundtrip: {
        state: 'degraded',
        last_pass_at: '2026-09-06T10:56:58.236Z',
        last_fail_at: '2026-09-06T11:06:12.153Z',
      },
      profile_reachable: { state: 'pass', last_pass_at: '2026-09-06T11:14:41.553Z' },
      negotiation_handshake: { state: 'pass', last_pass_at: '2026-09-06T11:09:41.358Z' },
    },
  },
  active_alerts: [],
}

const findings = [
  {
    finding_id: 'probe:s1:system:plugin_gateway:server_health',
    sku: 'system:plugin_gateway',
    field: 'server_health',
    sources: { status: '502' },
    severity: 'critical',
    detected_at: '2026-09-06T10:59:41.061Z',
  },
]

async function mountPage() {
  const router = createRouter({ history: createMemoryHistory(), routes })
  router.push('/health')
  await router.isReady()

  const w = mount(HealthSurface, { global: { plugins: [router, i18n] } })
  await flushPromises()
  return w
}

beforeEach(() => {
  setActivePinia(createPinia())
  const stores = useStoresStore()
  stores.stores = [store]
  stores.currentStoreId = 's1'
  stores.loaded = true

  const h = useHealthStore()
  h.health = health as never
  h.findings = findings as never
  h.loaded = true
  vi.spyOn(h, 'load').mockResolvedValue(undefined)
})

describe('endpoint status', () => {
  it('says why the status is degraded rather than only that it is', async () => {
    const text = (await mountPage()).text()
    expect(text).toContain('Request signing')
    expect(text).toMatch(/intermittent/i)
  })

  it('tells the merchant whether they need to act', async () => {
    expect((await mountPage()).text()).toMatch(/already a task on your Path/i)
  })
})

describe('the four probes', () => {
  it('names them in merchant language, not by probe id', async () => {
    const text = (await mountPage()).text()
    expect(text).toContain('Request signing')
    expect(text).toContain('Store discoverable')
    expect(text).toContain('Capability handshake')
    expect(text).toContain('Product lookup')
    expect(text).not.toContain('signature_roundtrip')
    expect(text).not.toContain('negotiation_handshake')
  })

  it('explains what each one proves', async () => {
    expect((await mountPage()).text()).toMatch(/verify the signature/i)
  })
})

describe('statements', () => {
  it('states the consistency count without the server sentence or its "(s)"', async () => {
    const text = (await mountPage()).text()
    expect(text).not.toContain('finding(s)')
    expect(text).not.toContain('consistency finding(s) detected')
    expect(text).toMatch(/1 serious mismatch\b/)
  })

  it('renders staleness as a readable span, never a Go duration', async () => {
    const text = (await mountPage()).text()
    expect(text).not.toMatch(/\d+h\d+m[\d.]+s/)
    expect(text).toContain('2 hours')
  })

  it('explains why served data went stale', async () => {
    expect((await mountPage()).text()).toMatch(/sync didn't finish|catches up on its own/i)
  })
})

describe('consistency findings', () => {
  it('describes an integration finding in words, not internal identifiers', async () => {
    const text = (await mountPage()).text()
    expect(text).not.toContain('system:plugin_gateway')
    expect(text).not.toContain('server_health')
    expect(text).toContain('The Sarvex plugin on your store')
    expect(text).toMatch(/isn't responding/i)
    expect(text).toContain('502')
  })
})

describe('timestamps', () => {
  // Intl formatting is locale-driven; the assertion is that no raw ISO
  // string survives to the page, whatever the runner's locale is.
  it('shows no raw UTC ISO timestamps anywhere', async () => {
    const text = (await mountPage()).text()
    expect(text).not.toMatch(/\d{4}-\d{2}-\d{2}T\d{2}:\d{2}/)
    expect(text).not.toContain('Z')
  })

  // The page used to read "checked 5 seconds ago" for as long as it stayed
  // open — every relative phrase claimed the page had just loaded, however
  // long it had actually been open, because formatFriendlyDateTime was
  // called once and never again. useNow's interval is what should make the
  // template re-run; this proves that wiring end to end, with no refetch —
  // the clock alone is what has to move the text.
  it('advances the relative time on its own as the page stays open', async () => {
    vi.useFakeTimers()
    try {
      vi.setSystemTime(new Date('2026-09-06T11:20:00.000Z'))

      const router = createRouter({ history: createMemoryHistory(), routes })
      router.push('/health')
      await router.isReady()

      const w = mount(HealthSurface, { global: { plugins: [router, i18n] } })
      await vi.advanceTimersByTimeAsync(0)

      const h = useHealthStore()
      const loadCallsAfterMount = (h.load as ReturnType<typeof vi.fn>).mock.calls.length

      const before = w.text()
      expect(before).toMatch(/Evaluated 5 minutes ago/)

      // Well past useNow's 15s tick, no page interaction, no refetch.
      await vi.advanceTimersByTimeAsync(5 * 60 * 1000)

      const after = w.text()
      expect(after).toMatch(/Evaluated 10 minutes ago/)
      expect(after).not.toBe(before)

      // A pure clock tick must not trigger a network refetch — only actual
      // change events (WebSocket push, mount, store switch) should.
      expect((h.load as ReturnType<typeof vi.fn>).mock.calls.length).toBe(loadCallsAfterMount)
    } finally {
      vi.useRealTimers()
    }
  })
})
