// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Shared severity/label mapping for the Checkout Escalation Console — used
// by both the session list table and the session detail header so state
// coloring never drifts between the two.

import type { ChipKind } from '@/components/ui/Chip.vue'
import type { CheckoutSession, CheckoutSessionState, ListCheckoutSessionsFilter } from '@/api/checkoutSessions'

/** All checkout_session_state values (sarvex-v1-control-plane.yaml), in the
 * order the state machine progresses. */
export const CHECKOUT_SESSION_STATES: CheckoutSessionState[] = [
  'initiated',
  'cart_active',
  'escalated',
  'awaiting_payment',
  'completed',
  'failed',
  'abandoned',
]

const STATE_CHIP_KIND: Record<CheckoutSessionState, ChipKind> = {
  initiated: 'quiet',
  cart_active: 'quiet',
  awaiting_payment: 'quiet',
  escalated: 'warn',
  completed: 'ok',
  failed: 'danger',
  abandoned: 'quiet',
}

/** Severity color for a session state chip — escalated (warn) and failed
 * (danger) are deliberately distinct colors (R3). */
export function checkoutStateChipKind(state: CheckoutSessionState | undefined): ChipKind {
  return state ? (STATE_CHIP_KIND[state] ?? 'quiet') : 'quiet'
}

const KNOWN_CAUSES = [
  'connector_create_failed',
  'connector_update_failed',
  'pricing_stale',
  'order_creation_failed',
  'escalation_stalled',
  'buyer_abandoned',
  'unknown',
]

/** Minimal shape of vue-i18n's `t` — avoids importing vue-i18n's composer
 * type just for this helper's signature. */
type Translate = (key: string) => string

/** Plain-language label for a diagnosis.cause / failure.cause code, via
 * vue-i18n (checkout.cause.* in src/locales). Callers pass their
 * component's own `t` so this stays a plain function usable from both
 * <script setup> and non-component code. */
export function causeLabel(t: Translate, cause: string | null | undefined): string {
  if (!cause) return '—'
  if (!KNOWN_CAUSES.includes(cause)) return cause
  return t(`checkout.cause.${cause}`)
}

/** All diagnosis.cause values (sarvex-v1-control-plane.yaml's closed enum). */
export const CHECKOUT_CAUSES = KNOWN_CAUSES

/**
 * GET /checkout-sessions/live (sarvex-v1-control-plane.yaml) accepts only
 * window_minutes + limit — none of the list filters. The Live result set is
 * already capped small (default 200) and polled, so the list filters (R4)
 * are applied to it client-side with this predicate instead of being sent
 * to the server; History sends the same filters server-side via
 * listCheckoutSessions. Keeping one predicate definition means Live and
 * History never silently disagree about what a filter means.
 */
export function matchesCheckoutSessionFilter(
  session: CheckoutSession,
  filter: ListCheckoutSessionsFilter,
): boolean {
  if (filter.state && session.state !== filter.state) return false
  if (filter.surface && session.surface !== filter.surface) return false
  if (filter.store_id && session.store_id !== filter.store_id) return false
  if (filter.cause && session.diagnosis?.cause !== filter.cause) return false
  if (filter.escalated_only && session.escalation == null) return false
  return true
}
