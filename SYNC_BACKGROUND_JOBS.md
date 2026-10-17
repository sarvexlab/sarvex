# Sync Plane — Background Jobs

Analysis of every background goroutine/timer running in `internal/planes/sync`.

---

## 1. SweepScheduler — `sweep_scheduler.go`

**What:** Leader-elected loop that fires a full per-store reconciliation sweep on a configurable interval (`defaultSweepInterval`).

**Pattern:** Single goroutine with two tickers — `electTicker` (renews the distributed lease) and `sweepTicker` (fires sweep cycles). `runSweepCycle` is a no-op unless this instance currently holds the `"scheduler:leader"` lease.

**Multi-instance safe:** Yes. `concurrency.Lease.TryAcquire` is a distributed compare-and-set; only one instance holds the lease at a time. Non-leaders keep their election loop running for fast failover (gap bounded by `leaseTTL`, not just `renewInterval`).

---

## 2. CatalogImportHandler goroutines — `catalog_import.go`

**What:** On-demand goroutines, one per in-flight catalog import job. Spawned in two places:
- `HandleCommand` — when a `control.catalog.import` event arrives.
- `ResumeIncomplete` — at startup, to resume any jobs left `running` by a prior crashed instance.

**Pattern:** `wg.Add(1); go func()` per job, all cancelled via a shared `runCtx`. `Stop()` cancels `runCtx` and blocks on `wg.Wait()`.

**Multi-instance safe:** Mostly. Before spawning, `HandleCommand` checks the DB for an existing `running` job and refuses to start a second concurrent import for the same store. However, `ResumeIncomplete` runs at startup on every instance without a distributed guard — if two instances start simultaneously and both read the same `running` jobs before either has progressed, both may spawn goroutines for the same store.

---

## 3. Surface artifact runner — `surface/runner.go`

**What:** Regenerates each enrolled store's surface artifact: per-store `time.AfterFunc` debounce timers (20 s after the last catalog change) plus an hourly safety-net ticker (`RunSafetyNet`) that backstops missed events. Only does work for surfaces that register a `Generator` in `surface/all`; the bundled `direct_agent` surface registers none.

**Pattern:** `sync.Mutex`-guarded `map[string]*time.Timer`; raw `time.NewTicker(1h)` goroutine.

**Multi-instance safe: No.** Every instance keeps its own timers and ticker, so with N instances each artifact is regenerated up to N times per burst/hour. Regeneration is idempotent (stable token, deterministic output), so this costs work, not correctness.

---

## Multi-instance summary

| Job | Leader-elected / sharded | Safe with N instances |
|---|---|---|
| Sweep scheduler | Yes — distributed lease | Yes |
| Surface artifact runner (debounce + safety net) | No | Correct but N × regenerations |
| Catalog import goroutines | DB-guarded (partial) | Mostly; startup race edge case |

### Multi-instance behaviour of the surface artifact runner

The surface artifact runner is not leader-elected, so with N instances each regenerates (N × regenerations); the result is correct because artifact writes are idempotent. The sweep scheduler, by contrast, holds the `"scheduler:leader"` lease.
