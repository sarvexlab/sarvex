// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package resilience_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/resilience"
)

func alwaysRetryable(error) bool { return true }

func TestGuard_RetriesTransientFailuresThenSucceeds(t *testing.T) {
	g := resilience.NewGuard(resilience.Config{
		MaxRetries:    5,
		BaseBackoff:   time.Millisecond,
		MaxBackoff:    5 * time.Millisecond,
		RatePerSecond: 1000,
		RateBurst:     1000,
	})

	var attempts int32
	err := g.Do(context.Background(), "store-a", resilience.PriorityHigh, alwaysRetryable, func(ctx context.Context) error {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected eventual success, got %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestGuard_ExhaustsRetriesAndReturnsTypedError(t *testing.T) {
	g := resilience.NewGuard(resilience.Config{
		MaxRetries:              10, // higher than breaker threshold so we hit the breaker first is avoided
		BaseBackoff:             time.Millisecond,
		MaxBackoff:              time.Millisecond,
		BreakerFailureThreshold: 100, // effectively disable breaker for this test
		RatePerSecond:           1000,
		RateBurst:               1000,
	})

	wantErr := errors.New("permanent failure")
	var attempts int32
	err := g.Do(context.Background(), "store-b", resilience.PriorityHigh, alwaysRetryable, func(ctx context.Context) error {
		atomic.AddInt32(&attempts, 1)
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped wantErr, got %v", err)
	}
	if int(attempts) != 11 { // 1 initial + 10 retries
		t.Fatalf("expected 11 attempts, got %d", attempts)
	}
}

func TestGuard_NonRetryableErrorFailsImmediately(t *testing.T) {
	g := resilience.NewGuard(resilience.Config{
		MaxRetries:    5,
		BaseBackoff:   time.Millisecond,
		RatePerSecond: 1000,
		RateBurst:     1000,
	})

	wantErr := errors.New("not found")
	var attempts int32
	err := g.Do(context.Background(), "store-c", resilience.PriorityHigh, func(error) bool { return false }, func(ctx context.Context) error {
		atomic.AddInt32(&attempts, 1)
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wantErr, got %v", err)
	}
	if attempts != 1 {
		t.Fatalf("expected exactly 1 attempt (no retry), got %d", attempts)
	}
}

func TestGuard_CircuitOpensAfterThresholdAndRecoversAfterCooldown(t *testing.T) {
	g := resilience.NewGuard(resilience.Config{
		MaxRetries:              -1, // disable retries: isolate breaker behavior from retry behavior
		BreakerFailureThreshold: 2,
		BreakerCooldown:         30 * time.Millisecond,
		RatePerSecond:           1000,
		RateBurst:               1000,
	})

	failing := func(ctx context.Context) error { return errors.New("boom") }

	// Two consecutive failures trip the breaker.
	for i := 0; i < 2; i++ {
		if err := g.Do(context.Background(), "store-d", resilience.PriorityHigh, alwaysRetryable, failing); err == nil {
			t.Fatalf("attempt %d: expected failure", i)
		}
	}

	// Next call should be rejected by the open breaker without invoking fn.
	var invoked bool
	err := g.Do(context.Background(), "store-d", resilience.PriorityHigh, alwaysRetryable, func(ctx context.Context) error {
		invoked = true
		return nil
	})
	if invoked {
		t.Fatal("fn should not be invoked while circuit is open")
	}
	if !errors.Is(err, resilience.ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}

	// After cooldown, a half-open trial should be allowed through and, on
	// success, close the circuit again.
	time.Sleep(50 * time.Millisecond)
	invoked = false
	err = g.Do(context.Background(), "store-d", resilience.PriorityHigh, alwaysRetryable, func(ctx context.Context) error {
		invoked = true
		return nil
	})
	if err != nil {
		t.Fatalf("expected recovery after cooldown, got %v", err)
	}
	if !invoked {
		t.Fatal("expected fn to be invoked after cooldown (half-open trial)")
	}
}

func TestGuard_CircuitStateIsPerKey(t *testing.T) {
	g := resilience.NewGuard(resilience.Config{
		MaxRetries:              -1, // disable retries
		BreakerFailureThreshold: 1,
		BreakerCooldown:         time.Hour, // never recovers within test
		RatePerSecond:           1000,
		RateBurst:               1000,
	})

	failing := func(ctx context.Context) error { return errors.New("boom") }
	if err := g.Do(context.Background(), "store-a", resilience.PriorityHigh, alwaysRetryable, failing); err == nil {
		t.Fatal("expected failure to trip store-a's breaker")
	}

	// store-a's breaker is now open; store-b must be unaffected.
	var invoked bool
	err := g.Do(context.Background(), "store-b", resilience.PriorityHigh, alwaysRetryable, func(ctx context.Context) error {
		invoked = true
		return nil
	})
	if err != nil {
		t.Fatalf("store-b call should succeed independently of store-a, got %v", err)
	}
	if !invoked {
		t.Fatal("expected store-b's fn to be invoked")
	}
}

func TestGuard_RateLimiterCapsRequestRate(t *testing.T) {
	g := resilience.NewGuard(resilience.Config{
		RatePerSecond: 5, // 5 req/s
		RateBurst:     1,
	})

	const n = 3
	start := time.Now()
	for i := 0; i < n; i++ {
		err := g.Do(context.Background(), "store-e", resilience.PriorityHigh, alwaysRetryable, func(ctx context.Context) error {
			return nil
		})
		if err != nil {
			t.Fatalf("call %d: unexpected error %v", i, err)
		}
	}
	elapsed := time.Since(start)

	// 3 calls at burst=1, 5/s should take at least ~2 inter-request gaps of 200ms.
	if elapsed < 300*time.Millisecond {
		t.Fatalf("expected rate limiting to space out calls, took only %v", elapsed)
	}
}

func TestGuard_BulkYieldsToConcurrentHighPriorityCall(t *testing.T) {
	g := resilience.NewGuard(resilience.Config{
		RatePerSecond:     1000,
		RateBurst:         1000,
		BulkYieldInterval: 2 * time.Millisecond,
	})

	highStarted := make(chan struct{})
	releaseHigh := make(chan struct{})

	go func() {
		_ = g.Do(context.Background(), "store-f", resilience.PriorityHigh, alwaysRetryable, func(ctx context.Context) error {
			close(highStarted)
			<-releaseHigh
			return nil
		})
	}()
	<-highStarted

	// While the high-priority call is in flight, a bulk call must be
	// blocked waiting to yield — prove it by giving it a short deadline it
	// cannot possibly beat unless it ran immediately.
	shortCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	var bulkRan bool
	err := g.Do(shortCtx, "store-f", resilience.PriorityBulk, alwaysRetryable, func(ctx context.Context) error {
		bulkRan = true
		return nil
	})
	if bulkRan {
		t.Fatal("bulk call ran while a high-priority call was in flight")
	}
	if err == nil {
		t.Fatal("expected bulk call to still be waiting (deadline exceeded) while high-priority call is in flight")
	}

	close(releaseHigh)

	// Once the high-priority call finishes, bulk should proceed promptly.
	if err := g.Do(context.Background(), "store-f", resilience.PriorityBulk, alwaysRetryable, func(ctx context.Context) error {
		bulkRan = true
		return nil
	}); err != nil {
		t.Fatalf("expected bulk call to proceed after high-priority call finished, got %v", err)
	}
	if !bulkRan {
		t.Fatal("expected bulk fn to run after high-priority call released")
	}
}

func TestGuard_RateLimiterWaitRespectsContextCancellation(t *testing.T) {
	g := resilience.NewGuard(resilience.Config{
		RatePerSecond: 0.1, // effectively one token every 10s
		RateBurst:     1,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	// First call consumes the single burst token immediately.
	if err := g.Do(context.Background(), "store-g", resilience.PriorityHigh, alwaysRetryable, func(ctx context.Context) error { return nil }); err != nil {
		t.Fatalf("first call: unexpected error %v", err)
	}

	// Second call should block on the limiter and be cancelled by ctx.
	err := g.Do(ctx, "store-g", resilience.PriorityHigh, alwaysRetryable, func(ctx context.Context) error { return nil })
	if err == nil {
		t.Fatal("expected context deadline error while waiting for rate limit budget")
	}
}
