# Real-store test plan (WooCommerce and Magento)

**Status:** not yet executed. Everything below was built and tested against the
simulator (`internal/planes/sync/sim`) and connector fakes, which mirror the
platforms' documented REST shapes. They are not real stores. This document lists
what a real-store run must confirm, ordered by how likely the simulator is to be
hiding a difference.

Scope: the two supported platforms (WooCommerce, Magento) × the two active spec
generations (2026-04-08, 2026-08-25). The frozen connectors are out of scope.

## 0. How to run and record

- Use a staging store per platform, never production. Use a throwaway payment
  method (offline / test gateway) and disposable buyer data.
- Drive the flows through Sarvex's serving endpoints (a UCP agent or the
  conformance client), not the connector directly, so profile, negotiation,
  session store, idempotency and the order handoff are exercised too.
- Repeat each flow at both spec versions (negotiate via `UCP-Agent`).
- Record for each item: platform, version, store/plugin versions, PASS/FAIL,
  request/response excerpts, and any store-side state (order, meta, comment).
- A failure that comes from the simulator being wrong is fixed in the simulator
  and the connector together (sarvex-v1-architecture.md), with a regression test.

## 1. Highest-risk gaps (simulator ≠ real store)

| # | Area | Why the simulator may be hiding a difference |
|---|------|----------------------------------------------|
| 1 | **WooCommerce shipping rate options** | The connector builds a checkout's rate options from `available_shipping_rates` on the order. That is a **simulator-only field**; stock WooCommerce orders do not have it. On a real store the shipping method will be offered with **no rate options** until rates are sourced from the store's shipping zones/methods (or a rate-quote endpoint). Expect this to fail. |
| 2 | **WooCommerce charging** | The connector sets the order to `processing` and forwards `payment_token`. Stock WooCommerce ignores the token, so nothing is charged. Needs a gateway integration, which conflicts with sarvex-v1-architecture.md (thin plugin). Decide the approach before testing payment. |
| 3 | **WooCommerce order totals** | REST-created orders do not necessarily compute shipping/tax/coupon totals the way the simulator does. Verify `shipping_total`, `total_tax`, `discount_total`, `total` after each write. |
| 4 | **Magento guest checkout** | The connector uses the real Magento request shapes (`addressInformation`, `cartItem`, `payment-information` with email + billing address), written from the documentation. Never run against a real Magento. |
| 5 | **Magento in-store pickup** | Detection reads MSI `GET /inventory/sources` for `is_pickup_location_active`; selection posts the `instore` / `pickup` carrier with `extension_attributes.pickup_location_code`. Both are from memory of the MSI module. |
| 6 | **Magento consent storage** | Consent is written as an internal order comment (`POST /orders/{id}/comments`). Confirm the endpoint, payload and admin visibility. |
| 7 | **Sim-only behaviours** | The sim accepts the `fail_token` payment token as a decline, uses a single flat rate, one coupon code (`SPRING20`), item ids equal to product ids, and a one-source store. None of these are real. |

## 2. WooCommerce

Environment: WooCommerce with the REST API enabled, consumer key/secret with
read/write, a shipping zone with a flat rate, at least one product with stock,
the Sarvex plugin installed. Note WooCommerce and plugin versions.

### 2.1 Catalog and identifiers
- Catalog lookup returns numeric product ids; checkout accepts SKUs (the
  connector resolves them) and numeric ids. Confirm both, and that an unknown SKU
  is a `not_found` rejection, not a 5xx.
- Variable products: variant ids resolve; out-of-stock variants reject with
  `out_of_stock`.

### 2.2 Checkout
- Create → update → complete creates exactly **one** WooCommerce order. Confirm
  no duplicate pending orders after retries or lost responses (create/complete
  use no-retry writes).
- Line-item replacement (PUT): quantity change, removal, addition.
- Currency, taxes and totals match what the WooCommerce admin shows (see 1.3).
- Order status transitions: `pending` on create, `processing` on complete; the
  completed response carries the order id, number and a working permalink.

