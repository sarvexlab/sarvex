// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Shared Agent Behavior Insights vocabulary: the tier gate (reused from the
// Ledger — same requireAnalyticsTier gate, sarvex-v1-control-plane.yaml),
// the surface enum (reused from the Channel Dashboard — insights_product_demand's
// `surface` param references channel_surface directly), and the
// capability-id -> human-label lookup (R16 — the map itself lives in
// src/locales/en.ts's insights.capabilityLabels, not here).

export { isAnalyticsTier } from '@/lib/ledger'
export { surfaceLabel } from '@/lib/channel'
import type { ImpactConfidence, NegotiationFailureReason } from '@/api/insights'

/** Minimal shape of vue-i18n's useI18n() return this module needs. */
type Translator = {
  t: (key: string, named?: Record<string, unknown>) => string
  te: (key: string) => boolean
}

/**
 * Human-readable label for a capability id (R5/R16). capability_id is a
 * reverse-domain string (e.g. "dev.ucp.shopping.fulfillment") taken verbatim
 * from the negotiation's requested-capability set — which also makes it a
 * valid vue-i18n key path under insights.capabilityLabels (dots nest the
 * same way in both). `te` decides whether a mapping exists; when it doesn't,
 * this falls back to the raw id (R16) rather than vue-i18n's own
 * missing-key behavior (which would return the whole dotted lookup path,
 * not the id).
 */
export function capabilityLabel(i18n: Translator, capabilityId: string): string {
  const key = `insights.capabilityLabels.${capabilityId}`
  return i18n.te(key) ? i18n.t(key) : capabilityId
}

/** Plain-language label for a negotiation failure reason (R9), via vue-i18n
 * (insights.negotiation.reason.* in src/locales). Falls back to a humanized
 * form of the raw enum value for any reason the frontend doesn't have a
 * label for yet — the schema's enum is closed today
 * (version_unsupported/capabilities_incompatible) but the API's own doc
 * comment derives it from a Go function, not a value this frontend should
 * assume it can enumerate exhaustively forever (A4: a fixture reason unknown
 * at build time must still render). */
export function negotiationFailureReasonLabel(i18n: Translator, reason: NegotiationFailureReason | string): string {
  const key = `insights.negotiation.reason.${reason}`
  if (i18n.te(key)) return i18n.t(key)
  return reason
    .split('_')
    .filter(Boolean)
    .map((w: string) => w[0].toUpperCase() + w.slice(1))
    .join(' ')
}

/** Whether a capability gap's estimated_impact_minor is safe to render as a
 * money figure (R6) — only when confidence claims some or all of the
 * counted sessions were demonstrably attributed. "unmeasurable" must never
 * render as $0, which is why this exists as a gate rather than trusting the
 * raw amount alone (an unmeasurable row's amount is always 0 by
 * construction, but the UI must not depend on that being true forever). */
export function impactIsMeasurable(confidence: ImpactConfidence | string): boolean {
  return confidence === 'measured' || confidence === 'partial'
}
