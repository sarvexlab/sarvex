// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Pure-logic tests for the connect-gate.
//
// This is what failed in step 4 — the original Pinia getter cached its
// branch on first eval and never re-evaluated. The plain function fixed
// that. These tests pin the four states it has to handle.

import { describe, it, expect } from 'vitest'
import { needsConnect } from './needsConnect'
import type { components } from '@/api/schema'

type Store = components['schemas']['store']
type Connector = components['schemas']['connector']

// Tiny stubs that match the slice of the stores the function reads.
function storesStub(current: Store | null) {
  return { currentStore: current } as unknown as Parameters<typeof needsConnect>[0]
}

function connectorsStub(forStoreReturn: Connector | null) {
  return {
    forStore: () => forStoreReturn,
  } as unknown as Parameters<typeof needsConnect>[1]
}

const acmeNoConnector: Store = {
  store_id: 'str-acme-au',
  name: 'Acme',
  platform: 'woocommerce',
  domain: 'acme.test',
  connector_id: null,
  journey_state: 'connected',
}

const acmeWithConnector: Store = { ...acmeNoConnector, connector_id: 'con-1' }

const pairingConnector: Connector = {
  connector_id: 'con-1',
  store_id: 'str-acme-au',
  platform: 'woocommerce',
  status: 'pairing',
  sync_model: 'realtime',
  last_signal_at: null,
  last_full_sync_at: null,
  freshness_warning: false,
  plugin_version: null,
}

const healthyConnector: Connector = { ...pairingConnector, status: 'healthy' }

describe('needsConnect', () => {
  it('returns false when no current store (operator on the board, etc.)', () => {
    expect(needsConnect(storesStub(null), connectorsStub(null))).toBe(false)
  })

  it('returns true when store has no connector_id at all', () => {
    expect(needsConnect(storesStub(acmeNoConnector), connectorsStub(null))).toBe(true)
  })

  it('returns true when connector exists but is still pairing', () => {
    expect(needsConnect(storesStub(acmeWithConnector), connectorsStub(pairingConnector))).toBe(true)
  })

  it('returns false once the connector flips to healthy', () => {
    expect(needsConnect(storesStub(acmeWithConnector), connectorsStub(healthyConnector))).toBe(false)
  })

  it('returns false even if connector is degraded — surfaces as a Path task, not a gate', () => {
    const degraded: Connector = { ...pairingConnector, status: 'degraded' }
    expect(needsConnect(storesStub(acmeWithConnector), connectorsStub(degraded))).toBe(false)
  })

  it('returns true when the store has a connector_id but connectors store has no record (race/empty)', () => {
    expect(needsConnect(storesStub(acmeWithConnector), connectorsStub(null))).toBe(true)
  })
})
