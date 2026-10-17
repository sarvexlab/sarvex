// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Shared self-loading hook for the named task widgets.
//
// Every widget reads one domain endpoint of its own rather than having the
// data threaded down through PathSurface — the same pattern SyncedStage and
// ReadinessPill already use, and the reason the widgets are drop-in for any
// stage that wants them.
//
// Failures are swallowed on purpose. A widget is a secondary read on the
// Path: if GET /orders or GET /health is down, the merchant should still get
// their task feed, and the widget should fall back to its neutral state
// rather than take the page with it. The surfaces that own these endpoints
// (Health, Store, Validation) are where a load error is actually actionable,
// and they report it there.

import { onMounted, watch } from 'vue'
import { useStoresStore } from '@/stores/stores'

export function useWidgetData(load: () => Promise<unknown> | unknown): void {
  const stores = useStoresStore()

  async function run() {
    if (!stores.currentStore) return
    try {
      await load()
    } catch {
      // Neutral state; the owning surface surfaces the error.
    }
  }

  onMounted(run)
  watch(() => stores.currentStoreId, run)
}
