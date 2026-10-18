// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package resilience provides a per-key (typically per-store) execution
// guard combining retry-with-backoff, a circuit breaker, and a token-bucket
// rate limiter. It is deliberately generic — no knowledge of WooCommerce,
// Magento, or any other backend — so every store-API connector can share one
// implementation instead of re-deriving retry/breaker/limiter logic per
// platform.
//
// State (breaker, limiter) is scoped per key: a failing or throttled store
// never affects another store's calls through the same Guard instance.
package resilience

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/sony/gobreaker"
	"golang.org/x/time/rate"
)

// Priority distinguishes latency-sensitive single-entity calls from bulk
// work, so bulk work can yield the rate-limit budget to priority calls.
type Priority int

const (
	// PriorityBulk marks large/background work (e.g. a full catalog page
	// fetch). Bulk calls wait while any PriorityHigh call is in flight.
	PriorityBulk Priority = iota
	// PriorityHigh marks latency-sensitive single-entity calls (e.g. a
	// live price check). High-priority calls are never delayed by bulk
	// work.
	PriorityHigh
)

// ErrCircuitOpen is returned (wrapped) when a key's circuit breaker is open
// or in half-open state with no trial slots available.
var ErrCircuitOpen = errors.New("resilience: circuit open")

// Config configures a Guard. Zero-value fields fall back to sane defaults
// via NewGuard.
type Config struct {
	// MaxRetries is the number of retry attempts after the first try. Zero
	// (the Go zero value, i.e. left unset) uses DefaultMaxRetries. To
	// explicitly disable retries, set a negative value.
	MaxRetries int
	// BaseBackoff is the initial retry delay; it grows exponentially.
	BaseBackoff time.Duration
	// MaxBackoff caps the exponential backoff delay.
	MaxBackoff time.Duration

	// BreakerFailureThreshold is the number of consecutive failures that
	// trips the breaker open for a key.
	BreakerFailureThreshold uint32
	// BreakerCooldown is how long the breaker stays open before allowing
	// a half-open trial request.
	BreakerCooldown time.Duration

	// RatePerSecond is the sustained request rate allowed per key.
	RatePerSecond float64
	// RateBurst is the token bucket burst size per key.
	RateBurst int
	// BulkYieldInterval is how often a waiting bulk call re-checks
	// whether high-priority calls are still in flight.
	BulkYieldInterval time.Duration
}

// DefaultMaxRetries is used when Config.MaxRetries is left unset (0).
const DefaultMaxRetries = 3

func (c Config) withDefaults() Config {
	switch {
	case c.MaxRetries == 0:
		c.MaxRetries = DefaultMaxRetries
	case c.MaxRetries < 0:
		c.MaxRetries = 0
	}
	if c.BaseBackoff <= 0 {
		c.BaseBackoff = 200 * time.Millisecond
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = 5 * time.Second
	}
	if c.BreakerFailureThreshold <= 0 {
		c.BreakerFailureThreshold = 5
	}
	if c.BreakerCooldown <= 0 {
		c.BreakerCooldown = 30 * time.Second
	}
	if c.RatePerSecond <= 0 {
		c.RatePerSecond = 5
	}
	if c.RateBurst <= 0 {
		c.RateBurst = 5
	}
	if c.BulkYieldInterval <= 0 {
		c.BulkYieldInterval = 10 * time.Millisecond
	}
	return c
}

// Guard executes calls behind a per-key retry+breaker+rate-limit stack.
type Guard struct {
	cfg Config

	mu       sync.Mutex
	breakers map[string]*gobreaker.CircuitBreaker
	limiters map[string]*keyedLimiter
}

// NewGuard creates a Guard. An empty Config uses documented defaults.
func NewGuard(cfg Config) *Guard {
	return &Guard{
		cfg:      cfg.withDefaults(),
		breakers: make(map[string]*gobreaker.CircuitBreaker),
		limiters: make(map[string]*keyedLimiter),
	}
}

