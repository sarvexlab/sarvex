// Serving-plane checkout negative-path checks: correctness under concurrent
// retries, not throughput. Exercises the documented behaviors in
// internal/planes/serving/checkout_service.go / session_redis.go:
//
//   - Idempotency-Key + same line items -> safe replay (same session id,
//     connector not called twice).
//   - Idempotency-Key + different line items -> 409 conflict
//     (ErrIdempotencyKeyConflict).
//   - Unknown session id on GET/PUT/complete -> 404.
//
// One documented gap this script surfaces rather than asserts on: Complete
// only checks the request body's Idempotency-Key isn't enforced anywhere in
// CheckoutService.Complete (internal/planes/serving/checkout_service.go) —
// unlike Create, a second /complete call on the same session is NOT deduped
// server-side. This script calls complete twice and logs both outcomes
// instead of asserting a specific status, since "correct" behavior here is
// currently undefined by the code, not something this test should pretend
// to verify.
import http from 'k6/http';
import { check } from 'k6';
import { SERVING_BASE_URL } from '../lib/config.js';
import { loadManifest, pick } from '../lib/manifest.js';
import { createCheckoutBody, updateCheckoutBody, completeCheckoutBody, defaultBuyer, defaultAddress, paymentHandlersFromProfile } from '../lib/fixtures.js';

const stores = loadManifest();

export const options = {
  scenarios: {
    negative_paths: {
      executor: 'per-vu-iterations',
      vus: 3,
      iterations: 5,
      maxDuration: '2m',
    },
  },
  thresholds: {
    checks: ['rate>0.9'],
  },
};

function base(store) {
  return `${SERVING_BASE_URL}/${store.platform}/${store.store_id}`;
}

function jsonPost(url, body, headers) {
  return http.post(url, JSON.stringify(body), { headers: Object.assign({ 'Content-Type': 'application/json' }, headers || {}) });
}

export default function () {
  const store = pick(stores);
  const key = `k6-neg-${__VU}-${__ITER}-${Date.now()}`;

  // --- 404s on an unknown session id ---
  check(http.get(`${base(store)}/checkout-sessions/does-not-exist`), {
    'GET unknown session -> 404': (r) => r.status === 404,
  });
  check(http.put(`${base(store)}/checkout-sessions/does-not-exist`, JSON.stringify({}), { headers: { 'Content-Type': 'application/json' } }), {
    'PUT unknown session -> 404': (r) => r.status === 404,
  });
  check(jsonPost(`${base(store)}/checkout-sessions/does-not-exist/complete`, {}), {
    'complete unknown session -> 404': (r) => r.status === 404,
  });

  // Need a real SKU for the idempotency checks below.
  const searchRes = jsonPost(`${base(store)}/catalog/search`, { pagination: { limit: 1 } });
  let sku;
  try {
    sku = JSON.parse(searchRes.body).products[0].id;
  } catch (e) {
    return; // store has no products yet; skip the rest of this iteration
  }

  // --- idempotent replay: same key + same items -> same session id twice ---
  const replayKey = `${key}-replay`;
  const first = jsonPost(`${base(store)}/checkout-sessions`, createCheckoutBody([{ sku, qty: 1 }]), { 'Idempotency-Key': replayKey });
  const second = jsonPost(`${base(store)}/checkout-sessions`, createCheckoutBody([{ sku, qty: 1 }]), { 'Idempotency-Key': replayKey });
  let firstID, secondID;
  try {
    firstID = JSON.parse(first.body).id;
    secondID = JSON.parse(second.body).id;
  } catch (e) {
    // leave undefined; checks below fail explicitly
  }
  check(null, {
    'idempotent replay: both calls 201': () => first.status === 201 && second.status === 201,
    'idempotent replay: same session id returned': () => !!firstID && firstID === secondID,
  });

  // --- idempotency conflict: same key + different items -> 409 ---
  const conflictKey = `${key}-conflict`;
  const c1 = jsonPost(`${base(store)}/checkout-sessions`, createCheckoutBody([{ sku, qty: 1 }]), { 'Idempotency-Key': conflictKey });
  const c2 = jsonPost(`${base(store)}/checkout-sessions`, createCheckoutBody([{ sku, qty: 2 }]), { 'Idempotency-Key': conflictKey });
  check(null, {
    'idempotency conflict: first call 201': () => c1.status === 201,
    'idempotency conflict: second call 409': () => c2.status === 409,
  });

  // --- documented gap: double-complete is not deduped (informational only) ---
  const profileRes = http.get(`${base(store)}/.well-known/ucp`);
  let handlerId;
  try {
    handlerId = paymentHandlersFromProfile(JSON.parse(profileRes.body))[0];
  } catch (e) {
    return;
  }
  if (!handlerId || !firstID) return;

  const updRes = http.put(
    `${base(store)}/checkout-sessions/${firstID}`,
    JSON.stringify(updateCheckoutBody({ buyer: defaultBuyer(key), address: defaultAddress() })),
    { headers: { 'Content-Type': 'application/json' } }
  );
  if (updRes.status !== 200) return;

  const complete1 = jsonPost(`${base(store)}/checkout-sessions/${firstID}/complete`, completeCheckoutBody({ handlerId, checkoutId: firstID }));
  const complete2 = jsonPost(`${base(store)}/checkout-sessions/${firstID}/complete`, completeCheckoutBody({ handlerId, checkoutId: firstID }));
  console.log(`double-complete on ${firstID}: first=${complete1.status} second=${complete2.status} (no dedup guarantee — see file header)`);
}
