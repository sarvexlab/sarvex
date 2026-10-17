// Request-payload builders for the serving-plane checkout flow.
//
// These mirror internal/planes/sync/sim.Agent (the repo's own reference UCP
// shopping-agent simulator, driven for real by `cmd/sim checkout run`) field
// for field, so the JSON shapes here are known-good against the actual wire
// types (internal/planes/serving/api/checkout_contract.go,
// internal/spec/v20260408/payment.go, .../connectors/base) rather
// than guessed.

// createCheckoutBody builds a POST /checkout-sessions body.
// items: [{ sku, qty }]
export function createCheckoutBody(items) {
  return {
    line_items: items.map((i) => ({ item: { id: i.sku }, quantity: i.qty || 1 })),
  };
}

// updateCheckoutBody builds a PUT /checkout-sessions/:id body (full
// replacement — buyer/fulfillment supplied together, as a real agent would
// on the one update call in the create->update->complete flow).
export function updateCheckoutBody({ buyer, address } = {}) {
  const body = {};
  if (buyer) {
    body.buyer = {
      email: buyer.email,
      first_name: buyer.firstName,
      last_name: buyer.lastName,
      phone_number: buyer.phone || '',
    };
  }
  if (address) {
    body.fulfillment = {
      selected_destination: {
        first_name: address.firstName,
        last_name: address.lastName,
        street_address: address.street,
        address_locality: address.city,
        address_region: address.region,
        postal_code: address.postalCode,
        address_country: address.countryCode,
      },
    };
  }
  return body;
}

// completeCheckoutBody builds a POST /checkout-sessions/:id/complete body.
// handlerId must come from the store's own profile (GET .well-known/ucp ->
// ucp.payment_handlers) — it is not a fixed constant, since payment
// handlers are per-store, negotiated configuration.
export function completeCheckoutBody({ handlerId, checkoutId }) {
  return {
    payment: {
      instruments: [
        {
          handler_id: handlerId,
          type: 'card',
          credential: { type: 'sim_token', token: `sim-tok-${checkoutId}` },
          selected: true,
        },
      ],
    },
  };
}

export function defaultBuyer(seq) {
  return {
    email: `loadtest-buyer-${seq}@example.com`,
    firstName: 'Jane',
    lastName: 'Doe',
    phone: '',
  };
}

export function defaultAddress() {
  return {
    firstName: 'Jane',
    lastName: 'Doe',
    street: '1 Main St',
    city: 'Springfield',
    region: 'IL',
    postalCode: '62701',
    countryCode: 'US',
  };
}

// paymentHandlersFromProfile extracts payment handler ids from a
// GET .well-known/ucp response body, in profile order — mirrors
// sim.Agent.FetchProfile's parsing exactly (internal/planes/sync/sim/agent.go).
export function paymentHandlersFromProfile(profile) {
  const handlers = [];
  const ucpBlock = (profile && profile.ucp) || {};
  const byHandlerType = ucpBlock.payment_handlers || {};
  for (const key of Object.keys(byHandlerType)) {
    const list = byHandlerType[key];
    if (!Array.isArray(list) || list.length === 0) continue;
    const entry = list[0];
    if (entry && entry.id) handlers.push(entry.id);
  }
  return handlers;
}
