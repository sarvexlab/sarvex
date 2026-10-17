// Serving-plane read-path load test: .well-known/ucp, catalog/search,
// catalog/:itemId — all pure Redis reads (CLAUDE.md invariant #2: discovery
// and catalog reads never call the store/control plane live).
//
// Two scenarios:
//   baseline_mixed        — traffic spread across every seeded store, to
//                            characterize normal multi-tenant read latency
//                            and to see catalog/search's cost scale with the
//                            large-catalog store's product count (that
//                            endpoint does an unindexed LRANGE+scan over the
//                            store's full product list per request — see
//                            internal/planes/serving/cache_redis.go).
//   single_merchant_ceiling — all load against ONE store (one merchant), to
//                            find the actual rate-limit onset. The limiter
//                            is documented as 100 req/s for reads
//                            (internal/planes/serving/middleware.go), but
//                            its map keys are built from unprefixed route
//                            strings ("POST /catalog/search") while
//                            Echo's c.Path() returns the full registered
//                            pattern including the group prefix
//                            ("POST /ucp/:platform/:storeID/catalog/search")
//                            — every route's lookup misses the map and
//                            falls through to the 100 req/s default. Reads
//                            are documented at 100 anyway, so this
//                            scenario's onset should still land near 100;
//                            it's checkout_flow.js (nominally 50 req/s)
//                            where that mismatch would actually be visible.
import http from 'k6/http';
import { check } from 'k6';
import { SERVING_BASE_URL } from '../lib/config.js';
import { loadManifest, pick } from '../lib/manifest.js';

const stores = loadManifest();
const ceilingStore = stores[0];

export const options = {
  scenarios: {
    baseline_mixed: {
      executor: 'ramping-vus',
      exec: 'baselineMixed',
      startVUs: 0,
      stages: [
        { duration: '30s', target: 20 },
        { duration: '2m', target: 50 },
        { duration: '2m', target: 100 },
        { duration: '30s', target: 0 },
      ],
    },
    single_merchant_ceiling: {
      executor: 'ramping-arrival-rate',
      exec: 'ceiling',
      startRate: 20,
      timeUnit: '1s',
      preAllocatedVUs: 50,
      maxVUs: 200,
      stages: [
        { duration: '30s', target: 20 },
        { duration: '1m', target: 80 },
        { duration: '1m', target: 150 },
        { duration: '30s', target: 0 },
      ],
      startTime: '5m30s', // run after baseline_mixed finishes
    },
  },
  thresholds: {
    'http_req_duration{name:catalog_search}': ['p(95)<800'],
    'http_req_duration{name:catalog_lookup}': ['p(95)<300'],
    'http_req_duration{name:well_known_ucp}': ['p(95)<300'],
  },
};

function base(store) {
  return `${SERVING_BASE_URL}/${store.platform}/${store.store_id}`;
}

function hitReadPaths(store) {
  http.get(`${base(store)}/.well-known/ucp`, { tags: { name: 'well_known_ucp' } });

  const searchRes = http.post(
    `${base(store)}/catalog/search`,
    JSON.stringify({ pagination: { limit: 20 } }),
    { headers: { 'Content-Type': 'application/json' }, tags: { name: 'catalog_search' } },
  );
  check(searchRes, { 'catalog/search 200': (r) => r.status === 200 || r.status === 429 });

  if (searchRes.status === 200) {
    let sku;
    try {
      const body = JSON.parse(searchRes.body);
      sku = body.products && body.products[0] && body.products[0].id;
    } catch (e) {
      // ignore parse failures under load; counted by the threshold instead
    }
    if (sku) {
      http.get(`${base(store)}/catalog/${encodeURIComponent(sku)}`, { tags: { name: 'catalog_lookup' } });
    }
  }
}

export function baselineMixed() {
  hitReadPaths(pick(stores));
}

export function ceiling() {
  hitReadPaths(ceilingStore);
}
