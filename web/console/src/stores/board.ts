// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { defineStore } from 'pinia'
import { listBoard } from '@/api/operator'
import type { components } from '@/api/schema'

type BoardCard = components['schemas']['board_card']
type JourneyState = components['schemas']['journey_state']

const STAGES: JourneyState[] = ['connected', 'synced', 'ready', 'live']

const STAGE_LABEL: Record<JourneyState, string> = {
  connected: 'Connected',
  synced: 'Synced',
  ready: 'Ready',
  live: 'Live',
}

export const useBoardStore = defineStore('board', {
  state: () => ({
    cards: [] as BoardCard[],
    loading: false,
    loaded: false,
    error: null as string | null,
  }),

  getters: {
    stages: () => STAGES,
    stageLabels: () => STAGE_LABEL,
    byStage(s): Record<JourneyState, BoardCard[]> {
      const out: Record<string, BoardCard[]> = {}
      STAGES.forEach((st) => (out[st] = []))
      s.cards.forEach((c) => {
        if (c.state) out[c.state]?.push(c)
      })
      return out as Record<JourneyState, BoardCard[]>
    },
    totalOpen: (s) =>
      s.cards.reduce(
        (a, m) =>
          a +
          (m.open_task_counts?.merchant ?? 0) +
          (m.open_task_counts?.sarvex ?? 0) +
          (m.open_task_counts?.external ?? 0),
        0,
      ),
    blockedCount: (s) => s.cards.filter((c) => c.blocked).length,
  },

  actions: {
    async load() {
      this.loading = true
      this.error = null
      try {
        this.cards = await listBoard()
        this.loaded = true
      } catch (e: unknown) {
        this.error = e instanceof Error ? e.message : 'Failed to load board'
      } finally {
        this.loading = false
      }
    },
  },
})
