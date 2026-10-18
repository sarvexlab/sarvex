// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

//go:build integration

// Package integration contains integration tests for the serving plane HTTP API.
//
// Tests verify agent-facing endpoint functionality with real Redis cache and MongoDB
// catalog source, validating profile serving, catalog operations, and checkout flows.
//
// Run with:
//
//	REDIS_ADDR=localhost:6379 MONGO_URI="mongodb://admin:password@localhost:27017/?directConnection=true" \
//	  go test -tags=integration ./internal/planes/serving/integration/ -v
package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/mongo"
	mongoopts "go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	merchantA = "merch-a"
	merchantB = "merch-b"
	storeA    = "store-a"
	storeB    = "store-b"
)

// testEnv holds shared test environment state for serving plane tests.
type testEnv struct {
	redisClient *redis.Client
	mongoClient *mongo.Client
	db          *mongo.Database
	catalogCol  *mongo.Collection
}

// setup initializes Redis and MongoDB connections for tests.
func setup(t *testing.T) *testEnv {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Connect to Redis
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		t.Fatalf("failed to ping Redis: %v", err)
	}

	// Connect to MongoDB
	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://admin:password@localhost:27017/?directConnection=true"
	}

	mongoClient, err := mongo.Connect(mongoopts.Client().ApplyURI(mongoURI))
	if err != nil {
		t.Fatalf("failed to connect to MongoDB: %v", err)
	}

	if err := mongoClient.Ping(ctx, nil); err != nil {
		t.Fatalf("failed to ping MongoDB: %v", err)
	}

	// Get database with random suffix to avoid conflicts
	dbName := fmt.Sprintf("sarvex_serving_int_%d", time.Now().UnixNano())
	db := mongoClient.Database(dbName)

	return &testEnv{
		redisClient: redisClient,
		mongoClient: mongoClient,
		db:          db,
		catalogCol:  db.Collection("sync_catalog"),
	}
}

// cleanup tears down the test environment.
func (e *testEnv) cleanup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Flush Redis keys (not full flush, only test-related keys)
	e.redisClient.FlushDB(ctx)

	// Drop MongoDB database
	if err := e.db.Drop(ctx); err != nil {
		t.Logf("warning: failed to drop test database: %v", err)
	}

	// Disconnect from MongoDB
	if err := e.mongoClient.Disconnect(ctx); err != nil {
		t.Logf("warning: failed to disconnect: %v", err)
	}
}

// flushRedis clears Redis keys for a test.
func (e *testEnv) flushRedis(ctx context.Context, t *testing.T) {
	if err := e.redisClient.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("failed to flush Redis: %v", err)
	}
}

// setRedisKey stores a key-value pair in Redis.
func (e *testEnv) setRedisKey(ctx context.Context, t *testing.T, key string, value interface{}, ttl time.Duration) {
	if err := e.redisClient.Set(ctx, key, value, ttl).Err(); err != nil {
		t.Fatalf("failed to set Redis key %s: %v", key, err)
	}
}

// getRedisKey retrieves a value from Redis.
func (e *testEnv) getRedisKey(ctx context.Context, t *testing.T, key string) string {
	val, err := e.redisClient.Get(ctx, key).Result()
	if err == redis.Nil {
		return ""
	}
	if err != nil {
		t.Fatalf("failed to get Redis key %s: %v", key, err)
	}
	return val
}
