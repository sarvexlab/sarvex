// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// SurfaceScope answers which agent surfaces a store is on, so the resolve and
// score stages can leave out the attributes and rules that belong to a surface
// the store does not use (v1cat.AttributeField.Surface / ScoringRule.Surface).
// Core fields and rules (no surface) apply to every store and never reach this.
type SurfaceScope interface {
	// ApplicableSurfaces returns the ids of the surfaces the store is
	// enrolled on or working towards: any enrollment that is not "available".
	// A store with no enrollment documents is on none.
	ApplicableSurfaces(ctx context.Context, merchantID, storeID string) (map[string]bool, error)
}

// noSurfaces is the scope used when none is wired: core only.
type noSurfaces struct{}

func (noSurfaces) ApplicableSurfaces(context.Context, string, string) (map[string]bool, error) {
	return nil, nil
}

// controlSurfacesCollection is the control plane's per-store, per-surface
// enrollment collection. The sync plane only reads it, by name, and never
// imports control-plane code (sarvex-v1-architecture.md A10.1: collections have one writer and
// any number of read-only cross-plane readers).
const controlSurfacesCollection = "control_store_surfaces"

// surfaceStateAvailable is the enrollment state of a surface the store has not
// started on (control's domain.SurfaceAvailable, duplicated as data here
// because planes never import one another).
const surfaceStateAvailable = "available"

// mongoSurfaceScope reads control_store_surfaces.
type mongoSurfaceScope struct {
	col *mongo.Collection
}

// NewMongoSurfaceScope returns a SurfaceScope over control_store_surfaces.
func NewMongoSurfaceScope(db *mongo.Database) SurfaceScope {
	return &mongoSurfaceScope{col: db.Collection(controlSurfacesCollection)}
}

func (s *mongoSurfaceScope) ApplicableSurfaces(ctx context.Context, merchantID, storeID string) (map[string]bool, error) {
	// Query-level tenant fence: both ids are in the filter (ADR-2).
	cur, err := s.col.Find(ctx, bson.M{
		"merchant_id": merchantID,
		"store_id":    storeID,
		"state":       bson.M{"$ne": surfaceStateAvailable},
	})
	if err != nil {
		return nil, fmt.Errorf("read surface enrollments: %w", err)
	}
	defer func() { _ = cur.Close(ctx) }()

	var docs []struct {
		SurfaceID string `bson:"surface_id"`
	}
	if err := cur.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode surface enrollments: %w", err)
	}
	out := make(map[string]bool, len(docs))
	for _, d := range docs {
		out[d.SurfaceID] = true
	}
	return out, nil
}

// cachedSurfaceScope memoises a SurfaceScope per store for a short TTL: a bulk
// import resolves and scores thousands of products of one store, and the answer
// changes only when the merchant enrolls on or withdraws from a surface.
// Invalidate drops a store's entry; the audit that follows an enrollment
// change calls it so the rescore sees the new set immediately.
type cachedSurfaceScope struct {
	inner SurfaceScope
	ttl   time.Duration
	now   func() time.Time

	mu      sync.Mutex
	entries map[string]cachedSurfaces
}

type cachedSurfaces struct {
	surfaces map[string]bool
	expires  time.Time
}

// NewCachedSurfaceScope wraps inner with a per-store TTL cache.
func NewCachedSurfaceScope(inner SurfaceScope, ttl time.Duration) *cachedSurfaceScope {
	return &cachedSurfaceScope{inner: inner, ttl: ttl, now: time.Now, entries: map[string]cachedSurfaces{}}
}

func (c *cachedSurfaceScope) ApplicableSurfaces(ctx context.Context, merchantID, storeID string) (map[string]bool, error) {
	key := merchantID + "\x00" + storeID
	c.mu.Lock()
	if e, ok := c.entries[key]; ok && c.now().Before(e.expires) {
		c.mu.Unlock()
		return e.surfaces, nil
	}
	c.mu.Unlock()

	surfaces, err := c.inner.ApplicableSurfaces(ctx, merchantID, storeID)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.entries[key] = cachedSurfaces{surfaces: surfaces, expires: c.now().Add(c.ttl)}
	c.mu.Unlock()
	return surfaces, nil
}

// Invalidate forgets one store's cached surfaces.
func (c *cachedSurfaceScope) Invalidate(merchantID, storeID string) {
	c.mu.Lock()
	delete(c.entries, merchantID+"\x00"+storeID)
	c.mu.Unlock()
}
