// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Theme-token guard (A6): the Lost Revenue Ledger's new components must
// read colors only via var(--token) so paper/midnight both work — no
// hardcoded hex literals sneaking into a <style> block. tokens.css itself
// is exempt (it's the one file allowed to define the hex values).

import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

const FILES = [
  '../components/ui/TierGate.vue',
  '../components/ledger/LedgerSummaryHeader.vue',
  '../components/ledger/LedgerCauseBreakdown.vue',
  '../components/ledger/LedgerEntriesTable.vue',
  '../components/analytics/LostRevenuePanel.vue',
]

const HEX_COLOR = /#[0-9a-fA-F]{3,8}\b/g
// Pure white on a solid accent-colored button is the one hex literal this
// codebase already accepts outside tokens.css (see LoadingButton.vue's
// .btn-primary) — accent is chosen for contrast against white in both
// themes, so it's a deliberate constant, not a theme-dependent color that
// could go wrong in one theme.
const ALLOWED = new Set(['#fff', '#ffffff'])

function styleBlocks(source: string): string[] {
  return [...source.matchAll(/<style[^>]*>([\s\S]*?)<\/style>/g)].map((m) => m[1])
}

describe('Ledger components use theme tokens, not hardcoded hex colors', () => {
  it.each(FILES)('%s has no unexpected hex color literal in its <style> block', (relPath) => {
    const abs = fileURLToPath(new URL(relPath, import.meta.url))
    const source = readFileSync(abs, 'utf-8')
    for (const block of styleBlocks(source)) {
      const found = (block.match(HEX_COLOR) ?? []).filter((h) => !ALLOWED.has(h.toLowerCase()))
      expect(found).toEqual([])
    }
  })
})
