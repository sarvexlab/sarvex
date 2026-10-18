// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// The presence panel's UCP-versions row: a store's negotiation engine can
// serve more than one UCP spec generation (types.Servable, backend side),
// and the panel must show all of them — not just the one version baked
// into the static /.well-known/ucp document (published_version).

import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { i18n } from '@/i18n'
import StorePresencePanel from './StorePresencePanel.vue'
import { useStoresStore } from '@/stores/stores'
import { usePresenceStore } from '@/stores/presence'
import type { components } from '@/api/schema'

type Store = components['schemas']['store']
type Presence = components['schemas']['presence']

const store = {
  store_id: 'store-1',
  name: 'Acme Tea',
  ucp_state: 'active',
} as unknown as Store

function mountPanel(presence: Presence) {
  setActivePinia(createPinia())
  const storesStore = useStoresStore()
  storesStore.stores = [store]
  storesStore.currentStoreId = store.store_id ?? null

  const presenceStore = usePresenceStore()
  presenceStore.presence = presence
  presenceStore.manifest = { manifest: {} }
  presenceStore.loaded = true

  return mount(StorePresencePanel, { global: { plugins: [i18n] } })
}

const basePresence = {
  summary: 'Agents can see 12 products across 2 capabilities.',
  product_count: 12,
  capabilities: ['catalog', 'checkout'],
  freshness_declarations: {},
  serving: 'serving',
  profile_url: 'https://acmetea.com.au/.well-known/ucp',
  profile_reachable: true,
} as unknown as Presence

describe('StorePresencePanel — UCP versions', () => {
  it('renders a chip for every supported version, not just the published one', () => {
    const w = mountPanel({
      ...basePresence,
      published_version: '2026-04-08',
      supported_versions: ['2026-04-08', '2026-08-25'],
    })

    const chips = w.findAll('.versions-row .chip')
    expect(chips).toHaveLength(2)
    expect(chips.map((c) => c.text())).toEqual(
      expect.arrayContaining([expect.stringContaining('2026-04-08'), expect.stringContaining('2026-08-25')]),
    )
  })

  it('marks the published version distinctly from merely-supported versions', () => {
    const w = mountPanel({
      ...basePresence,
      published_version: '2026-04-08',
      supported_versions: ['2026-04-08', '2026-08-25'],
    })

    const chips = w.findAll('.versions-row .chip')
    const published = chips.find((c) => c.text().includes('2026-04-08'))!
    const other = chips.find((c) => c.text().includes('2026-08-25'))!
    expect(published.classes()).toContain('chip-you')
    expect(other.classes()).toContain('chip-us')
    expect(published.text()).toContain('published')
  })

  it('omits the versions row entirely when supported_versions is absent', () => {
    const w = mountPanel({ ...basePresence, supported_versions: undefined })
    expect(w.find('.versions-row').exists()).toBe(false)
  })
})
