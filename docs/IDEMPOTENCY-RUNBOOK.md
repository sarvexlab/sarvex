# Sarvex Idempotency & Exactly-Once Semantics Runbook

**Last Updated**: 2026-09-16  
**Status**: Active  
**Owner**: Sarvex maintainers

---

## Overview

This runbook covers:
1. **How to safely replay/retry an order** without duplicate charges
2. **How to detect if we double-charged** a merchant or buyer
3. **Debugging idempotency failures**
4. **Audit log queries** for forensics

---

## Part 1: Safely Replaying a Failed Order

### Scenario: Checkout completed at merchant but Sarvex didn't record it

**Symptoms**:
- Buyer received order confirmation from merchant
- Sarvex returned an error to the agent (e.g., "session persistence failed")
- Agent wants to retry without charging twice

### Steps to Replay

1. **Recover the original idempotency key**
   ```bash
   # From agent's request logs, find the checkout.complete request
   curl -X POST https://sarvex.example.com/checkout/{sessionId}/complete \
     -H "Idempotency-Key: ORIGINAL_KEY" \
     -H "Content-Type: application/json" \
     -d '{...}'
   ```

2. **Retry with the same Idempotency-Key header**
   ```bash
   # Resend the exact same request
   curl -X POST https://sarvex.example.com/checkout/{sessionId}/complete \
     -H "Idempotency-Key: ORIGINAL_KEY" \
     -H "Content-Type: application/json" \
     -d '{...}'
   ```

3. **Expect one of three outcomes**:

   **a) 200 OK with same order ID** (ideal)
   ```json
   {
     "order_id": "1726489232a1b2c3d4e5f6g7",
     "status": "completed"
   }
   ```
   → Order was already recorded. No new charge. This is idempotent replay success.

   **b) 409 Conflict** (body changed)
   ```json
   {
     "error": {
       "code": "IDEMPOTENCY_KEY_CONFLICT",
       "message": "Request body differs from prior attempt with same key"
     }
   }
   ```
   → Same key used with different cart/payment. Generate a NEW key and retry.

   **c) 202 Accepted** (async capture in progress)
   ```json
   {
     "escalation": {
       "status": "requires_escalation",
       "continue_url": "..."
     }
   }
   ```
   → Payment is async (3DS, soft decline). Buyer must complete off-band.

---

## Part 2: Detecting Double-Charges

### Query 1: All Mutations for an Idempotency Key

**Purpose**: Determine if the same key produced multiple charges.

```bash
# Via MongoDB query
db.sync_order_mutations.find({
  merchant_id: "MERCHANT_ID",
  idempotency_key: "ORIGINAL_KEY"
}).sort({ attempted_at: -1 })
```

**Expected output** (healthy):
```json
[
  {
    mutation_id: "...",
    idempotency_key: "key-xyz-123",
    order_id: "1726489232a1b2c3d4e5f6g7",
    attempted_at: "2026-09-16T10:30:00Z",
    completed_at: "2026-09-16T10:30:01Z",
    local_status: "paid",
    handler_response: { stripe_charge_id: "ch_..." }
  }
]
```

**Red flag** (double-charge):
```json
[
  {
    idempotency_key: "key-xyz-123",
    order_id: "1726489232a1b2c3d4e5f6g7",
    handler_response: { stripe_charge_id: "ch_AAA" }
  },
  {
    idempotency_key: "key-xyz-123",
    order_id: "1726489233a1b2c3d4e5f6g8",  // DIFFERENT order ID
    handler_response: { stripe_charge_id: "ch_BBB" }  // DIFFERENT charge
  }
]
```

→ **Action**: Contact payment processor support to refund `ch_BBB`. Investigate why retry created new order.

### Query 2: All Mutations for an Order

**Purpose**: Audit trail of every attempt to mutate a specific order.

```bash
db.sync_order_mutations.find({
  order_id: "1726489232a1b2c3d4e5f6g7"
}).sort({ attempted_at: 1 })
```

**Example output** (healthy lifecycle):
```json
[
  {
    mutation_type: "create",
    attempted_at: "2026-09-16T10:30:00Z",
    handler_response: { charge_id: "ch_123" },
    local_status: "paid"
  },
  {
    mutation_type: "create",  // replay
    attempted_at: "2026-09-16T10:30:02Z",
    handler_response: null,   // no handler call (cached)
    local_status: "paid"
  }
]
```

### Query 3: Orders by Merchant in the Last 24 Hours

**Purpose**: Spot-check for anomalies (duplicate payment_handler calls, failed captures).

