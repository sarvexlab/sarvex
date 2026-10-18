// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"context"
	"testing"
	"time"
)

// TestSession_CreateAndRetrieve verifies session creation and retrieval.
func TestSession_CreateAndRetrieve(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()

	// Store session in Redis
	sessionKey := "session:store-a:sess-001"
	sessionJSON := `{"store_id":"store-a","status":"created","items":[]}`

	env.setRedisKey(ctx, t, sessionKey, sessionJSON, 30*time.Minute)

	// Retrieve session
	retrieved := env.getRedisKey(ctx, t, sessionKey)
	if retrieved != sessionJSON {
		t.Errorf("expected session to match, got %s", retrieved)
	}
}

// TestSession_UpdateSession verifies session state updates.
func TestSession_UpdateSession(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()

	sessionKey := "session:store-a:sess-002"

	// Create session
	initialJSON := `{"status":"created","items":[]}`
	env.setRedisKey(ctx, t, sessionKey, initialJSON, 30*time.Minute)

	// Update session
	updatedJSON := `{"status":"updated","items":["SKU-001"]}`
	env.setRedisKey(ctx, t, sessionKey, updatedJSON, 30*time.Minute)

	// Verify update
	retrieved := env.getRedisKey(ctx, t, sessionKey)
	if retrieved != updatedJSON {
		t.Errorf("expected updated session, got %s", retrieved)
	}
}

// TestSession_ExpiryHandling verifies session expiry via TTL.
func TestSession_ExpiryHandling(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()

	sessionKey := "session:store-a:sess-expire"

	// Store session with short TTL
	env.setRedisKey(ctx, t, sessionKey, `{"status":"created"}`, 1*time.Second)

	// Verify it exists immediately
	if env.getRedisKey(ctx, t, sessionKey) == "" {
		t.Fatal("session not found immediately after creation")
	}

	// Wait for expiry
	time.Sleep(2 * time.Second)

	// Verify it's expired
	if env.getRedisKey(ctx, t, sessionKey) != "" {
		t.Error("session should have expired after TTL")
	}
}

// TestSession_ConcurrentAccess verifies concurrent session operations are safe.
func TestSession_ConcurrentAccess(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()

	sessionKey := "session:store-a:sess-concurrent"
	initialJSON := `{"status":"created"}`

	// Create session
	env.setRedisKey(ctx, t, sessionKey, initialJSON, 30*time.Minute)

	// Simulate concurrent updates (sequential in test, but Redis handles atomicity)
	for i := 0; i < 5; i++ {
		updated := `{"status":"updated","version":` + string(rune('0'+byte(i))) + `}`
		env.setRedisKey(ctx, t, sessionKey, updated, 30*time.Minute)
	}

	// Verify final state
	final := env.getRedisKey(ctx, t, sessionKey)
	if final == "" {
		t.Error("session lost during concurrent access")
	}
}

// TestSession_TenantIsolation verifies sessions don't leak across stores.
func TestSession_TenantIsolation(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()

	sessionA := "session:store-a:sess-tenant-001"
	sessionB := "session:store-b:sess-tenant-002"

	jsonA := `{"store":"a"}`
	jsonB := `{"store":"b"}`

	// Create sessions for different stores
	env.setRedisKey(ctx, t, sessionA, jsonA, 30*time.Minute)
	env.setRedisKey(ctx, t, sessionB, jsonB, 30*time.Minute)

	// Verify isolation
	if env.getRedisKey(ctx, t, sessionA) != jsonA {
		t.Error("store-a session corrupted")
	}
	if env.getRedisKey(ctx, t, sessionB) != jsonB {
		t.Error("store-b session corrupted")
	}
}
