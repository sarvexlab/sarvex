# Sarvex Idempotency Layer: Design & Architecture

**Version**: 1.0  
**Date**: 2026-09-16  
**Status**: Implemented

---

## 1. Problem Statement

Sarvex handles financial mutations (order creation, payment capture, refunds) across merchants. Without exactly-once semantics, retry storms or network delays could cause:

- **Duplicate charges**: Same checkout retried twice → two charges to payment processor
- **Silent divergence**: Replay produces different order IDs → auditor cannot reconcile
- **Lost mutations**: Async handoff failure → order exists at merchant but not in Sarvex
- **Race conditions**: Concurrent updates without guards → invalid state transitions

**Goal**: Achieve exactly-once semantics end-to-end, so replaying the same request with the same idempotency key always produces the same outcome.

---

## 2. Idempotency Model

### 2.1 Idempotency Key Properties

**Scoping**:
- Scoped to `merchant_id` to prevent cross-tenant collisions
- Format: `{merchant_id}:{unique_fingerprint}` (stored internally)
- Client provides unscoped key via `Idempotency-Key` HTTP header

**Lifetime**:
- **Active window**: 24h after order placed (terminal state reached)
- **After 24h**: Key expires and may be reused → new order (acceptable, auditable)
- Implementation: MongoDB TTL index on `placed_at` + 90 days (audit log retention)

**Source**:
- Client-provided (agent/LLM) via HTTP header
- Hash-based to avoid UUID collisions across agents
- Example: `sha256(merchant_xyz:session_abc:timestamp)[0:32]`

### 2.2 Order ID

OrderID comes from the merchant store.


---

## 3. Guarantees & Invariants

### 3.1 Core Guarantees

| Guarantee | Mechanism | Scope |
|-----------|-----------|-------|
| **Exactly-Once** | Session-level dedup via Redis + MongoDB unique index | Checkout creation, order placement |
| **Atomic** | MongoDB transaction: Order insert + audit log append | Order + audit log (same transaction) |
| **Scoped** | Merchant-prefixed keys in storage | No cross-tenant collision |
| **Observable** | Audit log: every mutation with timestamp, handler response, status | Sync plane order_mutations collection |
| **Deferential** | Payment handler sees idempotency key, enforces its own exactly-once | Stripe, PayPal, etc. via header injection |

### 3.2 Invariants (Must Hold)

1. **One Order per (merchant_id, idempotency_key)**  
   - MongoDB unique index on `(merchant_id, idempotency_key)`
   - Exception: after 24h TTL, key may create new order (counter increments)

2. **Every Mutation Logged**  
   - Order.MutationAttempts array appended atomically
   - Sync plane audit log (sync_order_mutations) for forensics

3. **State Machine Forward-Only**  
   - No invalid transitions (e.g., paid → creating)
   - Enforced by CanTransition() validator before persist

4. **Handler Deferred to**  
   - Sarvex passes idempotency key to payment handler
   - Trusts handler's own exactly-once (doesn't double-check)
   - Reduces latency + complexity

5. **No Silent Retries in Money Path**  
   - Connector failure → surfaces error immediately
   - Retry is agent's responsibility (with same idempotency key)
   - No background retry loops on mutations

---

## 4. Architecture: Three Planes

### 4.1 Serving Plane (Order Initiation)

**Entry Point**: `POST /checkout/{sessionId}/complete`

**Flow**:
1. Extract idempotency key from `Idempotency-Key` header
2. Compute body hash (SHA256 of payment payload)
3. Check session store: `GetByIdempotencyKey(storeID, key)`
   - If found + same body hash → return cached result (no connector call)
   - If found + different body hash → return 409 Conflict
4. Call connector: `CheckoutComplete(ctx, storeID, checkoutID, payment, idempotencyKey)`
   - Pass idempotency key to handler (Stripe, etc.)
5. Receive handler response (order ID, status, charge ID)
6. Emit `serving.checkout.order_intent` event to sync plane (async)
7. Persist session with `CompleteIdempotencyKey` + `CompleteBodyHash`
   - Record audit attempt: timestamp, handler response, status
8. Return order to agent (idempotent)

**Storage**: Redis session store (TTL-bounded to session lifetime)  
**Audit**: CheckoutSession.CompleteAttempts array

### 4.2 Sync Plane (Order Recording)

**Consumer**: `serving.checkout.order_intent` event

**Flow**:
1. Parse `OrderIntentPayload` (order_id, idempotency_key, etc.)
2. Check: `GetByCheckoutSession(checkoutSessionID)`
   - If exists → no-op (order already recorded from prior event delivery)
3. Create Order in MongoDB with idempotency key field
4. Start MongoDB transaction:
   - Insert Order document
   - Append to Order.MutationAttempts (handler response, status)
   - Append to sync_order_mutations audit log (forensics)
5. Emit `sync.order.created` event to downstream consumers

**Storage**: MongoDB sync_orders (unique index on `checkout_session_id`, `merchant_id:idempotency_key`)  
**Audit**: Order.MutationAttempts + sync_order_mutations collection

