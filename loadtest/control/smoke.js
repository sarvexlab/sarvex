// Control-plane smoke test: 1-2 VUs, 30s, correctness gate before running
// console_read.js at load. Hits the Tier-1 endpoints every open console tab
// loads on every page (internal/planes/control/api/handler.go:SetupRoutes).
import http from 'k6/http';
import { check } from 'k6';
import { CONTROL_BASE_URL } from '../lib/config.js';
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

function authed(store) {
  return { headers: { Authorization: `Bearer ${store.token}` } };
}

export default function () {
  const store = pick(stores);
  const params = authed(store);

  check(http.get(`${CONTROL_BASE_URL}/account`, params), { 'account 200': (r) => r.status === 200 });
  check(http.get(`${CONTROL_BASE_URL}/stores`, params), { 'stores 200': (r) => r.status === 200 });
  check(http.get(`${CONTROL_BASE_URL}/stores/${store.store_id}/journey`, params), {
    'journey 200': (r) => r.status === 200,
  });
  check(http.get(`${CONTROL_BASE_URL}/tasks?limit=20`, params), { 'tasks 200': (r) => r.status === 200 });
  check(http.get(`${CONTROL_BASE_URL}/stores/${store.store_id}/catalog/readiness`, params), {
    'readiness 200': (r) => r.status === 200,
  });
  check(http.get(`${CONTROL_BASE_URL}/stores/${store.store_id}/catalog/products?limit=20`, params), {
    'products 200': (r) => r.status === 200,
  });
}
