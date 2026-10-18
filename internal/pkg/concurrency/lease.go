// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package concurrency

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Lease is a distributed, renewable, mutually-exclusive lock keyed by name.
// It is the coordination primitive behind both single-owner scheduling
// (leader election: one process renews a fixed key, all others fail to
// acquire it) and overlap guards (a per-resource "is this already running"
// lock, e.g. one lease per store_id). A Lease is safe for concurrent use by
// multiple process instances, which is the point: it must work correctly
// when N instances of the same service call it at once.
type Lease interface {
	// TryAcquire attempts to acquire or renew the lease identified by key.
	// It succeeds (true) when the lease is unheld, expired, or already held
	// by this same holder (a renewal); it fails (false, no error) when held
	// by a different holder. ttl bounds how long the lease survives without
	// a further renewal, so a crashed holder's lease self-expires rather
	// than wedging the resource forever.
	TryAcquire(ctx context.Context, key, holder string, ttl time.Duration) (bool, error)

	// Release drops the lease if it is currently held by holder. Releasing
	// a lease held by someone else (or already expired) is a no-op, not an
	// error, since by the time Release runs the caller may have lost the
	// lease already (e.g. to expiry under a slow operation) — Release only
	// ever cleans up its own ownership.
	Release(ctx context.Context, key, holder string) error
}

// tryAcquireScript atomically acquires key for holder: it succeeds if the
// key is absent or already owned by holder, and fails (returns 0) if held
// by someone else. Single Lua eval keeps the read-then-write race-free
// across concurrent callers targeting the same key.
var tryAcquireScript = redis.NewScript(`
local v = redis.call("GET", KEYS[1])
if v == false or v == ARGV[1] then
	redis.call("SET", KEYS[1], ARGV[1], "PX", ARGV[2])
	return 1
end
return 0
`)

// releaseScript deletes key only if it is still owned by holder (a
// compare-and-delete), so a lease that already expired and was re-acquired
// by someone else is never accidentally released out from under them.
var releaseScript = redis.NewScript(`
local v = redis.call("GET", KEYS[1])
if v == ARGV[1] then
	redis.call("DEL", KEYS[1])
	return 1
end
return 0
`)

// RedisLease implements Lease on a single Redis instance/cluster, the same
// backend already used for idempotency keys and cache snapshots elsewhere
// in this codebase. Keys carry their own TTL (PX), so a holder that crashes
// without releasing self-expires rather than requiring a reaper.
type RedisLease struct {
	client *redis.Client
	prefix string
}

// NewRedisLease creates a Lease backed by client. prefix namespaces every
// key this Lease touches (e.g. "sweep:") so it can share a Redis instance
// with unrelated keys (sessions, cache snapshots) without collision.
func NewRedisLease(client *redis.Client, prefix string) *RedisLease {
	return &RedisLease{client: client, prefix: prefix}
}

func (l *RedisLease) TryAcquire(ctx context.Context, key, holder string, ttl time.Duration) (bool, error) {
	res, err := tryAcquireScript.Run(ctx, l.client, []string{l.prefix + key}, holder, ttl.Milliseconds()).Int()
	if err != nil {
		return false, err
	}
	return res == 1, nil
}

func (l *RedisLease) Release(ctx context.Context, key, holder string) error {
	_, err := releaseScript.Run(ctx, l.client, []string{l.prefix + key}, holder).Int()
	return err
}

var _ Lease = (*RedisLease)(nil)
