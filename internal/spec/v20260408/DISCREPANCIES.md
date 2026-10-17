# DISCREPANCIES — UCP types v20260408

Notes on ambiguities in the spec source
(`github.com/Universal-Commerce-Protocol/ucp`, `release/2026-04-08` branch,
commit `a2d8bf0`) and the decisions taken when generating this package.
Source of truth used throughout: the JSON Schema files under `source/`, not
the rendered reference page (they were not observed to disagree, but the
repo schemas would win per the task brief if they had).

## 1. `business_fulfillment_config.json` and `merchant_fulfillment_config.json` are duplicates

Both files are byte-for-byte identical except `$id`, `title`, and
`description` (s/Business/Merchant/). Only `business_fulfillment_config.json`
is actually `$ref`'d anywhere in the schema tree (from
`fulfillment.json#/$defs/dev.ucp.shopping.fulfillment/business_schema`).
`merchant_fulfillment_config.json` is unreferenced — it reads like a
leftover from a rename that wasn't fully cleaned up.

**Decision:** modeled both as distinct Go types
(`BusinessFulfillmentConfig`, `MerchantFulfillmentConfig` in
`fulfillment.go`) per the coverage list, which names them separately. Only
`BusinessFulfillmentConfig` is wired into the fulfillment extension
(`extensions/fulfillment.BusinessCapability.Config`), matching the schema.
`MerchantFulfillmentConfig` exists for coverage but nothing in this module
constructs one from a wire shape — flag this if the spec's next release
resolves the duplication, since one of the two types will likely become
dead code.

## 2. `Order.fulfillment` is a bare, untitled object

`order.json`'s `fulfillment` property is inlined as
`{"type": "object", "properties": {"expectations": ..., "events": ...}}`
with no `$id`/`title`/`$ref` — unlike every other nested shape in the
spec, which is always a named `$ref`.

**Decision:** gave it a name anyway — `OrderFulfillment` in
`fulfillment.go` — since a bare anonymous struct would be awkward to
document and test. If a future spec version promotes this to a named
schema, expect its `$id` to land here.

## 3. `PaymentCredential` subtype dispatch is structural, not declared

