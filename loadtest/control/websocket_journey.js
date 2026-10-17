// Control-plane WebSocket scale test — GET /stores/:id/journey/stream
// (internal/planes/control/api/realtime.go). Auth travels as a
// Sec-WebSocket-Protocol entry ("bearer, <token>"), not an Authorization
// header, since browsers can't set custom headers on the WS handshake
// (internal/planes/control/api/middleware.go: wsSubprotocolToken).
//
// Two scenarios running concurrently:
//   connections — opens N long-lived connections spread across the seeded
//                 stores and holds them, counting inbound frames.
//   triggers    — periodically resolves an open task on one of those
//                 stores (POST /tasks/:id/resolve), which publishes
//                 control.task.updated -> Hub.Broadcast -> a
//                 journey_stream_message frame to every client watching
//                 that store (internal/planes/control/realtime/
//                 subscriber.go HandleTaskUpdated). This is what actually
//                 exercises Hub fan-out rather than just connection count.
//
// Caveat (see loadtest/README.md): the Hub is in-memory and single-instance
// — this only measures one control-plane process's fan-out. Multi-instance
// correctness depends on the event-bus-backed Subscriber, which this test
// cannot exercise from a single running instance.
import ws from 'k6/ws';
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Trend } from 'k6/metrics';
import { CONTROL_BASE_URL, DASHBOARD_ORIGIN } from '../lib/config.js';
import { loadManifest, pick } from '../lib/manifest.js';

const stores = loadManifest();

export const wsConnectFailures = new Counter('ws_connect_failures');
export const wsMessagesReceived = new Counter('ws_messages_received');
export const wsConnectDuration = new Trend('ws_connect_duration');

const HOLD_SECONDS = Number(__ENV.WS_HOLD_SECONDS || 90);
const CONNECT_VUS = Number(__ENV.WS_CONNECT_VUS || 20);

export const options = {
  scenarios: {
    connections: {
      executor: 'per-vu-iterations',
      exec: 'connect',
      vus: CONNECT_VUS,
      iterations: 1,
      maxDuration: `${HOLD_SECONDS + 30}s`,
    },
    triggers: {
      executor: 'constant-arrival-rate',
      exec: 'trigger',
      rate: 1,
      timeUnit: '10s',
      duration: `${HOLD_SECONDS}s`,
      preAllocatedVUs: 3,
      maxVUs: 5,
      startTime: '5s', // let connections establish first
    },
  },
  thresholds: {
    ws_connect_failures: ['count==0'],
  },
};

export function connect() {
  const store = pick(stores);
  const url = `${CONTROL_BASE_URL.replace(/^http/, 'ws')}/stores/${store.store_id}/journey/stream`;
  const start = Date.now();

  const res = ws.connect(
    url,
    { headers: { 'Sec-WebSocket-Protocol': `bearer, ${store.token}`, Origin: DASHBOARD_ORIGIN } },
    function (socket) {
      socket.on('open', function () {
        wsConnectDuration.add(Date.now() - start);
      });
      socket.on('message', function () {
        wsMessagesReceived.add(1);
      });
      socket.on('error', function () {
        wsConnectFailures.add(1);
      });
      socket.setTimeout(function () {
        socket.close();
      }, HOLD_SECONDS * 1000);
    },
  );

  check(res, { 'ws handshake succeeded': (r) => r && r.status === 101 });
  if (!res || res.status !== 101) wsConnectFailures.add(1);
}

export function trigger() {
  const store = pick(stores);
  const headers = { Authorization: `Bearer ${store.token}` };

  const listRes = http.get(
    `${CONTROL_BASE_URL}/tasks?store_id=${store.store_id}&state=open&limit=5`,
    { headers }
  );
  if (listRes.status !== 200) return;
  let taskId;
  try {
    const data = JSON.parse(listRes.body).data;
    taskId = data && data[0] && data[0].task_id;
  } catch (e) {
    return;
  }
  if (!taskId) return; // no open tasks left on this store right now — nothing to trigger

  http.post(
    `${CONTROL_BASE_URL}/tasks/${encodeURIComponent(taskId)}/resolve`,
    JSON.stringify({ note: 'k6 websocket_journey trigger' }),
    { headers: Object.assign({ 'Content-Type': 'application/json' }, headers) }
  );
}
