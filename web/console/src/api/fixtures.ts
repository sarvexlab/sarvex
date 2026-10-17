// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Mock fixtures for local dev. Owned by the mock adapter — fixtures are
// mutated in place so resolve/create round-trips look real. The shapes are
// the OpenAPI schemas verbatim; flipping VITE_USE_MOCKS=0 swaps to the real
// backend with no consumer changes.

import type { components } from './schema'

type Store = components['schemas']['store']
type Connector = components['schemas']['connector']
type Task = components['schemas']['task']
type Journey = components['schemas']['journey']
type Activation = components['schemas']['activation']
type Presence = components['schemas']['presence']
type ManifestView = components['schemas']['manifest_view']
type Readiness = components['schemas']['readiness']
type ProductSummary = components['schemas']['product_summary']
type ProductDetail = components['schemas']['product_detail']
type Order = components['schemas']['order']
type Account = components['schemas']['account']
type Member = components['schemas']['member']
type Billing = components['schemas']['billing']
type Prefs = components['schemas']['notification_preferences']
type PaymentConfig = components['schemas']['payment_config']
type StoreProfile = components['schemas']['store_profile']
type Health = components['schemas']['health']
type ConsistencyFinding = components['schemas']['consistency_finding']
type BoardCard = components['schemas']['board_card']
type ManualStep = components['schemas']['manual_step']
type AuditEntry = components['schemas']['audit_entry']
type ValidationStatus = components['schemas']['validation_status']
type ConformanceRecord = components['schemas']['conformance_record']
type Alert = components['schemas']['alert']
type HealthHistoryEntry = components['schemas']['health_history_entry']
type CheckoutSession = components['schemas']['checkout_session']
type CheckoutSessionTimelineEntry = components['schemas']['checkout_session_timeline_entry']
type LedgerEntry = components['schemas']['ledger_entry']
type ChannelSurface = components['schemas']['channel_surface']
type ProductDemand = components['schemas']['insights_product_demand']

const NOW = '2026-06-23T10:00:00Z'

