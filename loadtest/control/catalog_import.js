// Control-plane -> sync-plane fan-out load test — the bridge scenario: none
// of these control-plane endpoints do the expensive work themselves, they
// publish a command/event the sync plane consumes (internal/pkg/ucp/
// event_types.go / command_types.go are the source of truth for topic
// names). Confirmed call sites (internal/planes/control/connector/
// connector_service.go, .../catalog/*):
//
//   POST /connectors/:id/resync         -> control.connector.resync_requested
//                                          -> control.catalog.import (reason=resync)
//                                          full paginated catalog pull + rescoring,
//                                          same path as onboarding's initial import.
//   POST /catalog/audits                -> command.audit_requested (global.request.audit)
//   POST /catalog/remediations/apply     -> command.remediations_applied (global.apply.remediation)
//
// None of these three endpoints are rate-limited server-side — this
// scenario deliberately runs at a controlled, low rate rather than ramping to a breaking point, since an
// uncapped run here is a real "can the console DoS the sync plane" test,
// not just a throughput benchmark.
import http from 'k6/http';
import { check } from 'k6';
import { CONTROL_BASE_URL } from '../lib/config.js';
import { loadManifest, pick } from '../lib/manifest.js';

const stores = loadManifest();

export const options = {
  scenarios: {
    resync_fanout: {
      executor: 'constant-arrival-rate',
      exec: 'resync',
      rate: 2,
      timeUnit: '1s',
      duration: '2m',
      preAllocatedVUs: 10,
      maxVUs: 30,
    },
    audit_fanout: {
      executor: 'constant-arrival-rate',
      exec: 'audit',
      rate: 1,
      timeUnit: '1s',
      duration: '2m',
      preAllocatedVUs: 10,
      maxVUs: 30,
      startTime: '2m10s',
    },
    remediation_apply: {
      executor: 'constant-arrival-rate',
      exec: 'remediate',
      rate: 1,
      timeUnit: '2s',
      duration: '2m',
      preAllocatedVUs: 10,
      maxVUs: 30,
      startTime: '4m20s',
    },
  },
  thresholds: {
    'http_req_duration{name:resync}': ['p(95)<1000'],
    'http_req_duration{name:audit}': ['p(95)<1000'],
    'http_req_duration{name:remediate}': ['p(95)<1000'],
  },
};

function authed(store, name) {
  return { headers: { Authorization: `Bearer ${store.token}` }, tags: { name } };
}

export function resync() {
  const store = pick(stores);
  if (!store.connector_id) return;
  const res = http.post(
    `${CONTROL_BASE_URL}/connectors/${store.connector_id}/resync`,
    null,
    authed(store, 'resync')
  );
  check(res, { 'resync accepted (202) or not-yet-implemented (501)': (r) => r.status === 202 || r.status === 501 });
}

export function audit() {
  const store = pick(stores);
  const res = http.post(
    `${CONTROL_BASE_URL}/stores/${store.store_id}/catalog/audits`,
    null,
    authed(store, 'audit')
  );
  check(res, { 'audit accepted (202)': (r) => r.status === 202 });
}

export function remediate() {
  const store = pick(stores);
  const listRes = http.get(
    `${CONTROL_BASE_URL}/stores/${store.store_id}/catalog/remediations?limit=5`,
    authed(store, 'list_remediations')
  );
  if (listRes.status !== 200) return;
  let ids = [];
  try {
    ids = (JSON.parse(listRes.body).data || []).map((r) => r.remediation_id || r.id).filter(Boolean);
  } catch (e) {
    return;
  }
  if (ids.length === 0) return; // nothing pending to remediate on this store right now

  const res = http.post(
    `${CONTROL_BASE_URL}/stores/${store.store_id}/catalog/remediations/apply`,
    JSON.stringify({ remediation_ids: ids.slice(0, 3) }),
    { headers: Object.assign({ 'Content-Type': 'application/json' }, authed(store, 'remediate').headers), tags: { name: 'remediate' } }
  );
  check(res, { 'remediations apply 200 or 409 (already applied)': (r) => r.status === 200 || r.status === 409 });
}
