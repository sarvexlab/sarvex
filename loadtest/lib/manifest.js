// Loads the store/merchant/token fixtures scripts/loadtest/seed.sh writes.
// k6's open() only works in the init context (module scope), so every
// scenario file calls loadManifest() once at the top level, not inside
// setup()/default().
import { MANIFEST_PATH } from './config.js';

export function loadManifest() {
  let raw;
  try {
    raw = open(MANIFEST_PATH);
  } catch (e) {
    throw new Error(
      `could not read manifest at ${MANIFEST_PATH} — run scripts/loadtest/seed.sh first (${e})`
    );
  }
  const stores = JSON.parse(raw);
  if (!Array.isArray(stores) || stores.length === 0) {
    throw new Error(`manifest at ${MANIFEST_PATH} is empty — run scripts/loadtest/seed.sh first`);
  }
  return stores;
}

// pick returns a pseudo-random element, spread across VUs/iterations via
// __VU/__ITER so concurrent VUs don't all hammer the same store.
export function pick(list) {
  const i = (__VU * 7919 + __ITER) % list.length;
  return list[i < 0 ? i + list.length : i];
}
