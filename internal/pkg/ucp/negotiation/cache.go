// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package negotiation

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

// CacheKey produces a stable composite key for a negotiation result.
// Format: "{merchantID}:{ucpInstanceID}:{platformProfileURI}"
// This ensures that different platforms (type, version, identity) connecting
// to the same instance each get their own cached negotiation result.
//
// merchantID is folded in as a compound-key component alongside
// ucpInstanceID (the store id), per docs/TENANCY-PATTERNS.md's "compound
// keys — both merchant_id AND store_id required for store-scoped data"
// rule: ucpInstanceID/storeID is expected to be a globally-unique UUID, so
// in practice a collision should never happen, but nothing about this
// cache's structure enforced that — a store_id collision across two
// merchants (a UUID-generation bug, a migration, or a test/staging seed)
// would otherwise let one merchant's cached NegotiationResult (active
// capabilities, payment handlers) be served to another. This is a
// defense-in-depth fix, not a response to an observed collision.
func CacheKey(merchantID, ucpInstanceID, platformProfileURI string) string {
	hash := sha256.Sum256([]byte(merchantID + ":" + ucpInstanceID + ":" + platformProfileURI))

	// Print as a hexadecimal string
	return fmt.Sprintf("%x", hash)
}

// NegotiationCache caches negotiated results and raw platform profiles separately.
type NegotiationCache interface {
	// GetNegotiation retrieves a cached NegotiationResult for a composite key.
	GetNegotiation(key string) (*NegotiationResult, bool)

	// SetNegotiation stores a NegotiationResult under a composite key with TTL.
	SetNegotiation(key string, result *NegotiationResult, ttl time.Duration)

	// GetPlatformProfile retrieves a cached raw platform profile by its URI.
	GetPlatformProfile(uri string) (*wire.Profile, bool)

	// SetPlatformProfile stores a raw platform profile by URI with TTL.
	// TTL is derived from HTTP Cache-Control/Expires headers on the fetch response.
	SetPlatformProfile(uri string, profile *wire.Profile, ttl time.Duration)

	// GetPlatformProfileExpiry returns the remaining duration until the cached profile
	// expires. Returns 0 if the profile is not cached or already expired.
	GetPlatformProfileExpiry(uri string) time.Duration
}

type cacheEntry[T any] struct {
	value     T
	expiresAt time.Time
}

// memoryCache is an in-memory NegotiationCache using sync.Map with TTL support.
type memoryCache struct {
	negotiations sync.Map // string -> cacheEntry[*NegotiationResult]
	profiles     sync.Map // string -> cacheEntry[*UCPProfile]
}

// NewMemoryCache returns a new in-memory NegotiationCache.
func NewMemoryCache() NegotiationCache {
	return &memoryCache{}
}

func (m *memoryCache) GetNegotiation(key string) (*NegotiationResult, bool) {
	v, ok := m.negotiations.Load(key)
	if !ok {
		return nil, false
	}
	entry := v.(cacheEntry[*NegotiationResult])
	if time.Now().After(entry.expiresAt) {
		m.negotiations.Delete(key)
		return nil, false
	}
	return entry.value, true
}

func (m *memoryCache) SetNegotiation(key string, result *NegotiationResult, ttl time.Duration) {
	m.negotiations.Store(key, cacheEntry[*NegotiationResult]{
		value:     result,
		expiresAt: time.Now().Add(ttl),
	})
}

func (m *memoryCache) GetPlatformProfile(uri string) (*wire.Profile, bool) {
	v, ok := m.profiles.Load(uri)
	if !ok {
		return nil, false
	}
	entry := v.(cacheEntry[*wire.Profile])
	if time.Now().After(entry.expiresAt) {
		m.profiles.Delete(uri)
		return nil, false
	}
	return entry.value, true
}

func (m *memoryCache) SetPlatformProfile(uri string, profile *wire.Profile, ttl time.Duration) {
	m.profiles.Store(uri, cacheEntry[*wire.Profile]{
		value:     profile,
		expiresAt: time.Now().Add(ttl),
	})
}

func (m *memoryCache) GetPlatformProfileExpiry(uri string) time.Duration {
	v, ok := m.profiles.Load(uri)
	if !ok {
		return 0
	}
	entry := v.(cacheEntry[*wire.Profile])
	remaining := time.Until(entry.expiresAt)
	if remaining < 0 {
		return 0
	}
	return remaining
}