export const fixtures = {
  stores: [
    {
      store_id: 'str-acme-au',
      name: 'Acme Tea Co.',
      platform: 'woocommerce',
      domain: 'acmetea.com.au',
      connector_id: null,
      journey_state: 'connected',
      ucp_state: 'connector_linked',
      declared_capabilities: ['catalog', 'cart', 'checkout', 'order'],
    } satisfies Store,
  ] as Store[],

  connectors: [] as Connector[],

  // Tasks load only after a connector exists. Sized to walk all four owner
  // styles + the major states the design covers.
  tasks: [
    {
      task_id: 't-shipping-map',
      title: 'Map 12 shipping classes to Google taxonomy',
      why_it_matters: 'Agents quote shipping per region from this mapping.',
      owner: 'merchant',
      resolution: 'one_click',
      state: 'open',
      blocks_checkout: true,
      source: 'readiness',
      source_ref: null,
      created_at: NOW,
      updated_at: NOW,
    },
    {
      task_id: 't-brand-colour',
      title: 'Pick a primary brand colour',
      why_it_matters: 'Shown in the agent storefront card and checkout.',
      owner: 'merchant',
      resolution: 'guided',
      state: 'open',
      blocks_checkout: false,
      source: 'checklist',
      source_ref: null,
      created_at: NOW,
      updated_at: NOW,
    },
    {
      task_id: 't-return-url',
      title: 'Set a return-policy URL on your storefront',
      why_it_matters: 'Agents link shoppers to it when they ask about returns.',
      owner: 'merchant',
      resolution: 'guided',
      state: 'open',
      blocks_checkout: false,
      source: 'checklist',
      source_ref: null,
      created_at: NOW,
      updated_at: NOW,
    },
    {
      task_id: 't-recon',
      title: 'Reconciling catalog updates',
      why_it_matters: 'Catching the serving cache up after a large sync.',
      owner: 'sarvex',
      resolution: 'automatic',
      state: 'in_progress',
      blocks_checkout: false,
      source: 'monitoring',
      source_ref: null,
      created_at: NOW,
      updated_at: NOW,
    },
    {
      task_id: 't-google-validate',
      title: 'Sandbox validation under review',
      why_it_matters: "An external party is reviewing your submission; we'll email when it clears.",
      owner: 'external',
      resolution: 'external_wait',
      state: 'blocked',
      blocks_checkout: false,
      source: 'validation',
      source_ref: null,
      created_at: NOW,
      updated_at: NOW,
    },
    {
      task_id: 't-handshake',
      title: 'Verified the plugin handshake',
      why_it_matters: 'First plugin signal received and acknowledged.',
      owner: 'sarvex',
      resolution: 'automatic',
      state: 'done',
      blocks_checkout: false,
      source: 'monitoring',
      source_ref: null,
      created_at: NOW,
      updated_at: NOW,
    },
  ] as Task[],

  journey: {
    state: 'connected',
    next_action: { task_id: 't-shipping-map', title: 'Map 12 shipping classes to Google taxonomy' },
    open_task_counts: { merchant: 3, sarvex: 1, external: 1 },
  } as Journey,

  // ── Store UCP activation ───────────────────────────────────────────────
  // The store's own lifecycle. Nothing here mentions a surface.
  activation: {
    store_id: 'str-acme-au',
    ucp_state: 'connector_linked',
    capabilities: ['dev.ucp.shopping.catalog', 'dev.ucp.shopping.checkout'],
    entered_state_at: '2026-01-04T10:00:00Z',
    steps: [
      { state: 'draft', reached: true, reached_at: '2026-01-02T09:00:00Z' },
      {
        state: 'connector_linked',
        reached: true,
        reached_at: '2026-01-04T10:00:00Z',
        blocking: 'activation.blocking.profile',
      },
      { state: 'profile_published', reached: false },
      { state: 'verified', reached: false },
      { state: 'active', reached: false },
    ],
    history: [
      { state: 'draft', entered_at: '2026-01-02T09:00:00Z' },
      { state: 'connector_linked', entered_at: '2026-01-04T10:00:00Z' },
    ],
  } as Activation,

  // ── Store-on-Google ────────────────────────────────────────────────────
  presence: {
    summary:
      "Agents can see 1,840 products, your 3 shipping options, and your 30-day return policy.",
    product_count: 1840,
    capabilities: ['catalog', 'cart', 'checkout', 'order'],
    freshness_declarations: {
      inventory: 'near-real-time',
      pricing: 'near-real-time',
      catalog: 'every 6 hours',
    },
    serving: 'profile_only',
    profile_url: 'https://acmetea.com.au/.well-known/ucp',
    profile_reachable: true,
    published_version: '2026-04-08',
    supported_versions: ['2026-04-08', '2026-08-25'],
  } satisfies Presence as Presence,

  // The OpenAPI declares `manifest` as `type: object` with no schema
  // properties — the actual contents conform to the external ucp.dev profile
  // schema, not Sarvex's own API. The generator collapses that to
  // Record<string, never>, so we cast through unknown to surface real content.
  manifest: {
    manifest: {
      ucp: {
        version: '2026-08-25',
        services: { endpoint: 'https://api.sarvex.example.com/ucp/str-acme-au' },
        merchant: {
          name: 'Acme Tea Co.',
          domain: 'acmetea.com.au',
          locale: 'en-AU',
          currency: 'AUD',
        },
        capabilities: [
          { name: 'catalog',  version: '2026-08-25' },
          { name: 'cart',     version: '1.0' },
          { name: 'checkout', version: '2026-04-08' },
          { name: 'order',    version: '1.0' },
        ],
        keys: [{ kid: 'k1', alg: 'EdDSA', use: 'sig' }],
      },
    } as unknown,
    cache: {
      built_at: '2026-06-23T09:42:11Z',
      ttl_seconds: 3600,
      stale: false,
      source_watermark: '2026-06-23T09:41:08Z',
    },
  } as ManifestView,

  readiness: {
    overall: 74,
    dimensions: {
      completeness: 86,
      consistency: 79,
      attribute_quality: 64,
      delivery_data: 68,
    },
    last_audit_at: '2026-06-22T22:11:00Z',
    product_count: 1840,
  } satisfies Readiness as Readiness,

  products: [
    { sku: 'AT-001', title: 'Smoky Earl Grey 100g', score: 92, has_blocking_issue: false, open_issue_count: 0 },
    { sku: 'AT-002', title: 'Jasmine Pearls 50g', score: 88, has_blocking_issue: false, open_issue_count: 1 },
    { sku: 'AT-003', title: 'Iron Goddess Oolong 100g', score: 71, has_blocking_issue: false, open_issue_count: 2 },
    { sku: 'AT-004', title: 'Genmaicha 200g', score: 64, has_blocking_issue: true,  open_issue_count: 3 },
    { sku: 'AT-005', title: 'Pu-erh Cake 357g', score: 81, has_blocking_issue: false, open_issue_count: 1 },
    { sku: 'AT-006', title: 'Matcha Ceremonial 30g', score: 55, has_blocking_issue: true,  open_issue_count: 4 },
    { sku: 'AT-007', title: 'Lapsang Souchong 100g', score: 90, has_blocking_issue: false, open_issue_count: 0 },
    { sku: 'AT-008', title: 'Silver Needle 50g', score: 77, has_blocking_issue: false, open_issue_count: 1 },
  ] satisfies ProductSummary[] as ProductSummary[],

  productDetails: {
    'AT-004': {
      sku: 'AT-004',
      title: 'Genmaicha 200g',
      score: 64,
      has_blocking_issue: true,
      open_issue_count: 3,
      dimensions: { completeness: 78, consistency: 80, attribute_quality: 45, delivery_data: 52 },
      issues: [
        {
          issue_id: 'iss-1',
          field: 'gtin',
          problem: 'missing',
          blocks_checkout: true,
          revenue_impact_monthly: { amount_minor: 18000, currency: 'AUD' },
        },
        {
          issue_id: 'iss-2',
          field: 'shipping_weight',
          problem: 'missing',
          blocks_checkout: true,
          revenue_impact_monthly: null,
        },
        {
          issue_id: 'iss-3',
          field: 'google_product_category',
          problem: 'non_standard_enum',
          blocks_checkout: false,
          revenue_impact_monthly: null,
        },
      ],
      attribute_mappings: [
        { source_attribute: 'Brand',     catalog_attribute: 'brand',    tier: 'rule',    confidence: 1.0  },
        { source_attribute: 'Title',     catalog_attribute: 'title',    tier: 'rule',    confidence: 1.0  },
        { source_attribute: 'Price',     catalog_attribute: 'price',    tier: 'rule',    confidence: 1.0  },
        { source_attribute: 'Größe',     catalog_attribute: 'size',     tier: 'learned', confidence: 0.91 },
        { source_attribute: 'Material',  catalog_attribute: 'material', tier: 'learned', confidence: 0.62 },
      ],
    } as ProductDetail,
  } as Record<string, ProductDetail>,

  // Pre-live store: empty orders. Once live, this list populates.
  orders: [] as Order[],

  // ── Account ───────────────────────────────────────────────────────────
  account: {
    merchant_id: 'mer-acme-tea',
    name: 'Acme Tea Co.',
    timezone: 'Australia/Sydney',
    business_facts: {
      legal_entity_name: 'Acme Tea Co. Pty Ltd',
      support_email: 'support@acmetea.com.au',
      support_phone: '+61 2 5555 0123',
      fulfillment_regions: ['AU', 'NZ'],
    },
    created_at: '2026-05-01T10:00:00Z',
  } satisfies Account as Account,
  members: [
    { user_id: 'usr-owner-001', email: 'hello@sarvex.example.com', role: 'owner', status: 'active' },
    { user_id: 'usr-rae-001',   email: 'rae@acmetea.com.au',   role: 'staff', status: 'active' },
    { user_id: 'usr-sam-002',   email: 'sam@acmetea.com.au',   role: 'staff', status: 'invited' },
  ] satisfies Member[] as Member[],

  // ── Billing ───────────────────────────────────────────────────────────
  billing: {
    plan: 'concierge',
    status: 'active',
    sku_band: 'up_to_5000',
    usage: { sku_count: 1840, sku_cap: 5000, member_cap: 5 },
  } satisfies Billing as Billing,

  notificationPrefs: {
    email_enabled: true,
    weekly_digest: true,
    alert_email: 'hello@sarvex.example.com',
  } satisfies Prefs as Prefs,

  paymentConfig: {
    store_id: 'str-acme-au',
    handlers: [
      {
        handler_id: 'google_pay',
        name: 'com.google.pay',
        enabled: true,
        configured: false,
        config_status: 'incomplete',
      },
      {
        handler_id: 'stripe_link',
        name: 'com.stripe.link',
        enabled: false,
        configured: false,
        config_status: 'not_started',
      },
    ],
  } satisfies PaymentConfig as PaymentConfig,

  storeProfile: {
    store_id: 'str-acme-au',
    legal_name: 'Acme Tea Co. Pty Ltd',
    support_email: 'support@acmetea.com.au',
    domain: 'https://acmetea.com.au',
    return_policy_url: 'https://acmetea.com.au/returns',
    fulfillment_regions: ['AU', 'NZ'],
    shipping_zones: {
      dom: {
        id: 'dom',
        name: 'Domestic',
        rates: [{ method: 'ground', cost: { amount_minor: 1200, currency: 'AUD' } }],
      },
    },
  } satisfies StoreProfile as StoreProfile,

  // ── Health (pre-live: empty until journey_state === 'live') ───────────
  // One all-clear statement so the Live stage's consistency widget has
  // something to report in mock mode; findings stay empty (nothing drifted).
  health: {
    statements: [
      {
        kind: 'consistency',
        ok: true,
        message: 'Your prices and stock match across your store and what agents see.',
        checked_at: '2026-06-23T09:58:00Z',
      },
    ],
    endpoint_status: {
      status: 'healthy',
      evaluated_at: '2026-06-23T09:58:00Z',
      checks: {
        profile_reachable: { state: 'pass', current_value: 0, last_pass_at: '2026-06-23T09:58:00Z' },
        signature_roundtrip: { state: 'pass', current_value: 0.0, last_pass_at: '2026-06-23T09:58:00Z' },
        negotiation_handshake: { state: 'pass', current_value: 0.0, last_pass_at: '2026-06-23T09:58:00Z' },
        catalog_lookup: { state: 'pass', current_value: 0, last_pass_at: '2026-06-23T09:58:00Z' },
      },
    },
    active_alerts: [],
  } satisfies Health as Health,

  consistency: [] as ConsistencyFinding[],

  alerts: [] as Alert[],
  healthHistory: [] as HealthHistoryEntry[],

  // ── Operator (the Board) ──────────────────────────────────────────────
  // Cards span the journey so the kanban has something in every column.
  merchants: [
    { merchant_id: 'mer-acme-tea', name: 'Acme Tea Co.', timezone: 'Australia/Sydney', created_at: '2026-05-01T10:00:00Z' },
    { merchant_id: 'mer-bayside',  name: 'Bayside Trail',  timezone: 'Australia/Sydney', created_at: '2026-05-10T10:00:00Z' },
    { merchant_id: 'mer-harbor',   name: 'Harbor & Pine',  timezone: 'Pacific/Auckland', created_at: '2026-05-12T10:00:00Z' },
    { merchant_id: 'mer-tide',     name: 'Tidewater Goods', timezone: 'Australia/Brisbane', created_at: '2026-05-18T10:00:00Z' },
    { merchant_id: 'mer-fern',     name: 'Fern Lane Co.',  timezone: 'Australia/Sydney', created_at: '2026-05-20T10:00:00Z' },
    { merchant_id: 'mer-wattle',   name: 'Wattle Roastery', timezone: 'Australia/Melbourne', created_at: '2026-04-22T10:00:00Z' },
  ] as Account[],

  board: [
    { merchant_id: 'mer-acme-tea', name: 'Acme Tea Co.',     state: 'connected',          days_in_state: 1, open_task_counts: { merchant: 3, sarvex: 1, external: 1 }, blocked: false, stale: false },
    { merchant_id: 'mer-fern',     name: 'Fern Lane Co.',    state: 'connected',          days_in_state: 0, open_task_counts: { merchant: 0, sarvex: 0, external: 0 }, blocked: false, stale: false },
    { merchant_id: 'mer-bayside',  name: 'Bayside Trail',    state: 'synced',             days_in_state: 2, open_task_counts: { merchant: 4, sarvex: 0, external: 0 }, blocked: false, stale: false },
    { merchant_id: 'mer-tide',     name: 'Tidewater Goods',  state: 'ready',              days_in_state: 1, open_task_counts: { merchant: 2, sarvex: 1, external: 0 }, blocked: false, stale: false },
    { merchant_id: 'mer-harbor',   name: 'Harbor & Pine',    state: 'checklist_complete', days_in_state: 3, open_task_counts: { merchant: 0, sarvex: 0, external: 1 }, blocked: false, stale: true },
    { merchant_id: 'mer-wattle',   name: 'Wattle Roastery',  state: 'sandbox_validated',  days_in_state: 5, open_task_counts: { merchant: 1, sarvex: 0, external: 0 }, blocked: true,  stale: false },
  ] as BoardCard[],

  manualSteps: [
    { manual_step_id: 'ms-1', target_merchant_id: 'mer-bayside', description: 'Manually copied shipping table from spreadsheet → Bayside payload', disposition: 'templatise', created_at: '2026-06-21T11:14:00Z' },
    { manual_step_id: 'ms-2', target_merchant_id: 'mer-tide',    description: 'Verified Stripe handler config for Tidewater on their behalf',     disposition: 'automate',    created_at: '2026-06-22T09:02:00Z' },
  ] as ManualStep[],

  // audit_entry.detail is `type: object` with no schema in the OpenAPI
  // (the per-action shape varies), so the generated type collapses it to
  // Record<string, never>. Cast through unknown to surface real content.
  audit: [
    { entry_id: 'a-1', operator_user_id: 'op-dana-001', target_merchant_id: 'mer-bayside', action: 'remediation.apply', detail: { task_id: 't-shipping-map' } as unknown, occurred_at: '2026-06-22T09:11:00Z' },
    { entry_id: 'a-2', operator_user_id: 'op-dana-001', target_merchant_id: 'mer-tide',    action: 'task.reassign',     detail: { task_id: 't-payment', to: 'sarvex' } as unknown, occurred_at: '2026-06-22T10:33:00Z' },
    { entry_id: 'a-3', operator_user_id: 'op-dana-001', target_merchant_id: 'mer-harbor',  action: 'task.mark_done',    detail: { task_id: 't-validation' } as unknown, occurred_at: '2026-06-22T15:02:00Z' },
  ] as AuditEntry[],

  // ── Validation status + runs ──────────────────────────────────────────
  validationStatus: {
    pre_validation: 'failing',
    sandbox: 'not_submitted',
    production: 'not_submitted',
    target_spec_version: 'ucp-2026-05',
    last_run_at: '2026-06-22T17:42:00Z',
    open_failures: [
      {
        requirement: 'checkout.session.create',
        spec_section: '§4.2.1',
        summary: 'Session creation must accept a `buyer_consent` capability extension.',
      },
      {
        requirement: 'catalog.product.list',
        spec_section: '§2.3',
        summary: 'Products missing `shipping_weight` for AU region.',
      },
    ],
  } as ValidationStatus,

  validationRuns: [
    { run_id: 'run-007', ran_at: '2026-06-22T17:42:00Z', spec_version: 'ucp-2026-05', result: 'fail', passed: 142, failed: 2, warnings: 1 },
    { run_id: 'run-006', ran_at: '2026-06-21T13:18:00Z', spec_version: 'ucp-2026-05', result: 'fail', passed: 140, failed: 4, warnings: 1 },
    { run_id: 'run-005', ran_at: '2026-06-20T09:02:00Z', spec_version: 'ucp-2026-05', result: 'fail', passed: 137, failed: 7, warnings: 2 },
  ] as ConformanceRecord[],

  // ── Checkout Escalation Console ────────────────────────────────────────
  // co-active: still inside the /live window, so its timestamps are
  // relative to load time rather than the fixed NOW every other fixture
  // uses — /checkout-sessions/live filters by real last_activity_at
  // against the real clock (mocks.ts), not fixture time. co-stalled sits
  // outside the window (escalated_at > 60min ago, matching
  // EscalationStalledThreshold in checkoutescalation/sweep.go) so it only
  // surfaces via the cause filter — exactly the real Live/badge asymmetry
  // the console has to handle.
  checkoutSessions: [
    {
      checkout_id: 'co-active-1',
      store_id: 'str-acme-au',
      surface: 'ucp_checkout',
      agent_platform: 'gemini-app',
      state: 'cart_active',
      current_stage: 'cart_review',
      started_at: new Date(Date.now() - 4 * 60_000).toISOString(),
      last_activity_at: new Date(Date.now() - 50_000).toISOString(),
      terminal_at: null,
      cart_summary: { currency: 'AUD', total_minor: 4250, item_count: 3 },
      escalation: null,
      failure: null,
      diagnosis: null,
      event_count: 4,
      updated_at: '2026-06-23T09:59:10Z',
    },
    {
      checkout_id: 'co-stalled-1',
      store_id: 'str-acme-au',
      surface: 'ucp_checkout',
      agent_platform: 'gemini-app',
      state: 'escalated',
      current_stage: 'payment_handoff',
      started_at: '2026-06-23T08:10:00Z',
      last_activity_at: '2026-06-23T08:22:00Z',
      terminal_at: null,
      cart_summary: { currency: 'AUD', total_minor: 12800, item_count: 2 },
      escalation: {
        reason: 'Buyer handed off to hosted payment page for 3DS challenge.',
        escalated_at: '2026-06-23T08:22:00Z',
        continue_url_issued: true,
        returned_at: null,
        resolved: false,
      },
      failure: null,
      diagnosis: {
        stage_reached: 'payment_handoff',
        cause: 'escalation_stalled',
        plain_language_summary:
          'The buyer was sent to a hosted payment page over an hour ago and never came back — the checkout is stuck waiting on them.',
        recommended_action: 'No action needed from you; this resolves itself if the buyer returns, or times out.',
        evidence: [3, 4],
      },
      event_count: 4,
      updated_at: '2026-06-23T08:22:00Z',
    },
    {
      checkout_id: 'co-failed-1',
      store_id: 'str-acme-au',
      surface: 'ucp_checkout',
      agent_platform: null,
      state: 'failed',
      current_stage: 'order_creation',
      started_at: '2026-06-22T14:02:00Z',
      last_activity_at: '2026-06-22T14:03:40Z',
      terminal_at: '2026-06-22T14:03:40Z',
      cart_summary: { currency: 'AUD', total_minor: 7600, item_count: 1 },
      escalation: null,
      failure: { code: 'ORDER_CREATION_FAILED', cause: 'order_creation_failed', stage: 'order_creation' },
      diagnosis: {
        stage_reached: 'order_creation',
        cause: 'order_creation_failed',
        plain_language_summary: 'The order could not be created in WooCommerce — the store rejected the request.',
        recommended_action: 'Check the WooCommerce order log for the rejected request around 14:03.',
        evidence: [5, 6],
      },
      event_count: 6,
      updated_at: '2026-06-22T14:03:40Z',
    },
    {
      checkout_id: 'co-completed-1',
      store_id: 'str-acme-au',
      surface: 'ucp_checkout',
      agent_platform: 'gemini-app',
      state: 'completed',
      current_stage: 'complete',
      started_at: '2026-06-22T11:00:00Z',
      last_activity_at: '2026-06-22T11:04:20Z',
      terminal_at: '2026-06-22T11:04:20Z',
      cart_summary: { currency: 'AUD', total_minor: 3100, item_count: 2 },
      escalation: null,
      failure: null,
      diagnosis: null,
      event_count: 5,
      updated_at: '2026-06-22T11:04:20Z',
    },
  ] as CheckoutSession[],

  checkoutSessionTimelines: {
    'co-active-1': [
      { seq: 1, occurred_at: '2026-06-23T09:56:00Z', event_type: 'session.created', stage: 'initiated', payload: { agent_platform: 'gemini-app' } },
      { seq: 2, occurred_at: '2026-06-23T09:57:30Z', event_type: 'cart.item_added', stage: 'cart_active', payload: { sku: 'TEA-001', qty: 2 } },
      { seq: 3, occurred_at: '2026-06-23T09:58:45Z', event_type: 'cart.item_added', stage: 'cart_active', payload: { sku: 'TEA-014', qty: 1 } },
      { seq: 4, occurred_at: '2026-06-23T09:59:10Z', event_type: 'cart.priced', stage: 'cart_review', payload: { total_minor: 4250, currency: 'AUD' } },
    ],
    'co-stalled-1': [
      { seq: 1, occurred_at: '2026-06-23T08:10:00Z', event_type: 'session.created', stage: 'initiated', payload: {} },
      { seq: 2, occurred_at: '2026-06-23T08:15:00Z', event_type: 'cart.priced', stage: 'cart_review', payload: { total_minor: 12800, currency: 'AUD' } },
      { seq: 3, occurred_at: '2026-06-23T08:21:30Z', event_type: 'payment.challenge_required', stage: 'payment_handoff', payload: { handler_id: 'stripe-3ds' } },
      { seq: 4, occurred_at: '2026-06-23T08:22:00Z', event_type: 'session.escalated', stage: 'payment_handoff', payload: { continue_url_issued: true } },
    ],
    'co-failed-1': [
      { seq: 1, occurred_at: '2026-06-22T14:02:00Z', event_type: 'session.created', stage: 'initiated', payload: {} },
      { seq: 2, occurred_at: '2026-06-22T14:02:20Z', event_type: 'cart.priced', stage: 'cart_review', payload: { total_minor: 7600, currency: 'AUD' } },
      { seq: 3, occurred_at: '2026-06-22T14:02:50Z', event_type: 'payment.authorized', stage: 'awaiting_payment', payload: { handler_id: 'stripe' } },
      { seq: 4, occurred_at: '2026-06-22T14:03:10Z', event_type: 'order.create_requested', stage: 'order_creation', payload: {} },
      { seq: 5, occurred_at: '2026-06-22T14:03:38Z', event_type: 'order.create_rejected', stage: 'order_creation', payload: { code: 'woocommerce_rest_cannot_create_order' } },
      { seq: 6, occurred_at: '2026-06-22T14:03:40Z', event_type: 'session.failed', stage: 'order_creation', payload: { cause: 'order_creation_failed' } },
    ],
    'co-completed-1': [
      { seq: 1, occurred_at: '2026-06-22T11:00:00Z', event_type: 'session.created', stage: 'initiated', payload: {} },
      { seq: 2, occurred_at: '2026-06-22T11:01:10Z', event_type: 'cart.priced', stage: 'cart_review', payload: { total_minor: 3100, currency: 'AUD' } },
      { seq: 3, occurred_at: '2026-06-22T11:02:40Z', event_type: 'payment.authorized', stage: 'awaiting_payment', payload: { handler_id: 'google_pay' } },
      { seq: 4, occurred_at: '2026-06-22T11:03:50Z', event_type: 'order.created', stage: 'order_creation', payload: { order_id: 'ord-88213' } },
      { seq: 5, occurred_at: '2026-06-22T11:04:20Z', event_type: 'session.completed', stage: 'complete', payload: {} },
    ],
  } as Record<string, CheckoutSessionTimelineEntry[]>,

  // ── Lost Revenue Ledger ──────────────────────────────────────────────
  // Dated relative to real "now" (like co-active-1 above) rather than the
  // fixed NOW constant, so the ledger's default month selector shows data
  // whenever the console is actually opened. Two entries are pushed a
  // month back to give the summary's prior_month_delta_minor something
  // real to show. One AUD entry is deliberately unpriced (revenue_unknown)
  // and one entry uses USD so the multi-currency grouping has something to
  // group.
  ledgerEntries: (() => {
    const now = new Date()
    const thisMonth = (day: number, hour = 10) =>
      new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), day, hour)).toISOString()
    const lastMonth = (day: number, hour = 10) =>
      new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() - 1, day, hour)).toISOString()

    const entries: LedgerEntry[] = [
      {
        entry_id: 'led-1',
        occurred_at: thisMonth(3),
        checkout_id: 'co-failed-1',
        store_id: 'str-acme-au',
        surface: 'ucp_checkout',
        stage: 'order_creation',
        failure_code: 'ORDER_CREATION_FAILED',
        cause: 'platform_error',
        recommended_action: 'Check the WooCommerce order log for the rejected request around 14:03.',
        lost_revenue_minor: 7600,
        currency: 'AUD',
        revenue_unknown: false,
        status: 'open',
        status_changed_at: thisMonth(3),
        created_at: thisMonth(3),
      },
      {
        entry_id: 'led-2',
        occurred_at: thisMonth(5),
        checkout_id: 'co-ledger-2',
        store_id: 'str-acme-au',
        surface: 'ucp_checkout',
        stage: 'payment_handoff',
        failure_code: 'PAYMENT_HANDLER_DECLINED',
        cause: 'payment_handler',
        recommended_action: 'Check your Stripe handler config — declines are up for this handler.',
        lost_revenue_minor: 12800,
        currency: 'AUD',
        revenue_unknown: false,
        status: 'open',
        status_changed_at: thisMonth(5),
        created_at: thisMonth(5),
      },
      {
        entry_id: 'led-3',
        occurred_at: thisMonth(8),
        checkout_id: 'co-ledger-3',
        store_id: 'str-acme-au',
        surface: 'ucp_checkout',
        stage: 'cart_review',
        failure_code: 'ABANDONED',
        cause: 'buyer_abandoned',
        recommended_action: 'No action needed — the buyer left on their own.',
        lost_revenue_minor: 4250,
        currency: 'AUD',
        revenue_unknown: false,
        status: 'acknowledged',
        status_changed_at: thisMonth(9),
        created_at: thisMonth(8),
      },
      {
        entry_id: 'led-4',
        occurred_at: thisMonth(12),
        checkout_id: 'co-ledger-4',
        store_id: 'str-acme-au',
        surface: 'ucp_checkout',
        stage: 'cart_review',
        failure_code: 'ABANDONED',
        cause: 'buyer_abandoned',
        recommended_action: 'No action needed — the buyer left on their own.',
        lost_revenue_minor: 0,
        currency: 'AUD',
        revenue_unknown: true,
        status: 'open',
        status_changed_at: thisMonth(12),
        created_at: thisMonth(12),
      },
      {
        entry_id: 'led-5',
        occurred_at: thisMonth(14),
        checkout_id: 'co-ledger-5',
        store_id: 'str-acme-au',
        surface: 'ucp_checkout',
        stage: 'catalog_sync',
        failure_code: 'PROFILE_STALE',
        cause: 'sync_freshness',
        recommended_action: 'Your catalog sync is behind — trigger a resync from Your Store on Google.',
        lost_revenue_minor: 2100,
        currency: 'USD',
        revenue_unknown: false,
        status: 'resolved',
        status_changed_at: thisMonth(15),
        created_at: thisMonth(14),
      },
      {
        entry_id: 'led-6',
        occurred_at: lastMonth(10),
        checkout_id: 'co-ledger-6',
        store_id: 'str-acme-au',
        surface: 'ucp_checkout',
        stage: 'payment_handoff',
        failure_code: 'PAYMENT_HANDLER_DECLINED',
        cause: 'payment_handler',
        recommended_action: 'Check your Stripe handler config — declines are up for this handler.',
        lost_revenue_minor: 9000,
        currency: 'AUD',
        revenue_unknown: false,
        status: 'resolved',
        status_changed_at: lastMonth(11),
        created_at: lastMonth(10),
      },
    ]
    return entries
  })(),

  // Channel Dashboard raw daily counts — the shape mocks.ts's channel
  // handlers aggregate/filter from, mirroring how
  // internal/planes/control/channel's DailyAggregate documents get read
  // (one row per store/surface/day; checkouts_initiated always equals
  // sessions by construction, so it's derived at read time, not stored
  // here — see mocks.ts's toChannelDailyView). 120 days back covers
  // this_month/last_month/last_90_days and most custom ranges. Every 11th
  // day carries some sessions_inferred so the funnel's R7 qualifier has
  // something real to key off in mock mode; the store's other surface
  // (google_gemini) has a visibly different rate so the surface filter
  // (R3) is obviously doing something when exercised.
  channelDaily: (() => {
    const now = new Date()
    const surfaces: ChannelSurface[] = ['google_ai_mode', 'google_gemini']
    const rows: Array<{
      date: string
      surface: ChannelSurface
      store_id: string
      sessions: number
      sessions_inferred: number
      sessions_reached_cart: number
      sessions_reached_checkout: number
      checkouts_completed: number
      revenue_minor: number
      currency: string
      discovery_requests: number
    }> = []
    for (let i = 0; i < 120; i++) {
      const d = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() - i))
      const date = d.toISOString().slice(0, 10)
      const weekday = d.getUTCDay()
      const weekendDip = weekday === 0 || weekday === 6 ? 0.6 : 1
      for (const surface of surfaces) {
        const base = surface === 'google_ai_mode' ? 34 : 19
        const sessions = Math.max(0, Math.round((base + (i % 5) * 2) * weekendDip))
        const cart = Math.round(sessions * (surface === 'google_ai_mode' ? 0.62 : 0.55))
        const checkout = Math.round(cart * 0.71)
        const completed = Math.round(checkout * 0.83)
        const inferred = i % 11 === 0 ? Math.max(1, Math.round(sessions * 0.15)) : 0
        const revenue = completed * (surface === 'google_ai_mode' ? 4200 : 5100)
        rows.push({
          date,
          surface,
          store_id: 'str-acme-au',
          sessions,
          sessions_inferred: inferred,
          sessions_reached_cart: cart,
          sessions_reached_checkout: checkout,
          checkouts_completed: completed,
          revenue_minor: revenue,
          // Every checkout-lifecycle event this backend consumes today only
          // ever carries "USD" (see channel.go's aggregateCurrency doc
          // comment) — the fixture matches that reality rather than
          // fabricating a second currency to exercise currency_mixed;
          // mocks.ts's channel handlers still compute currency_mixed the
          // same way the real handler does, so a future fixture surface in
          // a different currency would exercise it correctly.
          currency: 'USD',
          discovery_requests: sessions + Math.round(sessions * 1.8),
        })
      }
    }
    return rows
  })(),

  // Web-baseline daily rollup (internal/planes/sync/webbaseline) the
  // /channel/comparison mock reads. The most recent 5 days deliberately sit
  // short of connector_reported confidence (confidence='estimated', i.e.
  // "not fully reported"), so the comparison's real tri-state confidence
  // (connector_reported | partial | unavailable — see mocks.ts) is
  // exercised in mock mode too, not just its happy path — this_month/
  // last_90_days will land on 'partial' (real numbers + a confidence
  // indicator, R11), only a fully-closed last_month sees
  // 'connector_reported'.
  channelWebBaseline: (() => {
    const now = new Date()
    const rows: Array<{
      date: string
      store_id: string
      revenue_minor: number
      orders: number
      currency: string
      confidence: 'connector_reported' | 'estimated' | 'none'
    }> = []
    for (let i = 0; i < 120; i++) {
      const d = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() - i))
      const date = d.toISOString().slice(0, 10)
      rows.push({
        date,
        store_id: 'str-acme-au',
        revenue_minor: Math.round(14_000_000 + (i % 9) * 250_000),
        orders: 260 + (i % 9) * 6,
        currency: 'USD',
        confidence: i < 5 ? 'estimated' : 'connector_reported',
      })
    }
    return rows
  })(),

  // ── Agent Behavior Insights ──────────────────────────────────────────
  // Static, not time-bucketed like channelDaily/channelWebBaseline above —
  // the real aggregates (internal/planes/control/insights) are monthly/daily
  // rollups the mock doesn't need to reproduce exactly to be useful for
  // local dev; these fixtures exist so `make console` has something to
  // render, not to model the backend's fold logic.
  capabilityGaps: [
    {
      capability_id: 'dev.ucp.shopping.fulfillment',
      request_count: 340,
      distinct_agent_count: 28,
      surface_breakdown: { google_ai_mode: 210, google_gemini: 130 },
      first_seen_at: '2026-07-02T09:00:00Z',
      last_seen_at: '2026-08-07T18:00:00Z',
      estimated_impact_minor: 0,
      impact_confidence: 'unmeasurable' as const,
      merchant_supported: false,
      remediation_available: false,
      recommended_action: 'Declare shipping/fulfillment options in your UCP profile.',
    },
    {
      capability_id: 'dev.ucp.shopping.discount',
      request_count: 58,
      distinct_agent_count: 11,
      surface_breakdown: { google_ai_mode: 58 },
      first_seen_at: '2026-07-15T09:00:00Z',
      last_seen_at: '2026-08-06T12:00:00Z',
      estimated_impact_minor: 0,
      impact_confidence: 'unmeasurable' as const,
      merchant_supported: false,
      remediation_available: false,
      recommended_action: 'Support promo-code application in your checkout capability.',
    },
  ],
  productDemand: [
    { product_id: 'SKU-1001', request_count: 412, surface_breakdown: { google_ai_mode: 300, google_gemini: 112 } },
    { product_id: 'SKU-1002', request_count: 187, surface_breakdown: { google_ai_mode: 187 } },
    { product_id: 'SKU-1003', request_count: 64, surface_breakdown: { google_gemini: 64 } },
  ] as ProductDemand[],
  negotiation: {
    attempts: 512,
    successes: 430,
    failures: [
      {
        reason: 'capabilities_incompatible' as const,
        count: 58,
        missing_capability_counts: { 'dev.ucp.shopping.fulfillment': 34, 'dev.ucp.shopping.discount': 24 },
      },
      { reason: 'version_unsupported' as const, count: 24 },
    ],
    payment_handler_mismatch: {
      occurrences: 19,
      offered_handler_counts: { paypal: 14, venmo: 5 },
      merchant_declared_handlers: ['stripe'],
    },
  },
}

/** Promote a freshly-paired connector to healthy after a delay so the
 *  pairing surface can show its state transition without a real backend. */
export function schedulePairingHealth(connectorId: string, delayMs = 1800) {
  setTimeout(() => {
    const c = fixtures.connectors.find((x) => x.connector_id === connectorId)
    if (!c) return
    c.status = 'healthy'
    c.last_signal_at = new Date().toISOString()
    c.last_full_sync_at = new Date().toISOString()
    c.freshness_warning = false
    // Wire the connector to the store so /stores reports needs-connect = false.
    const s = fixtures.stores.find((x) => x.store_id === c.store_id)
    if (s) s.connector_id = c.connector_id ?? null
  }, delayMs)
}
