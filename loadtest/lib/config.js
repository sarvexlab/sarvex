// Shared env-var configuration, defaulting to the ports/paths
// scripts/dev/lib.sh resolves for `make dev-up` (CONTROL_PORT=8081,
// SYNC_PORT=8082, SIM_PORT=4106, SERVING_PORT=8083).
export const CONTROL_BASE_URL = __ENV.CONTROL_BASE_URL || 'http://localhost:8081/v1';
export const SERVING_BASE_URL = __ENV.SERVING_BASE_URL || 'http://localhost:8083/ucp';
export const SYNC_BASE_URL = __ENV.SYNC_BASE_URL || 'http://localhost:8082/v1';
export const SIM_BASE_URL = __ENV.SIM_BASE_URL || 'http://localhost:4106';

// Must match the control plane's configured DashboardAddress
// (configs/control/configs.local.json's "DashboardAddress" for `make dev-up`,
// env DASHBOARD_ADDRESS otherwise) — checkWSOrigin
// (internal/planes/control/api/realtime.go) rejects any WebSocket handshake
// whose Origin header doesn't match it exactly, k6 included.
export const DASHBOARD_ORIGIN = __ENV.DASHBOARD_ORIGIN || 'http://localhost:5173';

// Path to the manifest seed.sh writes (store/merchant/token fixtures). k6's
// open() only works in the init context, so scripts that need it call
// open(MANIFEST_PATH) themselves at the top of the file, not inside a
// function — see loadtest/README.md.
export const MANIFEST_PATH = __ENV.LOADTEST_MANIFEST || '../.manifest.json';