### 2.3 Fulfillment
- **Rate options (1.1):** confirm what a real order returns and decide how rates
  are sourced. Check destination handling, chosen rate recorded as a shipping
  line with the right `method_id` / `instance_id` / `total`.
- **Pickup detection:** enable Local Pickup (classic and block "Pickup
  Location") in a zone; confirm `/shipping/zones`, `/shipping/zones/{id}/methods`
  (including zone 0) and `/settings/general` return what the connector reads.
  Check the store address maps correctly (`woocommerce_default_country` as
  `CC:REGION`), and that disabling pickup removes it within the 5-minute cache.
- **Pickup selection:** a pickup order has a local-pickup shipping line and the
  store address; no shipping charge; the response does not also show it as a
  shipping destination/rate.
- The pickup location is named generically ("Local pickup") because the store
  name is not in the general settings. Confirm this is acceptable.

### 2.4 Discounts
- Real coupons: percentage, fixed, expired, usage-limited, minimum-spend,
  free-shipping. Confirm applied amounts, the canonical code in `applied`, and
  that an invalid code is simply absent. Confirm an empty `codes` list clears.
- Multiple coupons on one order.

### 2.5 Buyer and consent
- Buyer maps to billing (name, email, phone). Consent is stored in order
  `meta_data` under `_ucp_buyer_consent`; an update without consent clears it.
- Confirm the meta is not exposed to the customer (underscore-prefixed keys are
  hidden in the admin UI and emails by default).

### 2.6 Cart
- A cart is a pending order. Confirm carts do not trigger emails, stock
  reservation surprises, or appear as real orders in reports; confirm abandoned
  carts are cleaned up (or decide a retention policy).
- Agents only ever see Sarvex cart ids, never the order id.

### 2.7 Payment
- See 1.2. Also confirm a rejected payment yields a `payment_failed` rejection
  and leaves the order recoverable, and that no token or card data appears in
  Sarvex logs, session store, events or responses.

## 3. Magento

Environment: Magento 2.4.x Open Source with REST enabled, an integration/admin
account, a shipping method (flat rate), an offline payment method (e.g.
`checkmo`), products with stock. For pickup: MSI enabled with at least one source
marked as a pickup location. Note Magento version and enabled modules.

### 3.1 Auth and store scope
- Admin token issuance and refresh; the `rest/{store_code}/V1` path with the
  configured store code; multi-store-view stores.

### 3.2 Cart and checkout
- Guest cart create returns a masked id; every response uses the **masked id**
  the agent addressed the cart by, never the numeric quote id.
- Add item is by SKU and **adds** to an existing line; the connector changes an
  existing line via `PUT .../items/{item_id}` and removes via `DELETE`. Confirm
  quantity replacement and removal semantics, configurable/bundle products, and
  out-of-stock messages (`The requested qty is not available` etc. →
  `out_of_stock`).
- **Totals** come from `GET .../totals`; the quote itself carries none. Confirm
  subtotal, tax, shipping, grand total, and that discount is negative and
  reported once.
- Quote currency vs the configured store currency (multi-currency stores).

### 3.3 Fulfillment
- `POST .../estimate-shipping-methods` with an address returns the rates offered;
  confirm carrier/method codes join as `carrier_method` and that carriers with
  underscores in their code parse correctly (the id is split at the first `_`).
- `POST .../shipping-information` with `addressInformation`: confirm required
  address fields for your country (region, postcode, telephone) and the
  resulting quote `shipping_assignments`.
- **Pickup (1.5):** confirm source fields (`is_pickup_location_active`, name,
  address, `frontend_name`), the carrier/method for in-store pickup, and the
  address extension attribute. Only the first active source (by code) is offered;
  decide whether multiple locations are needed. Stores without MSI must read as
  "no pickup" (404/403 from `/inventory/sources`).

### 3.4 Discounts
- `PUT .../coupons/{code}` and `DELETE .../coupons`. Magento allows **one coupon
  per cart**: the connector applies the first code the store accepts. Confirm
  cart-price-rule coupons (percentage, fixed, free shipping), refused codes, and
  auto-applied rules without a coupon (reported as a discount with no code).

### 3.5 Placing the order
- `payment-information` for a guest needs `email` and `billingAddress`. The
  connector derives the billing address from the quote's shipping destination
  (a UCP buyer has no postal address). Confirm this is accepted, including for
  pickup orders, and that virtual/no-shipping carts behave.
- Payment: the opaque token is sent as `paymentMethod.additional_data.
  payment_token`. Confirm the chosen payment method module accepts/ignores it and
  that a decline returns a client error mapped to `payment_failed`.
- Offline methods leave the order `pending`; the connector still reports the
  checkout `completed`. Confirm this is the desired semantics, and that the
  response's order id is the **entity id** (used for `/orders/{id}` lookups) with
  `increment_id` as the label.
- Placement is not retried. Confirm a timeout after the order was placed does not
  produce a second order on client retry (Sarvex idempotency key path).

### 3.6 Consent and buyer
- Consent flags recorded as an internal order comment (not customer-visible, no
  customer notification) — see 1.6. Buyer contact appears on the order.

### 3.7 Orders
- `GET /orders/{entity_id}` status mapping, including Magento's spelling
  `canceled`, `closed`, `holded`, `payment_review`.

## 4. Spec versions (both platforms)

- Negotiate 2026-08-25 and repeat 2.x / 3.x. Encoded responses are checked
  against the generation's typed schema, but only with simulator data — confirm
  against a real UCP client/validator:
  - `buyer.consent` as a map keyed by reverse-DNS purpose with `source` and
    `description`; a platform's own detail is echoed only when it agrees with the
    merchant's answer.
  - Fulfillment destinations carry `type` (`shipping_address` /
    `business_location`); pickup destinations have `id`, `name`, `address`
    (spec-required: `id`, `name`, `type`).
  - Fulfillment option `description` is a Description object.
  - Pickup: the platform selects with `selected_destination_id`; an unknown
    location is rejected (`invalid_destination`). The spec also lets a business
    accept a valid location id it did not list; we do not.
- A request with no `UCP-Agent` (not negotiated) is served as 2026-04-08.
- Payment credentials: 2026-08-25 adds raw-PAN-shaped fields (`pan`,
  `network_token`). Confirm none is stored, logged or echoed, and that only
  `handler_id` and the opaque token are used.
- Order webhooks are rendered in the version the session negotiated; confirm the
  platform's webhook endpoint accepts them.

## 5. Sarvex-side flows to cover with a real store

- **Order handoff:** completion emits the order intent with the order id; the
  sync plane records the sale and can re-fetch the order by that id (WooCommerce:
  post id; Magento: entity id). Check console order counts.
- **Idempotency:** replay create/update/complete with the same
  `Idempotency-Key` (identical response) and a different body (409).
- **Tenancy:** two stores with the same idempotency key and the same cart/order
  ids never see each other's sessions or carts.
- **Freshness/stock:** price or stock changed between create and complete.
- **Profile:** the served `/.well-known/ucp` advertises only functional
  capabilities/extensions (catalog, checkout, order, cart; fulfillment,
  discount, buyer_consent) at both versions, with pickup absent from the profile.
- **Resilience:** store 5xx/timeouts during create, update and complete surface
  as connector-unavailable, not as business rejections; no partial state is
  reported as success.
- **Logging/secrets:** no credentials, tokens, emails or addresses in logs
  beyond what policy allows.

## 6. Exit criteria

- Every item above PASS on both platforms at both versions, or has a filed
  defect and a decision recorded here.
- Any simulator difference found is fixed in the simulator + connector with a
  test, and `TestCapabilityMatrix` still passes.
- The WooCommerce rate-option source (1.1) and payment approach (1.2) are
  decided before the WooCommerce fulfillment/payment sections are considered
  testable.
