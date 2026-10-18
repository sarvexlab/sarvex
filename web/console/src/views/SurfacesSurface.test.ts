// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// The Surfaces page renders entirely from the registry, and the generic
// enrollment component renders every surface's flow from its descriptor.
//
// The test that matters most is the last one: a fixture surface with
// prerequisites unlike the example marketplace's, no artifacts, and a different verification
// signal must render and complete its flow with no new frontend code. If
// making that pass ever requires touching SurfacesSurface.vue or
// SurfaceEnrollmentFlow.vue, the abstraction is wrong (R20).

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { routes } from '@/router'
import { i18n } from '@/i18n'
import SurfacesSurface from './SurfacesSurface.vue'
import SurfaceEnrollmentFlow from '@/components/surface/SurfaceEnrollmentFlow.vue'
import { useSurfacesStore } from '@/stores/surfaces'
import { useActivationStore } from '@/stores/activation'
import { useStoresStore } from '@/stores/stores'
import type { SurfaceEnrollment } from '@/api/surfaces'
import type { components } from '@/api/schema'

type Descriptor = components['schemas']['surface_descriptor']

vi.mock('@/api/surfaces', () => ({
  listSurfaceRegistry: vi.fn(),
  listStoreSurfaces: vi.fn(),
  enrollSurface: vi.fn(),
  evaluatePrerequisites: vi.fn(),
  submitSurface: vi.fn(),
  setSurfaceConfig: vi.fn(),
  withdrawSurface: vi.fn(),
}))
vi.mock('@/api/activation', () => ({
  getActivation: vi.fn(),
  verifyActivation: vi.fn(),
}))

import { enrollSurface, listStoreSurfaces, setSurfaceConfig, submitSurface } from '@/api/surfaces'
import { getActivation } from '@/api/activation'

// ── Descriptors ──────────────────────────────────────────────────────────

const directAgent: Descriptor = {
  surface_id: 'direct_agent',
  display_name_key: 'surfaces.direct_agent.name',
  description_key: 'surfaces.direct_agent.description',
  protocol: 'ucp',
  auto_enroll: true,
}

const exampleMarketplace: Descriptor = {
  surface_id: 'example_marketplace',
  display_name_key: 'surfaces.example_marketplace.name',
  description_key: 'surfaces.example_marketplace.description',
  protocol: 'ucp',
  prerequisite_checks: ['example_return_policy', 'example_feed_generated', 'example_products_eligible'],
  artifacts: ['example_feed'],
  config_schema: [
    {
      key: 'return_policy_configured',
      type: 'bool',
      required: true,
      label_key: 'surfaces.example_marketplace.config.returnPolicy.label',
    },
    {
      key: 'excluded_skus',
      type: 'string_list',
      required: false,
      label_key: 'surfaces.example_marketplace.config.excludedSkus.label',
    },
    {
      key: 'notices_json',
      type: 'json',
      required: false,
      label_key: 'surfaces.example_marketplace.config.notices.label',
    },
  ],
  external_steps: [
    {
      step_id: 'interest_form',
      label_key: 'surfaces.example_marketplace.step.interestForm',
      url: 'https://example.test/form',
      owner: 'merchant',
    },
  ],
  verification_signal: 'example_first_fetch',
}

// R20's fixture surface. Deliberately unlike example_marketplace in every dimension
// the component renders: different prerequisites, NO artifacts, a different
// verification signal, and a differently-owned external step.
const fixtureSurface: Descriptor = {
  surface_id: 'fixture_marketplace',
  display_name_key: 'fixture.name',
  description_key: 'fixture.description',
  protocol: 'ucp',
  prerequisite_checks: ['fixture_partner_api_key', 'fixture_region_supported'],
  artifacts: [],
  external_steps: [
    {
      step_id: 'fixture_partner_agreement',
      label_key: 'fixture.step.agreement',
      url: 'https://partner.example/agreement',
      owner: 'merchant',
    },
  ],
  verification_signal: 'fixture_partner_callback',
}

function enrollment(
  descriptor: Descriptor,
  overrides: Partial<SurfaceEnrollment> = {},
): SurfaceEnrollment {
  return {
    surface_id: descriptor.surface_id,
    store_id: 'str-1',
    state: 'available',
    prerequisites: [],
    artifacts: [],
    updated_at: '2026-01-01T00:00:00Z',
    descriptor,
    blocked: false,
    ...overrides,
  } as SurfaceEnrollment
}

