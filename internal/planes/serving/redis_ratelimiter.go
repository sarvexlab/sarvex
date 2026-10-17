// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// tokenBucketScript atomically refills and takes one token from a Redis
// hash {tokens, last_fill_ms} — the same math as MemoryRateLimiter.Allowed
// (capacity == rate, i.e. no separate burst allowance), just shared across
// every serving replica instead of kept per-process. KEYS[1] is the bucket
// key; ARGV[1] is the rate/capacity (tokens per second), ARGV[2] is the
// current time in unix milliseconds.
var tokenBucketScript = redis.NewScript(`
local key = KEYS[1]
local rate = tonumber(ARGV[1])
local now = tonumber(ARGV[2])

local existing = redis.call("HMGET", key, "tokens", "last_fill")
local tokens = tonumber(existing[1])
local last_fill = tonumber(existing[2])
if tokens == nil then
	tokens = rate
	last_fill = now
end

local elapsed = math.max(0, now - last_fill) / 1000.0
tokens = math.min(rate, tokens + elapsed * rate)

local allowed = 0
if tokens >= 1 then
	tokens = tokens - 1
	allowed = 1
end

redis.call("HSET", key, "tokens", tostring(tokens), "last_fill", tostring(now))
redis.call("EXPIRE", key, 3600)
return allowed
`)

// RedisRateLimiter enforces per-merchant per-endpoint rate limits using a
// Redis-backed token bucket, so every serving replica in a region shares
// the same quota instead of each enforcing its own independent one (see
// RateLimiter's doc comment). Uses the same Redis instance already backing
// session_redis.go — no new backing service to run.
type RedisRateLimiter struct {
	client *redis.Client
	log    *slog.Logger
}

var _ RateLimiter = (*RedisRateLimiter)(nil)

// NewRedisRateLimiter creates a rate limiter backed by the given Redis
// client (share the one from serving.NewRedisCache/session_redis.go's
// Client(), rather than opening a second connection pool).
func NewRedisRateLimiter(client *redis.Client, log *slog.Logger) *RedisRateLimiter {
	return &RedisRateLimiter{client: client, log: log}
}

// Allowed returns true if the merchant/endpoint is within rate limit. On a
// Redis error it fails OPEN (allows the request) rather than closed: a
// cache outage shouldn't also take down the serving plane's whole
// agent-facing read path, and the serving cache itself already degrades
// the same way (CLAUDE.md §2.2 makes availability here the priority).
func (rl *RedisRateLimiter) Allowed(ctx context.Context, merchantID, endpoint string) bool {
	rate, ok := endpointLimits[endpoint]
	if !ok {
		rate = defaultRateLimit
	}

	key := "ratelimit:" + merchantID + ":" + endpoint
	now := time.Now().UnixMilli()

	result, err := tokenBucketScript.Run(ctx, rl.client, []string{key}, rate, now).Int64()
	if err != nil {
		if rl.log != nil {
			rl.log.Warn("rate limiter redis call failed; failing open", "error", err, "merchant_id", merchantID, "endpoint", endpoint)
		}
		return true
	}

	allowed := result == 1
	if !allowed && rl.log != nil {
		rl.log.Warn("rate limit exceeded", "merchant_id", merchantID, "endpoint", endpoint)
	}
	return allowed
}
