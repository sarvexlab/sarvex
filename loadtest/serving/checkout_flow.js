// Serving-plane checkout load test: the money path (architecture doc Risk
// R1 — "checkout depends on a third party's live server"). Every
// Create/Update/Complete call is a live synchronous round-trip through the
// sync-plane connector to the merchant backend (cmd/sim serve in dev) — this
// scenario load-tests that dependency's latency/error budget as much as
// Sarvex's own code.
//
// Request shapes mirror internal/planes/sync/sim.Agent exactly (the repo's
// own reference UCP shopping-agent simulator) — see loadtest/lib/fixtures.js.
//
// Freshness guard: Complete 409s if the serving cache's watermark is more
// than 5 minutes stale (internal/planes/serving — freshness guard tied to
// CacheWatermark). This scenario's total duration is kept under that
// window by design; a longer soak run needs a concurrent freshness
// keepalive (e.g. loadtest/control/catalog_import.js's resync scenario,
// which triggers a cache rebuild) — see loadtest/README.md.
import http from 'k6/http';
import { check, group } from 'k6';
import { Trend, Counter } from 'k6/metrics';
import { SERVING_BASE_URL } from '../lib/config.js';
import { loadManifest, pick } from '../lib/manifest.js';
import { createCheckoutBody, updateCheckoutBody, completeCheckoutBody, defaultBuyer, defaultAddress, paymentHandlersFromProfile } from '../lib/fixtures.js';

const stores = loadManifest();

export const checkoutCompletions = new Counter('checkout_completions');
export const checkoutFullFlowDuration = new Trend('checkout_full_flow_duration');

export const options = {
  scenarios: {
    checkout_flow: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '30s', target: 5 },
        { duration: '1m', target: 5 },
        { duration: '1m', target: 20 },
        { duration: '1m', target: 20 },
        { duration: '30s', target: 0 },
      ],
    },
  },
  thresholds: {
    'http_req_duration{name:checkout_create}': ['p(95)<2000'],
    'http_req_duration{name:checkout_update}': ['p(95)<2000'],
    'http_req_duration{name:checkout_complete}': ['p(95)<2500'],
    checkout_completions: ['count>0'],
  },
};

function base(store) {
  return `${SERVING_BASE_URL}/${store.platform}/${store.store_id}`;
}

export default function () {
  const store = pick(stores);
  const iterKey = `k6-${__VU}-${__ITER}-${Date.now()}`;
  const startedAt = Date.now();

  // Step 0: browse the catalog for a real SKU and the store's negotiated
  // payment handlers, exactly as a real agent would before checking out.
  const profileRes = http.get(`${base(store)}/.well-known/ucp`, { tags: { name: 'well_known_ucp' } });
  if (profileRes.status !== 200) return;
  let handlerId;
  try {
    handlerId = paymentHandlersFromProfile(JSON.parse(profileRes.body))[0];
  } catch (e) {
    return;
  }
  if (!handlerId) return;

  const searchRes = http.post(
    `${base(store)}/catalog/search`,
    JSON.stringify({ pagination: { limit: 5 } }),
    { headers: { 'Content-Type': 'application/json' }, tags: { name: 'catalog_search' } },
  );
  if (searchRes.status !== 200) return;
  let sku;
  try {
    const products = JSON.parse(searchRes.body).products;
    sku = products && products[0] && products[0].id;
  } catch (e) {
    return;
  }
  if (!sku) return;

  let sessionId;
  group('checkout_create', function () {
    const res = http.post(
      `${base(store)}/checkout-sessions`,
      JSON.stringify(createCheckoutBody([{ sku, qty: 1 }])),
      {
        headers: { 'Content-Type': 'application/json', 'Idempotency-Key': `${iterKey}-create` },
        tags: { name: 'checkout_create' },
      }
    );
    const ok = check(res, { 'create is 201': (r) => r.status === 201 });
    if (ok) {
      try {
        sessionId = JSON.parse(res.body).id;
      } catch (e) {
        // leave sessionId undefined; subsequent steps skip
      }
    }
  });
  if (!sessionId) return;

  let readyForComplete = false;
  group('checkout_update', function () {
    const res = http.put(
      `${base(store)}/checkout-sessions/${sessionId}`,
      JSON.stringify(updateCheckoutBody({ buyer: defaultBuyer(iterKey), address: defaultAddress() })),
      { headers: { 'Content-Type': 'application/json' }, tags: { name: 'checkout_update' } }
    );
    readyForComplete = check(res, { 'update is 200': (r) => r.status === 200 });
  });
  if (!readyForComplete) return;

  group('checkout_complete', function () {
    const res = http.post(
      `${base(store)}/checkout-sessions/${sessionId}/complete`,
      JSON.stringify(completeCheckoutBody({ handlerId, checkoutId: sessionId })),
      {
        headers: { 'Content-Type': 'application/json', 'Idempotency-Key': `${iterKey}-complete` },
        tags: { name: 'checkout_complete' },
      }
    );
    const ok = check(res, {
      'complete is 200': (r) => r.status === 200,
      'complete produced an order or an escalation, not a silent no-op': (r) => {
        try {
          const body = JSON.parse(r.body);
          return !!body.order || !!body.continue_url;
        } catch (e) {
          return false;
        }
      },
    });
    if (ok) {
      checkoutCompletions.add(1);
      checkoutFullFlowDuration.add(Date.now() - startedAt);
    }
  });
}
