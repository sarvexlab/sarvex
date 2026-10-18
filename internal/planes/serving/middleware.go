// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/negotiation"
)

// RateLimiter enforces per-merchant per-endpoint rate limits. MemoryRateLimiter
// (this file, in-process — tests and single-replica dev only) and
// RedisRateLimiter (redis_ratelimiter.go, shared across replicas — the
// production implementation) both implement it: a serving plane scaled to
// N replicas needs every replica enforcing the SAME bucket, not N
// independent ones, or effective per-merchant quotas silently multiply by N.
type RateLimiter interface {
	Allowed(ctx context.Context, merchantID, endpoint string) bool
}

// endpointLimits maps endpoint -> requests/sec, shared by every RateLimiter
// implementation so the limits themselves don't drift between them.
var endpointLimits = map[string]float64{
	"GET /.well-known/ucp":                 100.0, // 100 req/sec per merchant
	"POST /catalog/search":                 100.0,
	"GET /catalog/:sku":                    100.0,
	"POST /checkout-sessions":              50.0, // 50 req/sec per merchant
	"GET /checkout-sessions/:id":           50.0,
	"PUT /checkout-sessions/:id":           50.0,
	"POST /checkout-sessions/:id/complete": 50.0,
	// A surface fetcher reads a feed a few times a day; this is headroom, and
	// a ceiling for anything probing for tokens.
	ArtifactEndpoint: 5.0, // per token and per client address
}

const defaultRateLimit = 100.0 // requests/sec, for any endpoint not in endpointLimits

// MemoryRateLimiter tracks per-merchant per-endpoint token buckets
// in-process. Only correct for a single serving replica — see RateLimiter's
// doc comment — so it's used by tests and `make dev-up` (one replica), and
// NewRedisRateLimiter is used everywhere a real deployment might scale out.
type MemoryRateLimiter struct {
	mu      sync.RWMutex
	buckets map[string]*TokenBucket
	log     *slog.Logger
}

var _ RateLimiter = (*MemoryRateLimiter)(nil)

// TokenBucket tracks refillable token state.
type TokenBucket struct {
	tokens   float64
	capacity float64
	rate     float64 // tokens/sec
	lastFill time.Time
}

// NewRateLimiter creates an in-process per-merchant per-endpoint rate
// limiter. See MemoryRateLimiter's doc comment for its single-replica limitation.
func NewRateLimiter(log *slog.Logger) *MemoryRateLimiter {
	return &MemoryRateLimiter{
		buckets: make(map[string]*TokenBucket),
		log:     log,
	}
}

// Allowed returns true if the merchant/endpoint is within rate limit. ctx is
// unused (the in-process map has no I/O to cancel) — it's part of the
// RateLimiter interface only because RedisRateLimiter needs it.
func (rl *MemoryRateLimiter) Allowed(_ context.Context, merchantID, endpoint string) bool {
	key := merchantID + ":" + endpoint

	rl.mu.Lock()
	defer rl.mu.Unlock()

	bucket, exists := rl.buckets[key]
	if !exists {
		rate, ok := endpointLimits[endpoint]
		if !ok {
			rate = defaultRateLimit
		}
		bucket = &TokenBucket{
			tokens:   rate,
			capacity: rate,
			rate:     rate,
			lastFill: time.Now(),
		}
		rl.buckets[key] = bucket
	}

	// Refill based on elapsed time
	now := time.Now()
	elapsed := now.Sub(bucket.lastFill).Seconds()
	bucket.tokens = min(bucket.capacity, bucket.tokens+elapsed*bucket.rate)
	bucket.lastFill = now

	if bucket.tokens >= 1 {
		bucket.tokens -= 1
		return true
	}
	if rl.log != nil {
		rl.log.Warn("rate limit exceeded", "merchant_id", merchantID, "endpoint", endpoint)
	}
	return false
}

// WithRateLimiting middleware enforces per-merchant per-endpoint rate limits.
func WithRateLimiting(limiter RateLimiter) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			instance, ok := negotiation.GetStoreProfile(c)
			if !ok {
				return echo.NewHTTPError(http.StatusInternalServerError, "instance not loaded")
			}

			endpoint := fmt.Sprintf("%s %s", c.Request().Method, c.Path())
			merchantID := instance.Configuration().MerchantID
			if !limiter.Allowed(c.Request().Context(), merchantID, endpoint) {
				return echo.NewHTTPError(http.StatusTooManyRequests, "rate limit exceeded")
			}

			return next(c)
		}
	}
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
