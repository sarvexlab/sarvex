// Dev-mode bearer token minting for k6 scripts.
//
// Mirrors scripts/dev/token.sh exactly: an unsigned JWT-shaped
// "header.payload.sig" string, accepted only by the control plane's
// SimBearerResolver (internal/planes/control/auth/bearer.go), which is wired
// in whenever KINDE_ISSUER_URL is unset (dev/local only — see
// scripts/dev/up.sh). Reimplemented here in JS rather than shelling out to
// the bash script, since k6 scripts have no child-process access.
//
// Never valid against a real (Kinde-backed) deployment.
import encoding from 'k6/encoding';

function b64url(obj) {
  return encoding.b64encode(JSON.stringify(obj), 'rawurl');
}

// mintDevToken builds a bearer token for a given principal. role is one of
// "owner" | "staff" | "operator" (operator tokens carry no merchant_id, per
// scripts/dev/token.sh — operators scope via X-Operate-As instead).
export function mintDevToken({ merchantID = 'merch-loadtest', role = 'owner', sub = 'loadtest-user', email = 'loadtest@example.com' } = {}) {
  const header = b64url({ alg: 'none' });
  const claims = { sub, email, role, store_ids: [] };
  if (role !== 'operator') {
    claims.merchant_id = merchantID;
  }
  const payload = b64url(claims);
  return `${header}.${payload}.sig`;
}

export function authHeaders(token) {
  return { Authorization: `Bearer ${token}` };
}
