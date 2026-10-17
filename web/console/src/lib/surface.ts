// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Presentation helpers shared by the Surfaces page and the generic
// enrollment component. Deliberately keyed on enrollment *state*, never on
// surface id — nothing about how a surface renders may depend on which
// surface it is.

import type { ChipKind } from '@/components/ui/Chip.vue'
import type { SurfaceEnrollment, SurfaceState } from '@/api/surfaces'

const STATE_KIND: Record<SurfaceState, ChipKind> = {
  available: 'quiet',
  prerequisites_pending: 'warn',
  configured: 'you',
  submitted: 'us',
  verifying: 'us',
  enrolled: 'ok',
  suspended: 'danger',
}

/** Chip colour for an enrollment state. */
export function stateKind(state: SurfaceState | undefined): ChipKind {
  return (state && STATE_KIND[state]) || 'quiet'
}

/** Sort order for the Surfaces page: enrolled first (what the merchant is
 * live on), then work in progress, then everything still available. Within
 * a band, surface id keeps it stable. */
const BAND: Record<SurfaceState, number> = {
  enrolled: 0,
  suspended: 1,
  verifying: 2,
  submitted: 2,
  configured: 2,
  prerequisites_pending: 2,
  available: 3,
}

export function sortEnrollments(list: SurfaceEnrollment[]): SurfaceEnrollment[] {
  return [...list].sort((a, b) => {
    const band = (BAND[a.state] ?? 9) - (BAND[b.state] ?? 9)
    return band !== 0 ? band : a.surface_id.localeCompare(b.surface_id)
  })
}