async function mountPage() {
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push('/surfaces')
  await router.isReady()
  const w = mount(SurfacesSurface, { global: { plugins: [router, i18n] } })
  await flushPromises()
  return w
}

function mountFlow(e: SurfaceEnrollment) {
  return mount(SurfaceEnrollmentFlow, {
    global: { plugins: [i18n] },
    props: { enrollment: e },
  })
}

beforeEach(() => {
  setActivePinia(createPinia())
  // Both stores read the current store id from here; without one they
  // no-op, exactly as they do on a page loaded before the store list.
  const stores = useStoresStore()
  stores.stores = [{ store_id: 'str-1', name: 'Acme' }] as never
  stores.currentStoreId = 'str-1'
  stores.loaded = true
  vi.mocked(listStoreSurfaces).mockReset()
  vi.mocked(enrollSurface).mockReset()
  vi.mocked(submitSurface).mockReset()
  vi.mocked(setSurfaceConfig).mockReset()
  vi.mocked(getActivation).mockReset()
  vi.mocked(getActivation).mockResolvedValue({
    store_id: 'str-1',
    ucp_state: 'active',
    capabilities: [],
    entered_state_at: '2026-01-01T00:00:00Z',
    steps: [],
    history: [],
  })
})

// ── The page renders from the registry ───────────────────────────────────

describe('SurfacesSurface', () => {
  it('renders one card per registered surface', async () => {
    vi.mocked(listStoreSurfaces).mockResolvedValue([
      enrollment(directAgent, { state: 'enrolled' }),
      enrollment(exampleMarketplace),
      enrollment(fixtureSurface),
    ])

    const w = await mountPage()
    expect(w.findAll('.surface-card')).toHaveLength(3)
  })

  it('titles each card from its descriptor', async () => {
    vi.mocked(listStoreSurfaces).mockResolvedValue([
      enrollment(directAgent, { state: 'enrolled' }),
      enrollment(exampleMarketplace),
    ])

    const w = await mountPage()
    const names = w.findAll('.surface-name').map((n) => n.text())
    expect(names).toContain('Direct agent access')
    expect(names).toContain('Example marketplace')
  })

  // R19: direct_agent is always present and informational — it exists so
  // "active with no distribution partner" is explicit rather than implied.
  it('renders direct_agent as an always-present informational card', async () => {
    vi.mocked(listStoreSurfaces).mockResolvedValue([
      enrollment(directAgent, { state: 'enrolled' }),
    ])

    const w = await mountPage()
    const card = w.find('.surface-card')
    expect(card.find('.always-on').exists()).toBe(true)
    expect(card.text()).toContain('Any agent that reads your profile can transact')
    // Nothing to enroll in, so no action is offered.
    expect(card.findComponent(SurfaceEnrollmentFlow).exists()).toBe(false)
  })

  // R3, on the page that could most easily contradict it: a store with no
  // surfaces enrolled must not be told it is unfinished.
  it('states activation as a completion, not as a prerequisite', async () => {
    vi.mocked(listStoreSurfaces).mockResolvedValue([enrollment(exampleMarketplace)])

    const w = await mountPage()
    const banner = w.find('.activation')
    expect(banner.text()).toContain('Your store is UCP active')
    expect(banner.classes()).toContain('ok')
  })

  it('shows the blocked reason when the store is not active', async () => {
    vi.mocked(listStoreSurfaces).mockResolvedValue([
      enrollment(exampleMarketplace, { blocked: true, blocked_reason: 'surfaces.blocked.storeNotActive' }),
    ])

    const w = await mountPage()
    expect(w.text()).toContain('needs to be UCP active')
  })
})

// ── The generic component, on the real surfaces ──────────────────────────

describe('SurfaceEnrollmentFlow', () => {
  it('renders prerequisites with reason and remediation', () => {
    const w = mountFlow(
      enrollment(exampleMarketplace, {
        state: 'prerequisites_pending',
        prerequisites: [
          {
            check_id: 'example_return_policy',
            label_key: 'surfaces.example_marketplace.prereq.returnPolicy',
            passed: false,
            reason: 'return policy not yet configured ',
            remediation_key: 'surfaces.example_marketplace.remediation.returnPolicy',
            remediation_url: 'https://example.test/return-policy',
            evaluated_at: '2026-01-01T00:00:00Z',
          },
        ],
      }),
    )

    expect(w.text()).toContain('Return policy configured')
    expect(w.text()).toContain('return policy not yet configured')
    const fix = w.find('.prereq-fix')
    expect(fix.attributes('href')).toBe('https://example.test/return-policy')
  })

  it('renders an artifact with a copyable URL', () => {
    const w = mountFlow(
      enrollment(exampleMarketplace, {
        state: 'configured',
        artifacts: [
          {
            artifact_id: 'example_feed',
            label_key: 'surfaces.example_marketplace.artifact.feed',
            url: 'https://artifacts.sarvex.example.com/tok.xml',
            detail: { product_count: '1840' },
          },
        ],
      }),
    )

    expect(w.text()).toContain('https://artifacts.sarvex.example.com/tok.xml')
    expect(w.text()).toContain('1840')
  })
})

