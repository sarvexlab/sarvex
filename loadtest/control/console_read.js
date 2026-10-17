// Control-plane console read-path load test — the Tier-1 endpoints
// identified as high-frequency console traffic: hit on every page load
// (account/stores/journey/readiness/products), on every WebSocket
// journey/task push ("thin event, fat pull" — GET /journey and GET
// /tasks/:id re-fetch after every frame).
//
// The control plane has NO rate limiter, so this
// traffic passes straight through to Mongo/Redis; this scenario
// characterizes that unthrottled cost directly.
import http from 'k6/http';
import { check } from 'k6';
import { CONTROL_BASE_URL } from '../lib/config.js';
import { loadManifest, pick } from '../lib/manifest.js';

const stores = loadManifest();

export const options = {
  scenarios: {
    console_read: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '30s', target: 10 },
        { duration: '2m', target: 50 },
        { duration: '2m', target: 100 },
        { duration: '30s', target: 0 },
      ],
    },
  },
  thresholds: {
    'http_req_duration{name:account}': ['p(95)<400'],
    'http_req_duration{name:stores}': ['p(95)<400'],
    'http_req_duration{name:journey}': ['p(95)<400'],
    'http_req_duration{name:tasks}': ['p(95)<500'],
    'http_req_duration{name:readiness}': ['p(95)<500'],
    'http_req_duration{name:products}': ['p(95)<600'],
    http_req_failed: ['rate<0.01'],
  },
};

function authed(store, name) {
  return { headers: { Authorization: `Bearer ${store.token}` }, tags: { name } };
}

export default function () {
  const store = pick(stores);

  http.get(`${CONTROL_BASE_URL}/account`, authed(store, 'account'));
  http.get(`${CONTROL_BASE_URL}/stores`, authed(store, 'stores'));
  http.get(`${CONTROL_BASE_URL}/connectors`, authed(store, 'connectors'));

  check(http.get(`${CONTROL_BASE_URL}/stores/${store.store_id}/journey`, authed(store, 'journey')), {
    'journey 200': (r) => r.status === 200,
  });

  check(http.get(`${CONTROL_BASE_URL}/tasks?limit=20`, authed(store, 'tasks')), {
    'tasks 200': (r) => r.status === 200,
  });

  check(
    http.get(`${CONTROL_BASE_URL}/stores/${store.store_id}/catalog/readiness`, authed(store, 'readiness')),
    { 'readiness 200': (r) => r.status === 200 }
  );

  check(
    http.get(`${CONTROL_BASE_URL}/stores/${store.store_id}/catalog/products?limit=20`, authed(store, 'products')),
    { 'products 200': (r) => r.status === 200 }
  );

  check(http.get(`${CONTROL_BASE_URL}/stores/${store.store_id}/health`, authed(store, 'health')), {
    'health 200': (r) => r.status === 200,
  });
}
