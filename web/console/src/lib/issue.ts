// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Merchant-facing label for a product_issue's problem code. Shared between
// ProductDrawer (a product's own issue list) and TaskDrawer (the same issue
// surfaced as a readiness task's typed drill-in) so the two stay in sync.

/** Minimal shape of vue-i18n's `t` — avoids importing vue-i18n's composer
 * type just for this helper's signature. */
type Translate = (key: string) => string

const KNOWN_PROBLEMS = ['missing', 'free_text', 'non_standard_enum', 'inconsistent']

export function problemLabel(t: Translate, p: string | undefined): string {
  if (!p) return '—'
  if (!KNOWN_PROBLEMS.includes(p)) return p
  return t(`store.product.problem.${p}`)
}
