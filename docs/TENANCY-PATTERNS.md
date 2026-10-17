# Tenancy Enforcement Patterns

## Overview

Sarvex operates a two-level tenancy model:
- **Account data**: partitioned by `merchant_id`
- **Catalog/serving data**: partitioned by `store_id` (each store belongs to exactly one merchant)

Tenant isolation is **non-negotiable** and enforced via query-level fencing, not handler-level checks alone.

## The Pattern: Query-Level Fencing

Every repository method that accepts a tenant identifier (`merchantID` or `storeID`) **must include it as a Mongo query filter**. This is mandatory, not optional.

### Why Query-Level Fencing?

Handler-level authorization checks alone are insufficient:
- They fail open if a call site forgets to validate
- They don't protect non-HTTP callers (event consumers, background jobs)
- Tenant isolation becomes a property of "did the reviewer remember to check?" rather than a property of the database query

Query-level fencing is structurally the strongest: an unscoped Mongo query literally cannot find a document under the wrong tenant key. This makes tenant isolation a database-level guarantee, not a review-time assumption.

## Implementation Requirements

### For Repository Methods

```go
// ❌ WRONG: accepts merchantID but omits from filter
func (r *mongoRepo) GetAccount(ctx context.Context, merchantID string) (*Account, error) {
  var acc Account
  err := r.col.FindOne(ctx, bson.M{}).Decode(&acc) // BUG: no merchant_id filter
  return &acc, err
}

// ✅ CORRECT: includes merchantID in the Mongo query filter
func (r *mongoRepo) GetAccount(ctx context.Context, merchantID string) (*Account, error) {
  var acc Account
  err := r.col.FindOne(ctx, bson.M{"_id": merchantID}).Decode(&acc)
  return &acc, err
}

// ✅ CORRECT: compound key for store-scoped data
func (r *mongoRepo) GetCatalog(ctx context.Context, storeID, merchantID string) (*Catalog, error) {
  var cat Catalog
  err := r.col.FindOne(ctx, bson.M{"_id": storeID, "merchant_id": merchantID}).Decode(&cat)
  return &cat, err
}
```

### For Aggregate/List Queries

When query logic requires `$or`-style operations, still enforce tenant scoping:

```go
// ✅ CORRECT: merchant_id is AND'ed across all branches
func (r *mongoRepo) FindByStatus(ctx context.Context, merchantID string, statuses []string) ([]*Account, error) {
  filter := bson.M{
    "merchant_id": merchantID,  // Non-negotiable
    "status": bson.M{
      "$in": statuses,
    },
  }
  // ... cursor, decode, return
}

// ✅ CORRECT: if using $or, tenant scope still applies
func (r *mongoRepo) FindByNameOrEmail(ctx context.Context, merchantID string, nameOrEmail string) ([]*Account, error) {
  filter := bson.M{
    "merchant_id": merchantID,  // AND'ed with the $or
    "$or": []bson.M{
      {"name": nameOrEmail},
      {"email": nameOrEmail},
    },
  }
  // ... cursor, decode, return
}
```

### For Redis Access

Redis operations should follow the same principle: tenant scope must be part of the key structure:

```go
// ✅ CORRECT: merchantID is part of the key
func (r *redisCache) Get(ctx context.Context, merchantID, key string) ([]byte, error) {
  return r.client.Get(ctx, fmt.Sprintf("merchant:%s:%s", merchantID, key)).Bytes()
}
```

### Handler-Level Defense-in-Depth (Optional)

Handlers may perform additional authorization checks as a second line of defense, but **query-level fencing is the primary safeguard**:

```go
// ✅ OKAY: handler-level check provides defense-in-depth, but query filter is mandatory
func (h *handler) GetAccount(c echo.Context) error {
  principal, _ := c.Get("principal").(domain.Principal)  // Verify merchant
  accountID := c.Param("account_id")
  
  // Query already includes merchant_id filter — handler check is optional defense-in-depth
  account, err := h.repo.GetAccount(c.Request().Context(), string(principal.MerchantID), accountID)
  if err != nil {
    return c.JSON(http.StatusNotFound, nil)
  }
  
  // Additional check is optional but encouraged
  if account.MerchantID != principal.MerchantID {
    return c.JSON(http.StatusForbidden, nil)
  }
  
  return c.JSON(http.StatusOK, account)
}
```

## Code Review Checklist

When reviewing repository code, flag these patterns:

- **Repository method signature includes `merchantID` or `storeID` parameter → must appear in Mongo filter**
- **List/aggregate queries → tenant scope must be AND'ed in filter, never omitted**
- **Handler-level-only authorization** → flag as missing query-level fencing
- **Compound keys → both merchant_id AND store_id required for store-scoped data**

## Reference

See `docs/adr/0005-tenancy-enforcement-via-query-level-fencing.md` (ADR-2) for the decision rationale and consequences.
