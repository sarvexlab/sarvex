// Serving-plane smoke test: 1-2 VUs, 30s, correctness gate before running
// catalog_read.js/checkout_flow.js at load. Confirms the seeded stores'
// cache is actually populated (these endpoints read Redis only — see
// internal/planes/serving/api/handler.go and sarvex-v1-architecture.md A10 — and
// 404 on an unpopulated snapshot rather than falling back to a live call).
import http from 'k6/http';
import { check } from 'k6';
import { SERVING_BASE_URL } from '../lib/config.js';
import { loadManifest, pick } from '../lib/manifest.js';

const stores = loadManifest();

export const options = {
  scenarios: {
    smoke: {
      executor: 'constant-vus',
      vus: 2,
      duration: '30s',
    },
  },
  thresholds: {
    http_req_failed: ['rate==0'],
    checks: ['rate==1'],
  },
};

function base(store) {
  return `${SERVING_BASE_URL}/${store.platform}/${store.store_id}`;
}

export default function () {
  const store = pick(stores);

  const profileRes = http.get(`${base(store)}/.well-known/ucp`, { tags: { name: 'well_known_ucp' } });
  check(profileRes, {
    'well-known/ucp is 200': (r) => r.status === 200,
    'well-known/ucp has ucp block': (r) => {
      try {
        return !!JSON.parse(r.body).ucp;
      } catch (e) {
        return false;
      }
    },
  });

  const searchRes = http.post(
    `${base(store)}/catalog/search`,
    JSON.stringify({ pagination: { limit: 5 } }),
    { headers: { 'Content-Type': 'application/json' }, tags: { name: 'catalog_search' } },
  );
  const searchOk = check(searchRes, {
    'catalog/search is 200': (r) => r.status === 200,
    'catalog/search returns products[]': (r) => {
      try {
        return Array.isArray(JSON.parse(r.body).products);
      } catch (e) {
        return false;
      }
    },
  });

  if (searchOk) {
    const body = JSON.parse(searchRes.body);
    if (body.products && body.products.length > 0) {
      const sku = body.products[0].id;
      const itemRes = http.get(`${base(store)}/catalog/${encodeURIComponent(sku)}`, {
        tags: { name: 'catalog_lookup' },
      });
      check(itemRes, {
        'catalog/:itemId is 200': (r) => r.status === 200,
        'catalog/:itemId returns the requested sku': (r) => {
          try {
            return JSON.parse(r.body).sku === sku;
          } catch (e) {
            return false;
          }
        },
      });
    }
  }
}
