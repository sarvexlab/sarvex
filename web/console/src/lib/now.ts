// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// A reactive clock, for anything rendering a relative time.
//
// `formatFriendlyDateTime` produces "2 minutes ago" from a fixed input, so a
// template that calls it once renders a time that never moves — the Health
// page read "checked 5 seconds ago" for as long as it stayed open, and the
// timestamps aged silently while claiming not to. Reading `now` inside the
// formatting call gives Vue a dependency that changes, so the phrase
// re-renders on its own.

import { onUnmounted, ref, type Ref } from 'vue'

/** Default cadence. Fine grained enough that "just now" gives way promptly,
 *  coarse enough to be invisible in a frame budget. */
const DEFAULT_INTERVAL_MS = 15_000

/**
 * Returns a ref holding the current epoch milliseconds, advancing on an
 * interval. The timer is cleared when the calling component unmounts, so a
 * page that is navigated away from stops ticking.
 */
export function useNow(intervalMs: number = DEFAULT_INTERVAL_MS): Ref<number> {
  const now = ref(Date.now())
  const handle = setInterval(() => {
    now.value = Date.now()
  }, intervalMs)
  onUnmounted(() => clearInterval(handle))
  return now
}
