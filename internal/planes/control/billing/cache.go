// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package billing

import (
	"context"
	"sync"
	"time"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// DefaultPlanCacheTTL is how long a CachedPlanReader trusts a billing record
// before re-fetching it. Plan is chosen deliberately generous: unlike
// billing status or usage, which the console surfaces directly and expects
// to be current, a merchant's plan changes rarely (a Stripe portal
// self-serve upgrade, at most a few times a year) and its only consumers so
// far are task-owner decisions (validation.Orchestrator.milestoneOwner,
// monitoring.TaskServiceRouter.warningOwner) that tolerate a few minutes of
// staleness.
const DefaultPlanCacheTTL = 5 * time.Minute

// DefaultBillingStatusCacheTTL is how long a CachedPlanReader trusts a
// billing record when it backs requireActiveBilling — the write gate that
// runs on nearly every mutating console/API request. Unlike plan tier,
// billing status is user-facing (a merchant who just fixed a card in the
// Stripe portal expects write access back promptly), so this TTL is kept
// short: long enough to absorb the request bursts a single console session
// generates, short enough that the gate catches up to a real status change
// within seconds rather than minutes.
const DefaultBillingStatusCacheTTL = 15 * time.Second

// PlanReader is the minimal billing lookup CachedPlanReader wraps — narrow
// enough that it's satisfied by Service without restating Service's other
// methods, and by another CachedPlanReader if one were ever layered.
type PlanReader interface {
	Get(ctx context.Context, id domain.MerchantID) (domain.Billing, error)
}

// planCacheEntry is one merchant's cached billing record.
type planCacheEntry struct {
	billing   domain.Billing
	expiresAt time.Time
}

// CachedPlanReader decorates a PlanReader (Service in production) with an
// in-memory, per-process TTL cache, so the read-heavy consumers that only
// need a merchant's plan tier — one per prescribed task raised, which can
// mean once per monitoring probe pass, every few minutes, per store — don't
// each round-trip Mongo for a value that is effectively static.
//
// Per-process and unbounded: a control-plane replica restart or redeploy
// simply starts cold, and the merchant count this serves (V1: one store per
// merchant, no cross-tenant fan-out) never approaches a size where an
// unbounded map is a concern. If that changes, this is the seam to add
// eviction.
//
// A plan change (Stripe portal upgrade/downgrade, synced via
// HandleStripeWebhook) is not pushed into the cache — it is simply not
// visible here until the entry expires. That is an accepted tradeoff for the
// TTL chosen: see DefaultPlanCacheTTL.
type CachedPlanReader struct {
	source PlanReader
	ttl    time.Duration
	cache  sync.Map // domain.MerchantID -> planCacheEntry
}

// NewCachedPlanReader wraps source with a TTL cache. Share one instance
// across every consumer that only needs plan (rather than constructing one
// per consumer), so they draw on the same cache instead of each keeping —
// and independently missing — their own.
func NewCachedPlanReader(source PlanReader, ttl time.Duration) *CachedPlanReader {
	return &CachedPlanReader{source: source, ttl: ttl}
}

// Get returns id's billing record, from cache if a fresh entry exists,
// otherwise from source — populating the cache on the way back. A failed
// source lookup is never cached, so a transient Mongo error doesn't wedge
// every caller into the fallback-owner path (validation and monitoring both
// default to sarvex when this errors) until the TTL happens to expire.
func (c *CachedPlanReader) Get(ctx context.Context, id domain.MerchantID) (domain.Billing, error) {
	if v, ok := c.cache.Load(id); ok {
		entry := v.(planCacheEntry)
		if time.Now().Before(entry.expiresAt) {
			return entry.billing, nil
		}
		c.cache.Delete(id)
	}

	b, err := c.source.Get(ctx, id)
	if err != nil {
		return domain.Billing{}, err
	}

	c.cache.Store(id, planCacheEntry{billing: b, expiresAt: time.Now().Add(c.ttl)})
	return b, nil
}

var _ PlanReader = (*CachedPlanReader)(nil)
