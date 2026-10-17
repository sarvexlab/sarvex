// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Every Board column (a journey_state) needs its own label. The journey.stage
// labels moved to ucp_state, which left the Board rendering raw i18n keys.

import { describe, it, expect } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useBoardStore } from './board'
import en from '@/locales/en'

describe('board stage labels', () => {
  it('has an en label for every board column', () => {
    setActivePinia(createPinia())
    const labels = en.board.stage as Record<string, string>
    for (const s of useBoardStore().stages) {
      expect(labels[s], `board.stage.${s}`).toBeTruthy()
    }
  })
})
