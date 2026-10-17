// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Stateful mock of the agent-surface endpoints, for the console's standalone
// (mock) mode. It mirrors the backend's generic behaviour — a registry of
// descriptors, a per-store enrollment that advances as prerequisites pass —
// with just enough per-surface behaviour (example_marketplace's file appears once
// enrolled; its fetch lands on the second re-check) to demo a whole flow.

import type { components } from './schema'

type Descriptor = components['schemas']['surface_descriptor']
type Enrollment = components['schemas']['surface_enrollment']
type Prereq = components['schemas']['prerequisite_result']

const descriptors: Descriptor[] = [
  {
    surface_id: 'direct_agent',
    display_name_key: 'surfaces.direct_agent.name',
    description_key: 'surfaces.direct_agent.description',
    protocol: 'ucp',
    required_connectors: [],
    required_capabilities: [],
    prerequisite_checks: [],
    artifacts: [],
    config_schema: [],
    external_steps: [],
    verification_signal: '',
    auto_enroll: true,
  },
  {
    surface_id: 'example_marketplace',
    display_name_key: 'surfaces.example_marketplace.name',
    description_key: 'surfaces.example_marketplace.description',
    protocol: 'ucp',
    required_connectors: [],
    required_capabilities: [],
    prerequisite_checks: ['example_return_policy', 'example_feed_generated', 'example_products_eligible'],
    artifacts: ['example_feed'],
    config_schema: [
      { key: 'return_policy_configured', type: 'bool', required: true,
        label_key: 'surfaces.example_marketplace.config.returnPolicy.label', help_key: 'surfaces.example_marketplace.config.returnPolicy.help' },
      { key: 'excluded_skus', type: 'string_list', required: false,
        label_key: 'surfaces.example_marketplace.config.excludedSkus.label', help_key: 'surfaces.example_marketplace.config.excludedSkus.help' },
      { key: 'notices_json', type: 'json', required: false,
        label_key: 'surfaces.example_marketplace.config.notices.label', help_key: 'surfaces.example_marketplace.config.notices.help' },
    ],
    external_steps: [
      { step_id: 'interest_form', label_key: 'surfaces.example_marketplace.step.interestForm',
        url: 'https://example.test/marketplace-interest-form', owner: 'merchant' },
      { step_id: 'sandbox_validation', label_key: 'surfaces.example_marketplace.step.sandboxValidation', owner: 'surface' },
    ],
    verification_signal: 'example_first_fetch',
    auto_enroll: false,
  },
]

interface State {
  state: Enrollment['state']
  config: Record<string, string>
  rechecksWhileVerifying: number
}
const byStore = new Map<string, Map<string, State>>()

function stateFor(storeId: string, surfaceId: string): State {
  let m = byStore.get(storeId)
  if (!m) byStore.set(storeId, (m = new Map()))
  let s = m.get(surfaceId)
  if (!s) {
    const auto = descriptors.find((d) => d.surface_id === surfaceId)?.auto_enroll
    m.set(surfaceId, (s = { state: auto ? 'enrolled' : 'available', config: {}, rechecksWhileVerifying: 0 }))
  }
  return s
}

function examplePrereqs(s: State): { results: Prereq[]; pass: boolean } {
  const now = new Date().toISOString()
  const results: Prereq[] = [
    {
      check_id: 'example_return_policy',
      label_key: 'surfaces.example_marketplace.prereq.returnPolicy',
      passed: s.config.return_policy_configured === 'true',
      reason: 'return policy not confirmed as configured',
      remediation_key: 'surfaces.example_marketplace.remediation.returnPolicy',
      remediation_url: 'https://example.test/return-policy',
      evaluated_at: now,
    },
    { check_id: 'example_feed_generated', label_key: 'surfaces.example_marketplace.prereq.feedGenerated', passed: true, evaluated_at: now },
    { check_id: 'example_products_eligible', label_key: 'surfaces.example_marketplace.prereq.productsEligible', passed: true, evaluated_at: now },
  ]
  return { results, pass: results.every((r) => r.passed) }
}

function render(storeId: string, d: Descriptor): Enrollment {
  const s = stateFor(storeId, d.surface_id)
  const started = s.state !== 'available'
  const prereq = d.surface_id === 'example_marketplace' && started ? examplePrereqs(s) : { results: [], pass: true }
  const excluded = (s.config.excluded_skus ?? '').split('\n').filter((x) => x.trim()).length
  return {
    surface_id: d.surface_id,
    store_id: storeId,
    state: s.state,
    prerequisites: prereq.results,
    artifacts:
      d.surface_id === 'example_marketplace' && started
        ? [{
            artifact_id: 'example_feed',
            label_key: 'surfaces.example_marketplace.artifact.feed',
            url: 'https://api.sarvex.test/artifacts/3f9a1c0e8b7d4a62.xml',
            detail: {
              product_count: '48',
              eligible_count: String(48 - excluded),
              excluded_count: String(excluded),
              blocked_needs_notice_count: '0',
            },
            generated_at: new Date().toISOString(),
          }]
        : [],
    config: s.config,
    updated_at: new Date().toISOString(),
    descriptor: d,
    blocked: false,
  } as Enrollment
}

/** Advance as far as the evidence allows — the backend's converge pass. */
function converge(storeId: string, d: Descriptor) {
  const s = stateFor(storeId, d.surface_id)
  if (d.surface_id !== 'example_marketplace') return
  const { pass } = examplePrereqs(s)
  if (s.state === 'prerequisites_pending' && pass) s.state = 'configured'
  else if (s.state === 'configured' && !pass) s.state = 'prerequisites_pending'
  if (s.state === 'verifying') {
    // The marketplace "fetches the file" on the second re-check.
    if (++s.rechecksWhileVerifying >= 2) s.state = 'enrolled'
  }
}

function find(surfaceId: string): Descriptor | undefined {
  return descriptors.find((d) => d.surface_id === surfaceId)
}

export const surfaceMock = {
  registry: () => ({ data: descriptors, page: { cursor: null, has_more: false } }),
  list: (storeId: string) => ({
    data: descriptors.map((d) => render(storeId, d)),
    page: { cursor: null, has_more: false },
  }),
  get(storeId: string, surfaceId: string): Enrollment | undefined {
    const d = find(surfaceId)
    return d ? render(storeId, d) : undefined
  },
  enroll(storeId: string, surfaceId: string): Enrollment | undefined {
    const d = find(surfaceId)
    if (!d) return undefined
    const s = stateFor(storeId, surfaceId)
    if (s.state === 'available') s.state = 'prerequisites_pending'
    converge(storeId, d)
    return render(storeId, d)
  },
  evaluate(storeId: string, surfaceId: string): Enrollment | undefined {
    const d = find(surfaceId)
    if (!d) return undefined
    converge(storeId, d)
    return render(storeId, d)
  },
  setConfig(storeId: string, surfaceId: string, config: Record<string, string>): Enrollment | undefined {
    const d = find(surfaceId)
    if (!d) return undefined
    stateFor(storeId, surfaceId).config = config
    converge(storeId, d)
    return render(storeId, d)
  },
  submit(storeId: string, surfaceId: string): Enrollment | undefined {
    const d = find(surfaceId)
    if (!d) return undefined
    const s = stateFor(storeId, surfaceId)
    if (s.state === 'configured') {
      s.state = 'verifying'
      s.rechecksWhileVerifying = 0
    }
    return render(storeId, d)
  },
  withdraw(storeId: string, surfaceId: string) {
    const s = stateFor(storeId, surfaceId)
    if (!find(surfaceId)?.auto_enroll) {
      s.state = 'available'
      s.config = {}
    }
  },
}
