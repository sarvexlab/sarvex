// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// The merchant routes to /connect until they have a connector that's past
// pairing. Kept as a plain function (not a Pinia getter) so its dependency
// set is recomputed on every call — a getter with an early-return path can
// freeze its tracked deps on first eval and never re-evaluate when the
// other branch's inputs change.

import type { useConnectorsStore } from '@/stores/connectors'
import type { useStoresStore } from '@/stores/stores'

type StoresStore = ReturnType<typeof useStoresStore>
type ConnectorsStore = ReturnType<typeof useConnectorsStore>

export function needsConnect(stores: StoresStore, connectors: ConnectorsStore): boolean {
  const cur = stores.currentStore
  if (!cur) return false
  if (!cur.connector_id) return true
  const connector = connectors.forStore(cur.store_id ?? '')
  // Connector exists but the merchant is still mid-pairing → stay on the
  // wizard. Anything past `pairing` (healthy / degraded / down) counts as
  // connected — degraded / down are real states the merchant sees on the
  // Path as a task, not as a setup redirect.
  return !connector || connector.status === 'pairing'
}