### 4.3 Control Plane (Operator API)

**Queries**:
- Detect double-charges: `sync_order_mutations.find({idempotency_key: K}).count()` should be 1
- Replay diagnostics: `sync_orders.find({idempotency_key: K})[].order_id` should be same ID

---

## 5. Handler Key Injection

### 5.1 Stripe Example

**Serving Plane** (Sarvex):
```go
checkoutResp, err := connector.CheckoutComplete(ctx, storeID, checkoutID, 
  &base.CompleteCheckoutRequest{
    Payment: payment,
    IdempotencyKey: idempotencyKey,  // ← scoped key passed to connector
  })
```

**WooCommerce Plugin** (merchant's store):
```php
// Extract from Sarvex request
$idempotency_key = $sarvex_req->getHeader('Idempotency-Key');

// Pass to Stripe API
$headers = ['Idempotency-Key' => $idempotency_key];
$charge = $stripe->charges->create($charge_data, $headers);
```

**Stripe API**:
- Sees `Idempotency-Key: merchant_acme:key-xyz-123`
- Charges once per key (Stripe's own exactly-once)
- Returns `charge_id` in response

**Benefit**: Stripe is source of truth for charge uniqueness; Sarvex doesn't need to verify.

### 5.2 Supported Handlers

| Handler | Status | Notes |
|---------|--------|-------|
| Stripe | ✓ Full | Idempotency-Key header support confirmed |
| Google Pay | ⚠ TBD | Check handler spec for idempotency parameter |
| Shop Pay | ⚠ TBD | Shopify API contract needed |
| PayPal | ⚠ TBD | PayPal-Checkout-Request-ID support? |
| Adyen | ⚠ TBD | Adyen idempotency mechanism |
| Braintree | ⚠ TBD | Braintree API spec pending |

---

## 6. Audit & Observability

### 6.1 Mutation Audit Log (sync_order_mutations)

**Schema**:
```go
type AuditLog struct {
  MutationID      string                 // uuid
  MerchantID      string
  OrderID         string
  IdempotencyKey  string
  AttemptedAt     time.Time
  CompletedAt     *time.Time
  HandlerResponse map[string]interface{} // e.g., {stripe_charge_id: "ch_..."}
  LocalStatus     string                 // "capturing", "paid", "refunded"
  ErrorMessage    string
  RetryCount      int
  MutationType    string                 // "create", "capture", "refund", "void"
}
```

**Indexes**:
- `(merchant_id, idempotency_key)` — detect double-charges
- `(order_id)` — audit trail for one order
- `(attempted_at)` — TTL index (90-day retention)

**Queries**:

1. **Detect double-charge**:
   ```mongo
   db.sync_order_mutations.find({
     merchant_id: "M1",
     idempotency_key: "K1"
   })
   // Should return exactly 1 document
   ```

2. **Replay diagnostics**:
   ```mongo
   db.sync_order_mutations.find({
     idempotency_key: "K1"
   }, { order_id: 1, handler_response: 1 })
   // All documents should have same order_id
   ```

3. **Order audit trail**:
   ```mongo
   db.sync_order_mutations.find({
     order_id: "O1"
   }).sort({ attempted_at: 1 })
   // Shows create → maybe refund → terminal
   ```

### 6.2 State Machine Audit

**CanTransition Validator** (sync plane):

```go
// Before any state change, check:
ok, violations := CanTransition(currentStatus, newStatus)
if !ok {
  log.Error("invalid transition", "violations", violations)
  return ErrInvalidTransition
}
```

**Audit trail**: Every transition logged with:
- From state, to state
- Timestamp
- Preconditions checked
- Outcome (success or error code)

---

## 7. End-to-End Example: Order Creation

**Timeline**:

```
Time    Component        Action                          Storage
────────────────────────────────────────────────────────────────────────
T0      Agent            Agent generates idempotency key: key-A-T0

T1      Serving Plane    POST /checkout/{id}/complete
                         Header: Idempotency-Key: key-A-T0
                         Body: {payment: ...}

T1+10ms Serving Plane    Compute body_hash = sha256({payment: ...})

T1+20ms Serving Plane    Call redis.get(idempotency:S1:key-A-T0)
                         → MISS (first attempt)

T1+30ms Serving Plane    Call connector.CheckoutComplete(...)
                         → Pass idempotencyKey = "key-A-T0"

T1+100ms Connector       Call Stripe API with header Idempotency-Key
        (WooCommerce)    → Stripe charge created: ch_XYZ
                         → Order at merchant: ord_123
                         → Return to Sarvex: {order_id: ord_123, ...}

T1+110ms Serving Plane   Emit serving.checkout.order_intent event
                         {order_id: ord_123, idempotency_key: key-A-T0, ...}

T1+120ms Serving Plane   redis.set(session:{S1}:{SID}, {...with order_id...})

T1+130ms Serving Plane   Return 200 OK to agent
                         {order_id: ord_123, ...}

T1+200ms Sync Plane      Consume order_intent event
         (async)         → Check order not already recorded
                         → Create Order doc: {_id: ord_123, idempotency_key: key-A-T0}
                         → Append to sync_order_mutations audit log
                         → Emit sync.order.created event

──────────────────────────────────────────────────────────────────────────

T2 (retry, Agent
    network dupes)       POST /checkout/{id}/complete (same request)
                         Header: Idempotency-Key: key-A-T0

T2+10ms Serving Plane    Compute body_hash = sha256({payment: ...})

T2+20ms Serving Plane    Call redis.get(idempotency:S1:key-A-T0)
                         → HIT (stored at T1+120ms)
                         → sessionID = SID

T2+25ms Serving Plane    Call session.get(SID)
                         → Retrieve session, check CompleteIdempotencyKey = key-A-T0
                         → Compare body_hash ✓ match
                         → Return cached response: {order_id: ord_123, ...}

T2+30ms Serving Plane    Return 200 OK to agent (no connector call!)
                         {order_id: ord_123, ...}

────────────────────────────────────────────────────────────────────────

Result:  ✓ Same order ID (ord_123) returned
         ✓ No duplicate charge (Stripe API not called again)
         ✓ Audit log has 1 entry for key-A-T0 (mutex prevented double-record)
         ✓ Deterministic: can replay any time within 24h window
```

---

## 8. State Machine: Order Lifecycle

### 8.1 Valid States

```
OrderStatus:
  - created      (just placed at merchant)
  - confirmed    (merchant confirmed receipt)
  - shipped      (fulfillment initiated)
  - cancelled    (not proceeding, no refund)
  - refunded     (money returned to buyer)
```

### 8.2 Valid Transitions

```
created
  ├→ confirmed (with precondition: stock available)
  ├→ cancelled (with precondition: before payment capture)
  └→ refunded  (edge: refund on created order, rare)

confirmed
  ├→ shipped (normal fulfillment path)
  └→ refunded (refund after payment captured)

shipped
  └→ [TERMINAL - no outbound transitions]

cancelled
  └→ [TERMINAL]

refunded
  └→ [TERMINAL]
```

### 8.3 Enforcement

**Before any state transition**, check:

```go
ok, violations := CanTransition(order.Status, newStatus)
if !ok {
  return errors.New(violations[0])  // e.g., "cannot refund order already shipped"
}
```

**Preconditions** (examples):

- Refund: Order must be in `confirmed` or `shipped` state (not already `refunded`)
- Void: Order must be in `created` state (before capture)

---

## 9. Limitations & Trade-offs

### 9.1 Not Guaranteed

1. **Cross-service atomicity**
   - Inventory debit may succeed while order record fails (async eventual consistency)
   - Acceptable: retry with same key will converge eventually

2. **Automatic refunds**
   - Double-charge detected via audit log
   - Operator must manually issue refund (support ticket)

3. **Multi-order atomicity**
   - Batch orders (10 items) create 1 order, not 10 (by design)
   - Buyer can't roll back 5 items without refunding entire order

4. **Distributed tracing**
   - Audit log provides timestamps but not full request tracing
   - Recommendation: Correlation IDs in logs for multi-span queries

### 9.2 Design Choices

| Choice | Rationale |
|--------|-----------|
| 24h idempotency window | Balances safety (catch retries) vs. flexibility (garbage collection) |
| Scoped to merchant_id | Prevents cross-tenant key collisions; simpler than global UUID tracking |
| Defer to payment handler | Reduces latency (handler's idempotency is authoritative); simpler than dual-verification |
| MongoDB TTL, not Firestore | Simplifies implementation; existing Sarvex infrastructure |
| Order ID from merchant (v1) | Simple; uses existing order ID (merchant's source of truth). |

---

## 10. Migration & Rollout

### 10.1 Backwards Compatibility

- **No breaking changes** to HTTP API
  - `Idempotency-Key` header optional (graceful degredation without it)
  - New fields (`IdempotencyKey` in Order) non-breaking (nullable)

### 10.2 Gradual Rollout

1. **Phase 1-8** (in this commit):
   - Schema additions (Order.IdempotencyKey, etc.)
   - Storage layer (indexes, audit log)
   - State machine validator

### 10.3 Risk Mitigation

- **Monitor**: Audit log query counts (anomalies = double-charges)
- **Alert**: If same key appears with 2+ order IDs in 24h window
- **Runbook**: Established (IDEMPOTENCY-RUNBOOK.md) before rollout
- **Support**: Double-charge process documented for operators

---

## References

- **sarvex-v1-architecture.md**: Sarvex architecture invariants (A10.6 idempotency requirement)
- **sarvex-v1-architecture.md**: Three-plane design, atomicity assumptions
- **IDEMPOTENCY-RUNBOOK.md**: Operational guide for replay, double-charge detection
- **RFC 9421**: HTTP message signatures

---

**End of Design Document**