type keyedLimiter struct {
	tokens *rate.Limiter

	mu                   sync.Mutex
	highPriorityInFlight int
}

// Do runs fn for the given key: waits for rate-limit budget (yielding to any
// concurrent high-priority call if this is bulk priority), then executes fn
// through that key's circuit breaker, retrying transient failures with
// exponential backoff. isRetryable classifies whether a given error from fn
// should be retried; nil means "retry every error".
//
// A PriorityHigh call is marked "in flight" for the entire duration of this
// method — rate-limit wait, breaker, retries, and fn — so a concurrent
// PriorityBulk call for the same key yields for as long as the priority call
// is actually running, not just while it is queued.
//
// Returns the last error from fn on exhausted retries, or an error wrapping
// ErrCircuitOpen if the breaker rejected the call outright.
func (g *Guard) Do(ctx context.Context, key string, priority Priority, isRetryable func(error) bool, fn func(ctx context.Context) error) error {
	kl := g.limiterFor(key)

	if priority == PriorityHigh {
		kl.mu.Lock()
		kl.highPriorityInFlight++
		kl.mu.Unlock()
		defer func() {
			kl.mu.Lock()
			kl.highPriorityInFlight--
			kl.mu.Unlock()
		}()
	}

	if err := g.acquireRate(ctx, kl, priority); err != nil {
		return err
	}

	cb := g.breakerFor(key)
	_, err := cb.Execute(func() (interface{}, error) {
		return nil, g.retry(ctx, isRetryable, fn)
	})
	if err != nil {
		if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
			return fmt.Errorf("%w: store %s: %w", ErrCircuitOpen, key, err)
		}
		return err
	}
	return nil
}

func (g *Guard) retry(ctx context.Context, isRetryable func(error) bool, fn func(ctx context.Context) error) error {
	expo := backoff.NewExponentialBackOff()
	expo.InitialInterval = g.cfg.BaseBackoff
	expo.MaxInterval = g.cfg.MaxBackoff
	expo.MaxElapsedTime = 0 // bounded by MaxRetries, not wall-clock time

	b := backoff.WithContext(backoff.WithMaxRetries(expo, uint64(g.cfg.MaxRetries)), ctx)

	return backoff.Retry(func() error {
		err := fn(ctx)
		if err == nil {
			return nil
		}
		if isRetryable != nil && !isRetryable(err) {
			return backoff.Permanent(err)
		}
		return err
	}, b)
}

func (g *Guard) breakerFor(key string) *gobreaker.CircuitBreaker {
	g.mu.Lock()
	defer g.mu.Unlock()

	if cb, ok := g.breakers[key]; ok {
		return cb
	}
	cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:        key,
		MaxRequests: 1,
		Timeout:     g.cfg.BreakerCooldown,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= g.cfg.BreakerFailureThreshold
		},
	})
	g.breakers[key] = cb
	return cb
}

func (g *Guard) limiterFor(key string) *keyedLimiter {
	g.mu.Lock()
	defer g.mu.Unlock()

	if kl, ok := g.limiters[key]; ok {
		return kl
	}
	kl := &keyedLimiter{tokens: rate.NewLimiter(rate.Limit(g.cfg.RatePerSecond), g.cfg.RateBurst)}
	g.limiters[key] = kl
	return kl
}

// acquireRate waits for this key's rate-limit budget. A PriorityBulk caller
// first waits for any PriorityHigh call on the same key to finish (see Do),
// re-checking every BulkYieldInterval, before consuming a rate-limit token.
func (g *Guard) acquireRate(ctx context.Context, kl *keyedLimiter, priority Priority) error {
	if priority == PriorityBulk {
		for {
			kl.mu.Lock()
			inFlight := kl.highPriorityInFlight
			kl.mu.Unlock()
			if inFlight == 0 {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(g.cfg.BulkYieldInterval):
			}
		}
	}

	return kl.tokens.Wait(ctx)
}
