# DISCREPANCIES — UCP types v20260825

Notes on ambiguities in the spec source
(`github.com/Universal-Commerce-Protocol/ucp`, `release/2026-08-25` branch,
commit `cd78fb3`) and the decisions taken when generating this package.
Source of truth used throughout: the JSON Schema files under `source/`,
not the rendered reference page. Where useful, this file also compares
against the prior generation (`release/2026-04-08`, commit `a2d8bf0`,
modeled in the sibling `v20260408` package) to explain a breaking change
or to reconcile a discrepancy that package's own `DISCREPANCIES.md`
flagged.

This package is a **separate, isolated generation**: it does not import
`v20260408`, and `v20260408` was not modified to produce it. The two
packages are wire-incompatible siblings, not versions of one API.

## 1. `signing_keys[]` renamed to `keys[]`, and the JWK shape gained real structure

v20260408's discovery profile carried public keys under `signing_keys`
(`source/discovery/profile_schema.json`, not even a `source/schemas/`
file in that generation — see that package's own history). This
generation formalizes discovery profiles as `source/schemas/profile.json`,
whose `base` `$def` publishes keys under `keys`, described as "the
canonical UCP profile field for publishing signing keys, as a JWK Set per
RFC 7517" — explicitly interoperable as a Web Bot Auth `jwks_uri` source.

The JWK shape (`$defs/jwk_public_key`) also grew real validation: `kty`
examples narrow to `EC`/`OKP` (RSA's `n`/`e` are gone from the modeled
properties, though `additionalProperties: true` still permits them off
an unknown key type), curve/algorithm pairing is enforced (`P-256` with
`ES256`, `P-384` with `ES384`, `Ed25519` with `EdDSA`), and a `not`
clause explicitly forbids private-key members (`d`, `p`, `q`, `dp`, `dq`,
`qi`, `oth`, `k`) — a profile publishes public keys only.

**Decision:** renamed `PlatformDiscoveryProfile.SigningKeys` /
`BusinessDiscoveryProfile.SigningKeys` to `Keys` (`metadata.go`) to match
the wire field. Kept the Go type name `SigningKey` (rather than renaming
to something like `JWKPublicKey`) since it is still exactly what it says
— a signing key — and renaming it would just add churn on top of the
field rename that actually matters. `SigningKey` grew an `Extra
map[string]any` passthrough (`json.go`) since the schema's
`additionalProperties: true` is now meaningfully exercised (unmodeled key
types, non-standard JWK members) in a way v20260408's version — whose
predecessor schema was less explicit about extensibility — didn't call
out as clearly.

## 2. `Capability`'s `business_schema` now also requires `schema`

v20260408's `capability.json#/$defs/business_schema` had no additional
`required` beyond the base entity's `version`. This generation's
`business_schema` adds `{"required": ["schema"]}`, with an updated
description: "Requires the `schema` URL so platforms can fetch and
compose it during negotiation."

**Decision:** added `Capability.ValidateBusinessDiscovery()`
(`metadata.go`) alongside the existing `ValidateDiscovery()` (which still
covers the unchanged `platform_schema` requirement of `spec` + `schema`).
Business-level capability declarations that omit `schema` will fail the
new method but were never checked by `ValidateDiscovery()` in the first
place, so this is purely additive.

## 3. Payment credentials: `card` deprecated in favor of `pan` / `network_token`

v20260408 modeled exactly two credential shapes: `CardCredential` (fixed
`type: "card"`, carrying `card_number_type` to distinguish `fpan` from
`network_token` numbers) and the open-ended `TokenCredential`. This
generation's `common/types/card_credential.json` is now marked
`"deprecated": true` (kept, not removed, for backward compatibility) and
is joined by two narrower, non-deprecated successors:

- `pan_credential.json` (`type: "pan"`, required `number`; verified with
  a `cvc`) — for a funding PAN, however it was sourced (a raw card
  number, or a network token surfaced in PAN form and verified the same
  way a raw PAN would be).
- `network_token_credential.json` (`type: "network_token"`, required
  `number` + `cryptogram`) — for a token verified with a discrete
  transaction cryptogram, plus an optional `token_requestor_id`.

The schema's own description is explicit that credential selection
follows the *shape of the value on the wire*, not its provenance: the
same underlying network token can arrive as either a `PANCredential` or a
`NetworkTokenCredential` depending on which verification data
(`cvc` vs. `cryptogram`) accompanies it.

**Decision:** `payment.go` keeps `CardCredential` (doc-commented
`Deprecated:`) and adds `PANCredential` and `NetworkTokenCredential` as
new sibling subtypes of `PaymentCredential`. `PaymentCredential`'s
`UnmarshalJSON` dispatch gained two new `case`s (`type == "pan"`,
`type == "network_token"`) ahead of the existing `card`/token-presence/
raw-fallback logic, exactly mirroring the precedent v20260408 set
(`payment.go`'s doc comment, and that package's `DISCREPANCIES.md #3`).
Unknown credential types still fall through to `Raw` — round-trip
fidelity never regresses, whatever new `type` values a future release
adds. Every raw field (`Number`, `CVC`, `Cryptogram`, `TokenRequestorID`,
...) is modeled without exception, per the task's explicit instruction
not to design a "safer" API than the schema calls for: this package's
security boundary is structural (see `internal/planes/serving/api/
checkout_contract.go`'s `OpaquePaymentCredential`, which never decodes
into this package's types), not a matter of which fields this package
chooses to expose.

## 4. `Binding` is now capability-generic, and `Identity` moved out of it

v20260408's `Binding` (`shopping/types/binding.json`, referenced only
from the out-of-scope tokenization handler OpenAPI) was
`{checkout_id: string, identity?: PaymentIdentity}` — checkout-specific,
with the participant identity nested inside.

This generation's `common/types/binding.json` is
`{type: ReverseDomainName, id: string}` — a generic binding to *any*
capability resource (`type` names the owning capability, e.g.
`"dev.ucp.shopping.checkout"`; `id` is the opaque resource id within it),
no longer checkout-specific. `PaymentIdentity` (`common/types/
payment_identity.json`, unchanged shape) is now a sibling field next to
`binding` in the tokenization handler's request body, not nested inside
it — see `source/handlers/tokenization/openapi.json` at this commit,
still out of scope per item 10 below, but instructive for how the two
types now compose.

**Decision:** `Binding` (`types.go`) is `{Type ReverseDomainName, ID
string}`, matching the new generic shape. `PaymentIdentity` is unchanged.
Both remain modeled as standalone types (not composed into a struct
together) for the same reason v20260408 modeled them standalone: the
endpoint that combines them is transport/handler plumbing, out of scope
per item 10.

## 5. Buyer consent: booleans replaced by a reverse-DNS-keyed purpose/segment map

v20260408's Buyer Consent extension (`extensions/buyerconsent`) modeled
`Consent` as four independently nullable booleans (`Analytics`,
`Preferences`, `Marketing`, `SaleOfData`) — a closed, hardcoded set.

This generation's `shopping/buyer_consent.json` replaces that with
`Consent`, an object keyed by reverse-DNS purpose identifier (four
well-known purposes are named in the schema description:
`dev.ucp.consent.marketing`, `dev.ucp.consent.analytics`,
`dev.ucp.consent.preferences`, `dev.ucp.consent.sale_or_sharing` — note
`sale_or_sharing`, not v20260408's `sale_of_data`), whose value
(`consent_purpose`) carries not just a boolean but `granted` (bool),
`source` (`"business"` | `"platform"` — who asserted the current value),
a required `description`, optional `links`, and optional `segments` — a
nested reverse-DNS-keyed map of the same shape (minus further nesting)
for finer-grained decisions within a purpose (e.g.
`dev.ucp.consent.marketing.email` under `dev.ucp.consent.marketing`).

**Decision:** `extensions/buyerconsent/buyerconsent.go` models
`ConsentSource`, `ConsentSegment`, `ConsentPurpose`, and `Consent
map[ReverseDomainName]ConsentPurpose`, replacing v20260408's four bool
pointers wholesale — there is no meaningful backward-compatible bridge
between "four hardcoded booleans" and "an open, extensible purpose map"
worth building into the type itself. `Purpose*`/`Segment*` constants
name the schema's well-known identifiers. `BuyerWithConsent` keeps the
same Go-embedding composition pattern (`Buyer` + `Consent`,
custom `(Un)MarshalJSON` to avoid the embedding trap) as v20260408.

## 6. Fulfillment destinations gained a real `type` discriminator

v20260408's `FulfillmentDestination` had no wire discriminator at all —
`ShippingDestination` and `RetailLocation` were told apart purely
structurally (`RetailLocation` always carries `name`, required by its
schema; `ShippingDestination` never does — see that package's
`DISCREPANCIES.md`, which explicitly sanctioned this heuristic).

This generation's `shopping/types/fulfillment_destination.json` adds
`type` (`"shipping_address"` | `"business_location"`), described as
"required in Business responses and optional in Platform requests" — an
`if`/`then` pair on `type` selects between `shipping_destination.json`
(`ShippingDestination`, itself now also carrying its own required `id`
+ `type: "shipping_address"` when validated standalone) and the new
`location_destination.json` (`BusinessLocationDestination`, an alias of
the also-new `common/types/location_summary.json` plus a fixed
`type: "business_location"`).

**Decision:** `FulfillmentDestination.UnmarshalJSON` (`fulfillment.go`)
now dispatches on `type` first, and only falls back to the v20260408
name-presence heuristic when `type` is absent — which the spec still
permits on Platform requests. `MarshalJSON` always injects the
discriminator, so business-facing output is spec-conformant even when a
caller builds a `FulfillmentDestination` programmatically without
setting a `Type` field explicitly (there isn't one on `Shipping`/`Retail`
themselves — the discriminator is owned entirely by
`FulfillmentDestination`, mirroring how v20260408's `CardPaymentInstrument`
fixes `Type: "card"` on marshal rather than exposing a redundant field).
One added subtlety not present in v20260408: `UnmarshalJSON` must strip
the `type` key before delegating to `BusinessLocationDestination`
(`= LocationSummary`), or it round-trips into `LocationSummary`'s own
`Extra` passthrough on a second decode — `location_summary.json` has no
`type` property of its own, so an unstripped `type` key would otherwise
look like an unrecognized additional property. Renamed
`RetailLocation` -> `BusinessLocationDestination` to match the new
schema's title and wire constant.

## 7. Fulfillment config: the v20260408 duplicate is gone, and the "allows_" prefix was dropped

v20260408's `DISCREPANCIES.md #1` flagged that
`business_fulfillment_config.json` and `merchant_fulfillment_config.json`
were byte-for-byte duplicates (differing only in `$id`/title/description),
with only the former actually `$ref`'d anywhere, and predicted "flag this
if the spec's next release resolves the duplication."

It has: this generation's `shopping/types/` directory has only
`business_fulfillment_config.json` (paired with
`platform_fulfillment_config.json`, unchanged) — no `merchant_*` file
exists in the `cd78fb3` tree at all. The duplication is resolved, and
v20260408's speculative `MerchantFulfillmentConfig` accordingly has no
counterpart in this package; it was never referenced by anything besides
coverage-for-its-own-sake.

Separately, `business_fulfillment_config.json` itself changed shape:
`allows_multi_destination` (a fixed `{shipping: bool, pickup: bool}` map)
became `multi_destination`, an array of `{method: string, ...}` entries —
open-ended, so businesses can list any method type, not just the two
built-in ones — and `allows_method_combinations` was renamed to
`method_combinations` (same `[][]string]` shape). The "allows_" prefix
was dropped from both.

**Decision:** `BusinessFulfillmentConfig` (`fulfillment.go`) drops
`AllowsMultiDestination`/`AllowsMethodCombinations` in favor of
`MultiDestination []MultiDestinationEntry` / `MethodCombinations
[][]FulfillmentMethodType`, matching the new field names and shapes.
`MultiDestinationEntry` carries `Method` plus an `Extra` passthrough
(the schema's per-entry `additionalProperties: true` for
"optional per-method constraints MAY be added alongside"). No
`MerchantFulfillmentConfig` type exists in this package — there is
nothing left to model it against.

## 8. `PaymentCredential`'s subtype dispatch is structural for `TokenCredential`, declared for everything else

Same situation as v20260408's `DISCREPANCIES.md #3`, restated because the
dispatcher now has more cases: `card_credential.json`, `pan_credential
.json`, and `network_token_credential.json` all fix `type` to a constant
(`"card"`, `"pan"`, `"network_token"` respectively), so those three
dispatch declaratively. `token_credential.json` still leaves `type` as an
open string ("the specific type of token produced by the handler, e.g.
'stripe_token'"), so `TokenCredential` is still selected structurally, by
presence of the `token` field — its only other required property.

**Decision:** unchanged from v20260408's approach, extended with two more
declarative cases ahead of the structural one. See item 3 above and
`payment.go`'s dispatcher doc comment.

## 9. `Capability.extends`: oneOf(string, array) fidelity is semantic, not byte-exact

Unchanged from v20260408 (`capability.json#/$defs/base`'s `extends` is
still `oneOf(reverse_domain_name, array<reverse_domain_name> minItems
1)`; only the referenced type moved from an inline pattern to
`common/types/reverse_domain_name.json`, a namespace move, not a shape
change). `Capability.MarshalJSON` (`metadata.go`) still normalizes to a
bare string for exactly one parent and an array otherwise, per the same
rationale as v20260408's `DISCREPANCIES.md #4`.

## 10. Empty JSON objects/arrays round-trip to "absent", not byte-identically

Unchanged from v20260408's `DISCREPANCIES.md #6` — every optional
`map`/`slice` field in this package uses `omitempty`, so an explicitly-
empty registry and an absent one are indistinguishable after a
marshal/unmarshal cycle. Same rationale, same acceptance.

## 11. `Validate()` is opt-in, per the task brief

Per instruction, `UnmarshalJSON` never validates implicitly anywhere in
this package. `Validate() error` methods exist on `Totals`,
`ReverseDomainName`, `Description`, `PaginationResponse`, `ErrorResponse`,
`Checkout`, `Cart`, `Order`, `FulfillmentEvent`, `Capability` (as
`ValidateDiscovery` and, new in this generation, `ValidateBusinessDiscovery`
— see item 2), `Service` (as `ValidateDiscovery`), `PlatformMetadata`,
`BusinessMetadata`, `CheckoutResponseMetadata`, and
`ErrorResponseMetadata` — the same set v20260408 encoded, carried
forward unchanged in shape and rationale. Not every schema-level
constraint is enforced (e.g. `Totals`' `minContains`/`maxContains` on
detail types, `total.json`'s sign `if`/`then` constraints on discount vs.
non-discount amounts, `unit.json`'s `C62`-implies-`scale:0` constraint,
or `payment_instrument.json`'s `selected_payment_instrument` shape). Only
the invariants the task brief called out by name, plus the handful
ambiguous enough to be worth codifying, are covered. Treat `Validate()`
as "the constraints we chose to encode," not a full JSON Schema
validator.

## 12. The task brief's hypothesized `anyOf(integer, measure-object)` quantity shape does not exist in this spec version

The task brief predicted that `line_item.json`/`order_line_item.json`
would now type `quantity` as `anyOf(integer, measure-object)` and asked
for a polymorphic int-or-object type analogous to
`FulfillmentDestination`'s dispatch. Checked directly against
`source/schemas/shopping/types/line_item.json` and
`order_line_item.json` at `cd78fb3`: `quantity` is still a **plain
integer** in both — "Always an integer step count," per
`line_item.json`'s own description. `order_line_item.json`'s `quantity`
is still the `{original, total, fulfilled}` object of plain integers,
unchanged in shape from v20260408 beyond the description wording.

What actually carries the fractional-quantity support this release adds
is a *separate* mechanism: `common/types/unit.json` (a reusable
`{unit, scale, display_text}` descriptor), `common/types/quantity_unit
.json` (`Unit` + an `increment` ordering-granularity field), and
`common/types/measure.json` (`Unit` + an integer `value` — "the integer
count of `10^-scale` units of `unit`"). `Item` gained an optional
`quantity_unit` field (the sale basis `LineItem.Quantity` is denominated
in — absence means "each"), and `Item`/`Variant`'s `unit_price` field
(shared `unit_price.json`, previously Variant-only in v20260408) now
uses two `Measure` values (`measure`, `reference`) in place of
v20260408's ad hoc `VariantUnitPriceQuantity{Value float64, Unit
string}`. Separately, `Adjustment`'s per-line-item entries
(`shopping/types/adjustment.json`) gained an optional `measure` field —
a `Measure` reconciling the settled quantity when a line's price settles
by measurement (e.g. actual picked weight) — sitting *alongside* the
existing signed integer `quantity`, not replacing it.

**Decision:** no polymorphic int-or-object type exists in this package,
because the schema does not call for one. `Unit`, `Measure`, and
`QuantityUnit` are modeled in `types.go` as the real mechanism; `Item
.QuantityUnit`/`Item.UnitPrice`, `Variant.QuantityUnit`/`Variant
.UnitPrice`, and `AdjustmentLineItem.Measure` wire them in at their
actual schema locations. `LineItem.Quantity` and `OrderLineItemQuantity`
remain plain `int` fields, unchanged from v20260408. Flagged here per the
task's own instruction to verify every claim against the real schema
file rather than a summary, and to report the outcome either way.

## 13. New in-scope primitives not present in v20260408: `Actions` and `Policy`

Two new horizontal primitives are now embedded directly on the in-scope
capabilities:

- `common/types/actions.json` (`Actions`, `types.go`): outstanding
  extension-defined Action instances, keyed by reverse-domain Action type
  (not by the extension's own capability name). Present on `Cart`,
  `Checkout`, `CatalogSearchResponse`, `CatalogLookupResponse`, and
  `GetProductResponse` — not on `Order`.
- `common/types/policy.json` (`Policy`, `types.go`): a durable business
  rule (return/warranty/etc. terms) with a `type` (open reverse-DNS),
  required `description`, optional `applies_to` (JSONPath targets) and
  `url`. Present on `Cart`, `Checkout`, `Order`, and all three catalog
  response shapes.

Per the task's guidance to use judgment on `policy.json` ("a shallow
Policy type ... is worth modeling if it's simple, exclude with a note if
it's large and not load-bearing"): `policy.json` is a small, flat,
four-property object with an `additionalProperties: true` tail (modeled
via the same `Extra` pattern as `Buyer`/`Context`/etc.) — not a deep
subsystem — so it is modeled directly rather than excluded.

## 14. Product/Variant `description` is now required at the top level

v20260408's `product.json`/`variant.json` did not list `description` in
their own `required` arrays (only `Description.Validate()`'s internal
"at least one of plain/html/markdown" invariant applied once a
`Description` was present). This generation's `product.json` requires
`["id", "title", "description", "price_range", "variants"]` and
`variant.json` requires `["id", "title", "description", "price"]` — both
now list `description` explicitly. No Go-level change follows from this:
`Product.Description`/`Variant.Description` were already non-pointer
`Description` value fields in v20260408, and this package does not add a
`Product.Validate()`/`Variant.Validate()` method (per item 11, only the
task-brief-named invariants get one) — flagged here as a documentation-only
note in case a future task wants to add one.

## 15. `AccountInfo` (Payment Account Reference) was removed from the base spec

v20260408 modeled `AccountInfo{PaymentAccountReference string}` from
`shopping/types/account_info.json`. No file named `account_info.json`,
and no `payment_account_reference` property anywhere, exists in the
`cd78fb3` tree — grepped the full `source/schemas/` directory to confirm.
The EMVCo PAR concept this type carried is not present in this
generation's schema surface at all.

**Decision:** not modeled. If a future spec release reintroduces it
(possibly under a handler-specific schema, since PAR is
issuer/network-specific plumbing more than a core UCP concept), add it
back as a standalone type following the same pattern.

## 16. Out of scope, per the task's coverage list — noted, not modeled

The following schemas exist in the `release/2026-08-25` tree but are not
modeled, matching this package's "checkout-first subset" philosophy and
the task's explicit exclusion list:

- **`common/identity_linking.json`** (`dev.ucp.common.identity_linking`,
  moved from `common/identity_linking.json`'s v20260408 location under
  the same name) — same exclusion rationale as v20260408
  `DISCREPANCIES.md #8`: not in Sarvex's V1 capability list. If needed,
  its shape (a flat `scopes` map of OAuth-scope strings, e.g.
  `"dev.ucp.shopping.order:read"`) is straightforward to add as
  `extensions/identitylinking`.
- **`common/location_search.json`** (`dev.ucp.common.location.search`)
  and **`common/location_lookup.json`** (`dev.ucp.common.location.lookup`)
  — new capabilities in this release; Sarvex V1 does not do physical-store
  discovery. Their supporting deep types (`common/types/location.json`
  with `geo`/`hours`/`exception_hours`/`amenities`,
  `common/types/location_distance.json`, `common/types/location_filter
  .json`, `common/types/location_serves.json`,
  `common/types/daily_hour.json`, `common/types/exception_hour.json`,
  `common/types/amenity_type.json`, `common/types/time_interval.json`)
  are correspondingly **not** modeled either — only the much shallower
  `common/types/location_summary.json` (id/name/address) is modeled, as
  `LocationSummary`, because the in-scope Fulfillment extension's pickup
  destination (`shopping/types/location_destination.json`) needs it. See
  item 6.
- **`common/loyalty.json`** (`dev.ucp.common.loyalty`) — new; not in
  Sarvex V1 scope.
- **`shopping/permalink.json`** (`dev.ucp.shopping.permalink`) — new; not
  in Sarvex V1 scope.
- **`common/payment_split_payments.json`**
  (`dev.ucp.common.payment.split_payments`) and
  **`common/payment_terms.json`** (`dev.ucp.common.payment.terms`) — new;
  Sarvex V1 checkout does not need deferred/split payments. Flagged as a
  real feature gap worth a follow-up if Sarvex ever needs to support
  multi-instrument or deferred-payment checkouts, not silently dropped.
  Their supporting types (`common/types/instrument_group.json`,
  `common/types/business_split_payments_config.json`,
  `common/types/payment_schedule.json`, `common/types/payment_term.json`)
  are correspondingly not modeled.
- **`common/payment_authentication.json`**
  (`dev.ucp.common.payment.authentication`) — new; 3DS2/device-data
  challenge flows built on top of the (in-scope, see item 13) `Actions`
  primitive. `Actions` itself is modeled since Cart/Checkout/the catalog
  responses embed it directly; this specific *consumer* of it (the
  authentication extension's well-known Action type identifiers and
  their `config` shapes) is not. Flagged as a follow-up alongside the
  split-payments/terms gap above, since Sarvex's checkout flow will
  eventually need 3DS support.
- **`common/types/policy.json`'s `applies_to` semantics** are modeled at
  the field level (`Policy.AppliesTo []string`, raw JSONPath strings) but
  this package does not implement JSONPath evaluation or specificity
  resolution ("when policies of the same type contest a node, the
  narrowest target wins") — that is business logic for a consumer of
  this package, not a wire-shape concern.
- **`common/types/constraint_expression.json`** — referenced from
  `common/types/available_payment_instrument.json`'s `constraints` field
  (in scope, since `AvailablePaymentInstrument` is part of
  `PaymentHandler` discovery) and from `common/types/
  request_constraints.json` (see below). It is a small recursive JSON
  Schema Draft 2020-12 subset grammar (`required`/`properties`/`anyOf`,
  with property values recursively either another Constraint Expression
  or a `{enum, const}` Value Constraint). Modeled as an opaque
  `map[string]any` pass-through, the same treatment this package (and
  v20260408) already gives to `Entity.Config`, `PaymentInstrument
  .Display`, and other "structure defined elsewhere" regions — a fully
  typed recursive mirror was judged disproportionate to its load-bearing
  weight for Sarvex's checkout-first subset. See `payment.go`'s
  `AvailablePaymentInstrument` doc comment.
- **`ucp.json`'s `members`/`request_constraints` `$def`s** — the `members`
  `$def` (grouping `map_order` and `request_constraints` as "members
  defined inside the reserved `ucp` protocol object") is not `$ref`'d
  from anywhere else in the `cd78fb3` schema tree (grepped to confirm) —
  it reads as aspirational/forward-looking scaffolding, similar in spirit
  to v20260408's unreferenced `merchant_fulfillment_config.json` (see
  item 7, now resolved). `map_order` itself **is** referenced directly
  from `ucp.json#/$defs/base` and is modeled (`UCPMetadataBase.MapOrder`,
  `metadata.go`). `request_constraints` is not modeled — it is
  negotiation/validation machinery (RFC 9535 JSONPath-scoped constraints
  on the *next* request) squarely in `ucpnego`/`echoucp` territory, per
  the same non-goal v20260408's `DISCREPANCIES.md #10` already stated for
  transport/service plumbing.
- **Everything under `source/services/`, `source/handlers/`, and
  `source/schemas/transports/`** — transport/service plumbing, same
  "that's `ucpnego`/`echoucp` territory" rationale as v20260408
  `DISCREPANCIES.md #10`. This includes the tokenization handler
  (`source/handlers/tokenization/openapi.json`) referenced in items 3-4
  above for context only.
- **`shopping/cart.json`'s inline `checkout` `$def`** (cart-to-checkout
  conversion, adding `cart_id` to `Checkout`) — unchanged in spirit from
  v20260408 `DISCREPANCIES.md #9`; still a local `$def` inside the Cart
  capability schema itself, not a `dev.ucp.*` extension capability in its
  own right, and still not modeled as a type.
- **The fulfillment extension's full catalog-search/lookup wrapper
  compositions** (`fulfillment_search_request`, `fulfillment_search
  _response`, `fulfillment_lookup_request`, `fulfillment_lookup
  _response`, `fulfillment_get_product_request`, `fulfillment_get
  _product_response`, `fulfillment_product`, `fulfillment_lookup_variant`,
  `fulfillment_lookup_product`, `fulfillment_detail_product` — all in
  `shopping/fulfillment.json`) are new in this release (catalog-level
  fulfillment discovery, not just checkout-level) and are only partially
  modeled: `extensions/fulfillment` models the building blocks
  (`CatalogFulfillmentMethod`, `CatalogFulfillment`, `FulfillmentVariant`,
  `SearchFiltersWithFulfillment`) but not every generated
  request/response wrapper the schema spells out. Those wrappers are
  mechanical `allOf` compositions of `Product`/`Variant` with the
  building blocks this package already provides — exactly the kind of
  composition this package's base already expects callers to do
  themselves for `CatalogLookupProduct`/`DetailProduct` (there is no
  "FulfillmentCatalogLookupProduct" type either, by the same reasoning).
  Flag this if a caller needs the full generated set; it is
  straightforward to add following the `CatalogLookupProduct`/
  `DetailProduct` precedent.
