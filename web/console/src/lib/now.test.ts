// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { useNow } from './now'

beforeEach(() => vi.useFakeTimers())
afterEach(() => vi.useRealTimers())

const Host = defineComponent({
  setup() {
    const now = useNow(1000)
    return () => h('span', String(now.value))
  },
})

describe('useNow', () => {
  // The whole point: a relative time rendered once must not stay put. The
  // Health page read "checked 5 seconds ago" for as long as it was open.
  it('advances on its interval', async () => {
    const w = mount(Host)
    const first = Number(w.text())

    await vi.advanceTimersByTimeAsync(3000)

    expect(Number(w.text())).toBeGreaterThan(first)
  })

  // A page navigated away from must stop ticking rather than leaking a timer
  // that re-renders a dead component forever.
  it('stops when the component unmounts', async () => {
    const clearSpy = vi.spyOn(globalThis, 'clearInterval')
    const w = mount(Host)
    w.unmount()
    expect(clearSpy).toHaveBeenCalled()
  })
})