describe('SurfaceConfigForm (rendered from config_schema)', () => {
  it('renders a field per schema entry, by kind', () => {
    const w = mountFlow(enrollment(exampleMarketplace, { state: 'prerequisites_pending' }))
    expect(w.text()).toContain('My return policy is configured')
    expect(w.find('input[type="checkbox"]').exists()).toBe(true)
    expect(w.findAll('textarea')).toHaveLength(2) // string_list + json
  })

  it('saves typed values through the generic store action', async () => {
    vi.mocked(setSurfaceConfig).mockResolvedValue(enrollment(exampleMarketplace, { state: 'configured' }))
    const w = mountFlow(enrollment(exampleMarketplace, { state: 'prerequisites_pending' }))

    await w.find('input[type="checkbox"]').setValue(true)
    await w.findAll('textarea')[0].setValue('SKU-1\nSKU-2')
    await w.find('form').trigger('submit')
    await flushPromises()

    expect(setSurfaceConfig).toHaveBeenCalledWith('str-1', 'example_marketplace', {
      return_policy_configured: 'true',
      excluded_skus: 'SKU-1\nSKU-2',
    })
  })

  it('refuses to save invalid JSON and says why', async () => {
    const w = mountFlow(enrollment(exampleMarketplace, { state: 'prerequisites_pending' }))
    await w.findAll('textarea')[1].setValue('{nope')
    expect(w.text()).toContain('not valid JSON')
    expect(w.find('form button[type="submit"]').attributes('disabled')).toBeDefined()
  })

  it('starts from the stored config', () => {
    const w = mountFlow(
      enrollment(exampleMarketplace, {
        state: 'prerequisites_pending',
        config: { return_policy_configured: 'true', excluded_skus: 'A\nB' },
      }),
    )
    expect((w.find('input[type="checkbox"]').element as HTMLInputElement).checked).toBe(true)
    expect((w.findAll('textarea')[0].element as HTMLTextAreaElement).value).toBe('A\nB')
  })

  // A surface that declares no schema takes free-form config (the fixture
  // does), so no settings section is drawn for it.
  it('draws no settings section for a surface without a schema', () => {
    const w = mountFlow(enrollment(fixtureSurface, { state: 'prerequisites_pending' }))
    expect(w.text()).not.toContain('Settings')
    expect(w.find('form').exists()).toBe(false)
  })
})

describe('example_marketplace copy', () => {
  // Every i18n key the backend descriptor and the generic flow derive for
  // this surface must have words: a missing one would render as its last
  // path segment.
  const keys = [
    'name', 'description',
    'check.example_return_policy', 'check.example_feed_generated', 'check.example_products_eligible',
    'prereq.returnPolicy', 'prereq.feedGenerated', 'prereq.productsEligible',
    'remediation.returnPolicy', 'remediation.feedGenerated', 'remediation.productsEligible',
    'config.returnPolicy.label', 'config.returnPolicy.help',
    'config.excludedSkus.label', 'config.excludedSkus.help',
    'config.notices.label', 'config.notices.help',
    'artifact.feed',
    'artifactDetail.product_count', 'artifactDetail.eligible_count',
    'artifactDetail.excluded_count', 'artifactDetail.blocked_needs_notice_count',
    'step.interestForm', 'step.sandboxValidation',
    'signal.example_first_fetch',
  ]
  it.each(keys)('has copy for surfaces.example_marketplace.%s', (k) => {
    expect(i18n.global.te(`surfaces.example_marketplace.${k}`)).toBe(true)
  })

  it('previews the surface\'s checks in words before enrolling', () => {
    const w = mountFlow(enrollment(exampleMarketplace))
    expect(w.text()).toContain('Return policy confirmed')
    expect(w.text()).not.toContain('example_return_policy')
  })

  it('names the signal being waited on', () => {
    const w = mountFlow(enrollment(exampleMarketplace, { state: 'verifying' }))
    expect(w.text()).toContain('The marketplace to fetch your product file')
  })
})

