// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// The three commercial tiers, shared between the plan-switch drawer
// (SettingsDrawer) and the plan summary row (SettingsSurface) so pricing
// content can't drift between the two — see sarvex-v1-architecture.md
// §Domain 8 for the plan model this backs.

import type { components } from '@/api/schema'

type Plan = components['schemas']['plan']

export interface PlanInfo {
  id: Plan
  name: string
  price: string
  cadence: string
  blurb: string
}

/** Minimal shape of vue-i18n's `t` — avoids importing vue-i18n's composer
 * type just for this module's signature. */
type Translate = (key: string) => string

/** Builds the plan table from vue-i18n (plans.* in src/locales). Callers
 * pass their component's own `t` so this stays a plain function usable
 * from both <script setup> and non-component code — same pattern as
 * lib/ledger.ts's causeLabel. */
export function buildPlans(t: Translate): PlanInfo[] {
  return [
    {
      id: 'self_serve',
      name: t('plans.self_serve.name'),
      price: t('plans.self_serve.price'),
      cadence: t('plans.self_serve.cadence'),
      blurb: t('plans.self_serve.blurb'),
    },
    {
      id: 'concierge',
      name: t('plans.concierge.name'),
      price: t('plans.concierge.price'),
      cadence: t('plans.concierge.cadence'),
      blurb: t('plans.concierge.blurb'),
    },
    {
      id: 'scale',
      name: t('plans.scale.name'),
      price: t('plans.scale.price'),
      cadence: t('plans.scale.cadence'),
      blurb: t('plans.scale.blurb'),
    },
  ]
}

export function planName(t: Translate, id: Plan | undefined | null): string {
  return buildPlans(t).find((p) => p.id === id)?.name ?? t('plans.fallback')
}