`card_credential.json` fixes `type` to the constant `"card"`, but
`token_credential.json` leaves `type` as an open string ("the specific
type of token produced by the handler, e.g. 'stripe_token'") — there is
no enumerable list of token type strings to switch on, and arbitrary
future/handler-specific credential types are explicitly in scope per
`payment_credential.json`'s `additionalProperties: true` base.

**Decision:** `PaymentCredential.UnmarshalJSON` (`payment.go`) dispatches
`type == "card"` to `CardCredential`; otherwise, presence of a `token`
field (`token_credential.json`'s only other required property) selects
`TokenCredential`; everything else is preserved verbatim in `Raw`
(`json.RawMessage`) so unknown/handler-specific credential shapes never
lose data passing through Sarvex. This mirrors the
`FulfillmentDestination` heuristic (`RetailLocation` has `name`;
`ShippingDestination` doesn't) that the task brief explicitly sanctioned
for the same class of problem.

## 4. `Capability.extends`: oneOf(string, array) fidelity is semantic, not byte-exact

`capability.json#/$defs/base` types `extends` as
`oneOf(string, array<string> minItems 1)`. A capability that extends
exactly one parent can therefore be written either as a bare string or as
a one-element array on the wire; both mean the same thing.

**Decision:** `Capability.MarshalJSON` (`metadata.go`) always emits a bare
string for exactly one parent and an array for zero-or-multiple. A
document that used a one-element array for `extends` will therefore
round-trip to the equivalent bare-string form, not byte-identically. This
is a deliberate normalization, not data loss — the parsed `Extends
[]ReverseDomainName` is unaffected either way.

## 5. AP2's `error_code` is a closed enum; the module-wide "don't reject unknown" convention still applies

`ap2_mandate.json#/$defs/error_code` is a genuine closed `enum` (unlike
the base spec's freeform `error_code.json`/`warning_code.json`/
`info_code.json`, which are typed `string` with only illustrative
`examples`).

**Decision:** kept `extensions/ap2.ErrorCode` as an unmarshal-permissive
typed string with a `Known()` helper, exactly like every other code enum
in this module, rather than special-casing it to reject unrecognized
values. Rationale: Sarvex is a pass-through gateway (see package
`payment.go` doc comment) — hard-rejecting a value the spec's *next*
patch release adds to this enum would turn a forwards-compatible wire
change into a decode failure on our side. `Known()` still reports `false`
for anything off the current list, so callers that want strict validation
against the pinned spec version can do so explicitly.

## 6. Empty JSON objects/arrays round-trip to "absent", not byte-identically

Every optional `map`/`slice` field in this package uses `omitempty`
(per the module convention: "optional fields get `omitempty` where JSON
absence and zero value are equivalent"). A registry field sent as an
explicit empty object (e.g. `"payment_handlers": {}`) unmarshals to a
non-nil empty map, but marshaling that map back out omits the key
entirely (Go's `encoding/json` treats `omitempty` + zero-length the same
whether the map is `nil` or merely empty) — so a second unmarshal yields
`nil`, not an empty map.

**Decision:** left as-is. An explicitly-empty registry and an absent one
are semantically identical for every consumer of this package ("nothing
registered" either way), and the alternative — dropping `omitempty` on
`UCPMetadataBase.Services/Capabilities/PaymentHandlers` — would force
`CartResponseMetadata`/`OrderResponseMetadata`/`CatalogResponseMetadata`
(where these fields are optional and normally absent) to serialize
explicit `null`, which the schema's plain `"type": "object"` properties do
not allow. Code that must distinguish "explicitly empty, present" from
"absent" for spec-conformance testing should inspect the pre-marshal Go
value, not rely on wire round-trip byte equality.

## 7. `Description`/`ReverseDomainName`/`Totals`/etc. `Validate()` is opt-in, per the task brief

Per instruction, `UnmarshalJSON` never validates implicitly anywhere in
this package. `Validate() error` methods exist on `Totals`,
`ReverseDomainName`, `Description`, `PaginationResponse`,
`ErrorResponse`, `Checkout`, `Cart`, `Order`, `FulfillmentEvent`,
`Capability` (as `ValidateDiscovery`), `Service` (as `ValidateDiscovery`),
`PlatformMetadata`, `BusinessMetadata`, `CheckoutResponseMetadata`, and
`ErrorResponseMetadata` — callers opt in explicitly. Not every
schema-level constraint is enforced (e.g. `Totals`' `minContains`/
`maxContains` on detail types, or every `if`/`then` conditional in
`total.json`'s sign constraints) — only the ones the task brief called
out by name, plus the handful of invariants ambiguous enough to be worth
codifying. Treat `Validate()` as "the constraints we chose to encode,"
not a full JSON Schema validator.

## 8. `dev.ucp.common.identity_linking` (`source/schemas/common/identity_linking.json`) is out of scope

This capability schema exists in the release/2026-04-08 tree but is not
named anywhere in the task's coverage list (Capability schemas: Cart,
Catalog Lookup, Catalog Search, Checkout, Order, Payment — identity
linking is not among them). Not modeled. If a future task needs it, its
shape (an OAuth-scope map keyed by `{capability}:{scope}` reverse-domain
tokens) is straightforward to add as `extensions/identitylinking`
following the same composition pattern as the other four extensions.

## 9. `cart.json`'s inline `checkout` `$def` (cart-to-checkout conversion) is a request field, not a type

`cart.json#/$defs/checkout` ("Checkout with Cart") adds a `cart_id` field
to `Checkout` for cart→checkout conversion. It isn't a `dev.ucp.shopping.*`
extension capability in its own right — it's a local `$def` inside the Cart
capability schema — so it is not modeled as a type here. The serving plane
implements it as the optional `cart_id` field of its checkout-create request
(`internal/planes/serving/api/checkout_contract.go`): line items, buyer and
discounts the request does not itself carry are taken from the cart.

## 10. Transport/service plumbing (`transports/embedded_config.json`, `*.openrpc.json`, `*.openapi.json`) is out of scope

Per the task's non-goals ("No HTTP clients, handlers, or transport
code — that's `ucpnego`/`echoucp` territory") and per `Service.Config`
being typed `map[string]any` (mirroring `Entity.Config`), the
embedded-transport config shape, and the REST/MCP/embedded RPC method
definitions under `source/services/` and `source/handlers/`, are not
modeled. `Service`/`PaymentHandler`/`Capability` all carry a generic
`Config map[string]any` for exactly this kind of transport- or
handler-specific payload.