// ── R20: the fixture surface, with no new frontend code ─────────────────

describe('a surface the frontend has never heard of', () => {
  it('renders its prerequisites, which are nothing like the marketplace\'s', () => {
    const w = mountFlow(
      enrollment(fixtureSurface, {
        state: 'prerequisites_pending',
        prerequisites: [
          {
            check_id: 'fixture_partner_api_key',
            label_key: 'fixture.prereq.apiKey',
            passed: false,
            reason: 'no partner API key configured',
            evaluated_at: '2026-01-01T00:00:00Z',
          },
          {
            check_id: 'fixture_region_supported',
            label_key: 'fixture.prereq.region',
            passed: true,
            evaluated_at: '2026-01-01T00:00:00Z',
          },
        ],
      }),
    )

    expect(w.text()).toContain('no partner API key configured')
    expect(w.findAll('.prereqs li')).toHaveLength(2)
    expect(w.findAll('.prereqs li.failed')).toHaveLength(1)
  })

  // A surface with no artifacts must render no artifact section at all —
  // not an empty one, and certainly not a feed. This is the assertion that
  // catches "the component assumes every surface has a feed URL".
  it('renders no artifact section when the surface generates nothing', () => {
    const w = mountFlow(enrollment(fixtureSurface, { state: 'configured', artifacts: [] }))

    expect(w.text()).not.toContain('What we generate for you')
    expect(w.text()).not.toMatch(/feed/i)
  })

  it('renders its own external step and verification signal', () => {
    const w = mountFlow(enrollment(fixtureSurface, { state: 'verifying' }))

    expect(w.text()).toContain('Steps outside Sarvex')
    expect(w.text()).toContain('Waiting on confirmation')
    // The example marketplace's signal copy must not leak into another surface.
    expect(w.text()).not.toContain('The marketplace to fetch your product file')
  })

  it('reports enrollment when it completes', () => {
    const w = mountFlow(enrollment(fixtureSurface, { state: 'enrolled' }))
    expect(w.text()).toContain("You're live on this surface")
  })

  // The whole flow, driven through the same store and the same component
  // the real surfaces use — no branch anywhere is specific to this surface.
  it('completes its flow end to end through the generic component', async () => {
    vi.mocked(listStoreSurfaces).mockResolvedValue([enrollment(fixtureSurface)])
    const surfaces = useSurfacesStore()
    const activation = useActivationStore()
    await activation.load()
    await surfaces.load()

    vi.mocked(enrollSurface).mockResolvedValue(
      enrollment(fixtureSurface, {
        state: 'configured',
        prerequisites: [
          {
            check_id: 'fixture_partner_api_key',
            label_key: 'fixture.prereq.apiKey',
            passed: true,
            evaluated_at: '2026-01-01T00:00:00Z',
          },
        ],
      }),
    )
    await surfaces.enroll('fixture_marketplace')
    expect(surfaces.byId('fixture_marketplace')?.state).toBe('configured')

    vi.mocked(submitSurface).mockResolvedValue(
      enrollment(fixtureSurface, { state: 'verifying' }),
    )
    await surfaces.submit('fixture_marketplace')
    expect(surfaces.byId('fixture_marketplace')?.state).toBe('verifying')

    vi.mocked(setSurfaceConfig).mockResolvedValue(
      enrollment(fixtureSurface, { state: 'enrolled', enrolled_at: '2026-01-02T00:00:00Z' }),
    )
    await surfaces.saveConfig('fixture_marketplace', { partner_callback_received: 'true' })

    const final = surfaces.byId('fixture_marketplace')
    expect(final?.state).toBe('enrolled')
    expect(surfaces.enrolled.map((e) => e.surface_id)).toEqual(['fixture_marketplace'])
  })

  // The page itself is equally surface-agnostic: an unknown surface gets a
  // card without SurfacesSurface.vue knowing anything about it.
  it('gets a card on the Surfaces page with no routing or view changes', async () => {
    vi.mocked(listStoreSurfaces).mockResolvedValue([enrollment(fixtureSurface)])

    const w = await mountPage()
    const cards = w.findAll('.surface-card')
    expect(cards).toHaveLength(1)
    expect(cards[0].findComponent(SurfaceEnrollmentFlow).exists()).toBe(true)
  })
})
