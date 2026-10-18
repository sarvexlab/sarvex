// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	servingpkg "github.com/sarvexlab/sarvex/internal/pkg/serving"
)

// The serving cache — its types, its read interface, the Redis client and the
// profile source built on it — lives in internal/pkg/serving, because the sync
// plane (the cache's one writer) and the control plane (the profile doorway)
// construct it too, and no plane may import another (sarvex-v1-architecture.md A10.1). These
// aliases keep the serving plane's own code unchanged.

var (
	ErrNotFound      = servingpkg.ErrNotFound
	ErrInvalidCursor = servingpkg.ErrInvalidCursor

	// ConvertJWKsToSigningKeyCache re-exports the function from the shared package.
	ConvertJWKsToSigningKeyCache = servingpkg.ConvertJWKsToSigningKeyCache

	NewRedisCache         = servingpkg.NewRedisCache
	NewCacheProfileSource = servingpkg.NewCacheProfileSource
)

// freshnessWindow is the serving cache's staleness bound, which checkout's
// freshness guard reports.
const freshnessWindow = servingpkg.FreshnessWindow

type (
	CacheProduct        = servingpkg.CacheProduct
	CacheProfile        = servingpkg.CacheProfile
	CacheWatermark      = servingpkg.CacheWatermark
	Money               = servingpkg.Money
	StockLevel          = servingpkg.StockLevel
	UCPBlockCache       = servingpkg.UCPBlockCache
	SigningKeyCache     = servingpkg.SigningKeyCache
	ServingCache        = servingpkg.ServingCache
	CacheSnapshot       = servingpkg.CacheSnapshot
	CacheSearchResult   = servingpkg.CacheSearchResult
	CacheShippingConfig = servingpkg.CacheShippingConfig
	ShippingZone        = servingpkg.ShippingZone
	Rate                = servingpkg.Rate
	RedisCache          = servingpkg.RedisCache
	CacheProfileSource  = servingpkg.CacheProfileSource
)