```bash
db.sync_orders.find({
  merchant_id: "MERCHANT_ID",
  placed_at: { $gte: ISODate("2026-09-15T10:30:00Z") }
}).sort({ placed_at: -1 }).limit(100)
```

---

## Part 3: Debugging Idempotency Failures

### Problem: "Previous attempt failed before a backend checkout was created"

**Error**: `ErrIdempotentReplayIncomplete`

**Root cause**: Idempotency key exists but session never reached `BackendCheckoutID` state (connector call failed before response).

**Solution**:
```bash
# 1. Check session state
redis> get session:STORE_ID:SESSION_ID
# Look for BackendCheckoutID field — should be empty

# 2. Agent must generate a NEW idempotency key
# Cannot replay the same key because nothing safe to replay

# 3. Retry with new key
curl -X POST .../checkout/{sessionId}/complete \
  -H "Idempotency-Key: NEW_KEY" \
  -d '{...}'
```

### Problem: "Idempotency key already used by same merchant"

**Error**: `ErrIdempotencyKeyConflict`

**Root cause**: Same key, different request body (e.g., different line items).

**Solution**:
```bash
# 1. Verify the difference
redis> get idempotency:STORE_ID:ORIGINAL_KEY
# Returns SESSION_ID

redis> hgetall session:STORE_ID:SESSION_ID
# Compare line_items, payment fields

# 2. If changes are intentional, use a NEW key
curl -X POST .../checkout/{sessionId}/complete \
  -H "Idempotency-Key: NEW_KEY_WITH_BODY_HASH" \
  -d '{...}'

# 3. If changes are a mistake, retry with ORIGINAL key + ORIGINAL body
```

### Problem: "Order created at merchant but audit log missing"

**Symptom**: `sync_orders` has record but `sync_order_mutations` log is empty.

**Root cause**: Order was created before audit logging was added, or async event processing lagged.

**Solution**:
```bash
# 1. Check order state
db.sync_orders.findOne({ _id: "ORDER_ID" })

# 2. If order is in terminal state (paid, refunded, shipped), it's safe
# — existing data is grandfathered, audit trail starts going forward

# 3. If order is in non-terminal state (capturing), manually verify:
# Query payment handler (Stripe, etc.) for charge confirmation
# if needed to resolve disputes
```

---

## Part 4: Operational Guarantees

### What is guaranteed?

1. **Exact-Once**: Same idempotency key → same order ID, no duplicate charge
   - Within 24h of order placement: guaranteed idempotent
   - After 24h: key may be reused (new order created)

2. **Deterministic**: Replay always produces the same outcome
   - Same key + same merchant → same order ID
   - Same key + different body → 409 Conflict (no charge)

3. **Atomic**: Order record + handler response commit together
   - Or neither commits (handler succeeds, local record fails → order exists at merchant, Sarvex retries)

4. **Observable**: Every mutation logged
   - Audit trail: `sync_order_mutations` collection
   - Queryable by: idempotency key, order ID, merchant ID, timestamp

### What is NOT guaranteed?

1. **No cross-service atomicity**: Inventory debit may happen without order record (async eventual consistency)
2. **No automatic refunds**: Double-charge detected in audit log must be manually refunded
3. **No magic recovery**: Failed checkout requires agent retry with same key
4. **No multi-order atomicity**: Buying N items creates 1 order (not N orders with one-per-item)

---

## Part 5: Best Practices

### For Agents (LLMs, ChatGPT, etc.)

1. **Generate idempotency key once per checkout flow**
   ```
   Key should be deterministic from: merchant_id + session_id + timestamp
   Example: sha256(merchant_xyz:sess_abc:2026-09-16T10:30:00Z)[0:32]
   ```

2. **Persist the key client-side** (agent memory, conversation state)
   ```
   "Idempotency-Key: key-from-step-1"
   ```

3. **On 5xx errors, retry with same key**
   ```
   if status >= 500:
       wait(1s)
       retry(same_request, same_idempotency_key)
   ```

4. **On 409 Conflict, use NEW key**
   ```
   if status == 409:
       generate_new_key()
       retry(with_new_key)
   ```

### For Merchants (WooCommerce, Magento)

1. **Inject idempotency key into payment handler calls**
   ```php
   // In WooCommerce plugin
   $headers = [
       'Idempotency-Key' => $sarvex_request['idempotency_key'],
   ];
   $stripe_response = $stripe_client->charges->create($charge_data, $headers);
   ```

