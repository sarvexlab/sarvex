// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Theme-token guard (A6): the Channel Dashboard's new components must read
// colors only via var(--token) so paper/midnight both work — no hardcoded
// hex literals sneaking into a <style> block. Mirrors ledgerTheme.test.ts's
// pattern for the Lost Revenue Ledger.

import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

const FILES = [
  '../components/analytics/ChannelPanel.vue',
  '../components/channel/ChannelKpiRow.vue',
  '../components/channel/ChannelFunnel.vue',
  '../components/channel/ChannelTrendChart.vue',
  '../components/channel/ChannelComparison.vue',
]

const HEX_COLOR = /#[0-9a-fA-F]{3,8}\b/g
const ALLOWED = new Set(['#fff', '#ffffff'])

function styleBlocks(source: string): string[] {
  return [...source.matchAll(/<style[^>]*>([\s\S]*?)<\/style>/g)].map((m) => m[1])
}

describe('Channel Dashboard components use theme tokens, not hardcoded hex colors', () => {
  it.each(FILES)('%s has no unexpected hex color literal in its <style> block', (relPath) => {
    const abs = fileURLToPath(new URL(relPath, import.meta.url))
    const source = readFileSync(abs, 'utf-8')
    for (const block of styleBlocks(source)) {
      const found = (block.match(HEX_COLOR) ?? []).filter((h) => !ALLOWED.has(h.toLowerCase()))
      expect(found).toEqual([])
    }
  })
})
