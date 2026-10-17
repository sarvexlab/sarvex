// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package billing_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/planes/control/billing"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// countingPlanReader counts calls to Get so tests can assert on cache hits vs.
// misses, and can be made to fail on demand to test that errors pass through
// uncached.
type countingPlanReader struct {
	calls int
	plan  domain.Plan
	err   error
}

func (c *countingPlanReader) Get(_ context.Context, _ domain.MerchantID) (domain.Billing, error) {
	c.calls++
	if c.err != nil {
		return domain.Billing{}, c.err
	}
	return domain.Billing{Plan: c.plan}, nil
}

func TestCachedPlanReader_CachesWithinTTL(t *testing.T) {
	source := &countingPlanReader{plan: domain.PlanConcierge}
	cache := billing.NewCachedPlanReader(source, time.Minute)

	for range 3 {
		got, err := cache.Get(context.Background(), "merchant1")
		require.NoError(t, err)
		assert.Equal(t, domain.PlanConcierge, got.Plan)
	}

	assert.Equal(t, 1, source.calls, "expected only the first call to reach the source")
}

func TestCachedPlanReader_RefetchesAfterTTLExpires(t *testing.T) {
	source := &countingPlanReader{plan: domain.PlanSelfServe}
	cache := billing.NewCachedPlanReader(source, time.Millisecond)

	_, err := cache.Get(context.Background(), "merchant1")
	require.NoError(t, err)
	time.Sleep(5 * time.Millisecond)
	_, err = cache.Get(context.Background(), "merchant1")
	require.NoError(t, err)

	assert.Equal(t, 2, source.calls, "expected the expired entry to be re-fetched")
}

func TestCachedPlanReader_CachesPerMerchant(t *testing.T) {
	source := &countingPlanReader{plan: domain.PlanScale}
	cache := billing.NewCachedPlanReader(source, time.Minute)

	_, err := cache.Get(context.Background(), "merchant1")
	require.NoError(t, err)
	_, err = cache.Get(context.Background(), "merchant2")
	require.NoError(t, err)
	_, err = cache.Get(context.Background(), "merchant1")
	require.NoError(t, err)

	assert.Equal(t, 2, source.calls, "expected one source call per distinct merchant")
}

func TestCachedPlanReader_DoesNotCacheErrors(t *testing.T) {
	source := &countingPlanReader{err: errors.New("mongo unavailable")}
	cache := billing.NewCachedPlanReader(source, time.Minute)

	_, err1 := cache.Get(context.Background(), "merchant1")
	_, err2 := cache.Get(context.Background(), "merchant1")

	require.Error(t, err1)
	require.Error(t, err2)
	assert.Equal(t, 2, source.calls, "expected every call to retry the source when it's failing")
}