2. **Never strip or modify the key**
   ```php
   // Bad:
   $modified_key = hash('sha256', $key);  // Don't rehash
   
   // Good:
   // Pass through as-is to payment processor
   ```

3. **Log handler response with key**
   ```php
   error_log("Charge {$stripe_response->id} for key {$sarvex_key}");
   ```

### For Operators (Support, DevOps)

1. **Audit log is your friend**
   - Every mutation is recorded with timestamp, handler response, status
   - Use it to answer "did we charge twice?" immediately

2. **24h is the idempotency window**
   - After 24h, the same key may create a new order (acceptable, auditable)
   - Communicate this to buyers: "retry within 24h for idempotency guarantee"

3. **Monitor for key reuse**
   ```bash
   # Alert if same key appears with 2+ different order IDs in last 24h
   db.sync_order_mutations.aggregate([{
     $match: { attempted_at: { $gte: 24.hours.ago } }
   }, {
     $group: {
       _id: "$idempotency_key",
       order_count: { $sum: 1 },
       orders: { $push: "$order_id" }
     }
   }, {
     $match: { order_count: { $gt: 1 } }
   }])
   ```

---

## Part 6: Troubleshooting Checklist

| Symptom | Check | Action |
|---------|-------|--------|
| Buyer charged twice | Query `sync_order_mutations` by key. If 2+ charges, check handler response | Refund second charge at processor |
| Agent got error but order exists | Query `sync_orders` by session_id. Order should be there. | Retry with same key (idempotent) |
| Idempotency key shows 409 Conflict | Compare body hash of both requests. If different, conflict is correct | Use NEW key |
| Audit log empty for order | Check if order pre-dates audit feature. If recent, check async lag | Manual handler verification if needed |
| Session has order_id, but order not in MongoDB | Sync plane lag. Wait 30s and recheck. | Monitor async event backlog |

---

## Part 7: Example: End-to-End Replay

**Scenario**: Buyer's agent submitted checkout, merchant confirmed order, Sarvex failed to respond.

```bash
# Step 1: Agent has the idempotency key from original request
$ idempotency_key="key-buyer-alice-2026-09-16T10:30:00Z"
$ session_id="sess_xyz789"
$ merchant_id="merchant_acme"

# Step 2: Check if order was already created (via audit)
$ mongo
  db.sync_order_mutations.find({
    merchant_id: "merchant_acme",
    idempotency_key: idempotency_key
  })
  # Returns: [{ order_id: "17264892...", local_status: "paid", ... }]
  # Order exists! Replay is safe.

# Step 3: Retry with same key (idempotent)
$ curl -X POST https://sarvex/checkout/$session_id/complete \
    -H "Idempotency-Key: $idempotency_key" \
    -H "Content-Type: application/json" \
    -d '{ ... same body as original ... }'
  # Returns: 200 OK { order_id: "17264892...", status: "completed" }
  # ✓ Same order ID, same charge. Idempotent replay succeeded.

# Step 4: Verify no double charge
$ db.sync_order_mutations.find({
    merchant_id: "merchant_acme",
    idempotency_key: idempotency_key
  }).count()
  # Returns: 1 (only one charge)
```

---

## Contacts & Escalation

- **Idempotency bugs**: File in `sarvex.cp#idempotency` project
- **Double-charge incident**: Notify payment team + merchant support
- **Audit log queries**: Contact platform engineer
- **Handler integration**: Connector team (WooCommerce, Magento, etc.)

---

## Appendix: Database Schema Reference

### sync_order_mutations (Audit Log)

```json
{
  "_id": "ObjectId",
  "mutation_id": "uuid",
  "merchant_id": "string",
  "order_id": "string",
  "idempotency_key": "string",
  "attempted_at": "ISO8601",
  "completed_at": "ISO8601 (nullable)",
  "mutation_type": "create|capture|refund|void",
  "handler_response": {},
  "local_status": "paid|capturing|refunded|failed",
  "error_message": "string (nullable)",
  "retry_count": "int"
}
```

### sync_orders (Order Records)

```json
{
  "_id": "order_id",
  "merchant_id": "string",
  "store_id": "string",
  "idempotency_key": "string",
  "status": "created|confirmed|shipped|cancelled|refunded",
  "sync_state": "created|synced|failed",
  "value_minor": "int64",
  "currency": "string",
  "placed_at": "ISO8601",
  "mutation_attempts": [
    {
      "attempted_at": "ISO8601",
      "idempotency_key": "string",
      "handler_response": {},
      "local_status": "string"
    }
  ]
}
```

---

**End of Runbook**
