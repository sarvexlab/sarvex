# Tenancy enforcement via query-level fencing with defense-in-depth

Merchant and store data isolation is enforced at the database query level, not at the handler level alone. Every repository method that accepts a tenant identifier (`merchantID` or `storeID`) must include it in the actual Mongo query filter; handler-level authorization checks serve as defense-in-depth.

Sarvex operates a two-level tenancy model: account data is partitioned by `merchant_id`, and catalog/serving data by `store_id`. Violating tenant isolation is the repository's most severe risk class. Two coexisting enforcement strategies were observed during audit: query-level fencing (compound Mongo keys, fails closed) and post-fetch handler-level validation (fails open if a call site forgets it). The audit found instances of the handler-only pattern that leaked cross-tenant data (F-007/F-008), proving that handler-level checks are insufficient in review.

Query-level fencing is structurally the strongest: an unscoped Mongo query literally cannot find a document under the wrong tenant key. This makes tenant isolation a property of the database schema and query, not a property of whether reviewers remembered to add a check.

## Considered options

**Handler-level fencing only**: Centralize the authorization check in a single middleware or helper function invoked at the top of every store-scoped handler, with repository-layer scoping as defense-in-depth. Easier to audit for completeness in a single place, but doesn't protect non-HTTP callers (event consumers, background jobs) and fails open if a call site forgets it.

**Query-level fencing only**: Ignore handler-level checks and rely solely on Mongo query filters. Strongest structurally, but occasionally awkward for aggregate/list queries that need `$or`-style logic instead of a simple compound key.

## Consequences

- Every repository method taking a `merchantID` or `storeID` parameter must include it as a query filter clause, not merely accept it as a parameter. This is a lint/review checklist item.
- Query-level fencing is the non-negotiable requirement; handler-level authorization checks (following the pattern established in `getStoreProfile`) are optional but encouraged as defense-in-depth.
- Existing repository methods that accept tenant identifiers but omit them from the query filter are a breaking remediation and must be corrected during the F-007/F-008 remediation phase.
- This pattern applies to all data layers: MongoDB repositories, Redis accesses, and any other tenant-scoped data source.
