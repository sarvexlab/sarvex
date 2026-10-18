// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package endpointhealth

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/redis/go-redis/v9"
)

// windowRecord is one telemetry sample folded into a store's rolling
// window.
type windowRecord struct {
	RequestID string `json:"rid"`
	Route     string `json:"route"`
	Status    int    `json:"status"`
	Signature string `json:"sig"`  // valid | invalid | missing | not_applicable
	Negotiate string `json:"nego"` // succeeded | failed | not_applicable
	LatencyMs int64  `json:"lat"`
	Timeout   bool   `json:"to"`
}

// Window is a per-store rolling window of real-traffic telemetry, backed
// by one Redis sorted set per store (score = event unix-nano timestamp,
// member = the JSON record). Old entries are evicted on every read/write
// rather than via a separate sweep job — bounded by the fact that eviction
// only ever removes entries this store's own write path added.
type Window struct {
	client *redis.Client
	prefix string
	ttl    time.Duration
}

// NewWindow constructs a Window. ttl is the rolling window length (e.g.
// Config.RollingWindow).
func NewWindow(client *redis.Client, prefix string, ttl time.Duration) *Window {
	return &Window{client: client, prefix: prefix, ttl: ttl}
}

func (w *Window) key(storeID string) string {
	return w.prefix + "window:" + storeID
}

// Record folds one telemetry sample into storeID's window. Idempotent by
// requestID: a redelivered telemetry event for the same request_id is a
// no-op (the JSON member is deterministic given the same input, so a
// duplicate ZADD just re-sets the same score — but to guard against a
// truly duplicate publish being folded twice into rate math via two
// distinct-looking members, callers should not call Record twice for the
// same request_id; the evaluator's subscriber dedups upstream of this
// call, same convention as every other bus consumer in this repo).
func (w *Window) Record(ctx context.Context, storeID string, rec windowRecord, at time.Time) error {
	body, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal window record: %w", err)
	}
	key := w.key(storeID)
	pipe := w.client.TxPipeline()
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(at.UnixNano()), Member: string(body)})
	pipe.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("%d", time.Now().Add(-w.ttl).UnixNano()))
	pipe.Expire(ctx, key, w.ttl+time.Minute)
	_, err = pipe.Exec(ctx)
	return err
}

// Snapshot returns every record currently within the window for storeID,
// evicting stale entries first.
func (w *Window) Snapshot(ctx context.Context, storeID string) ([]windowRecord, error) {
	key := w.key(storeID)
	cutoff := time.Now().Add(-w.ttl).UnixNano()
	if err := w.client.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("%d", cutoff)).Err(); err != nil {
		return nil, fmt.Errorf("evict stale window entries: %w", err)
	}
	members, err := w.client.ZRange(ctx, key, 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("read window: %w", err)
	}
	out := make([]windowRecord, 0, len(members))
	for _, m := range members {
		var rec windowRecord
		if err := json.Unmarshal([]byte(m), &rec); err != nil {
			continue // one malformed member must not fail the whole read
		}
		out = append(out, rec)
	}
	return out, nil
}

// Metrics is the set of rolling-window rates/latencies the evaluator
// derives per store per pass.
type Metrics struct {
	SampleSize             int
	SignatureFailureRate   float64
	NegotiationFailureRate float64
	TimeoutRate            float64
	ErrorRate              float64
	P95LatencyMs           int64
	PerRouteP95LatencyMs   map[string]int64
}

// ComputeMetrics reduces a window snapshot to rates. Only records with a
// signature outcome other than not_applicable count toward SampleSize —
// unsigned requests (e.g. GET /.well-known/ucp) aren't representative of
// agent-checkout traffic health.
func ComputeMetrics(records []windowRecord) Metrics {
	var m Metrics
	var latencies []int64
	perRoute := map[string][]int64{}

	signed := 0
	var sigFail, negoAttempts, negoFail, timeouts, errors int

	for _, r := range records {
		if r.Signature == "not_applicable" {
			continue
		}
		signed++
		if r.Signature != "valid" {
			sigFail++
		}
		if r.Negotiate != "not_applicable" {
			negoAttempts++
			if r.Negotiate != "succeeded" {
				negoFail++
			}
		}
		if r.Timeout {
			timeouts++
		}
		if r.Status >= 500 {
			errors++
		}
		latencies = append(latencies, r.LatencyMs)
		perRoute[r.Route] = append(perRoute[r.Route], r.LatencyMs)
	}

	m.SampleSize = signed
	if signed > 0 {
		m.SignatureFailureRate = float64(sigFail) / float64(signed)
		m.TimeoutRate = float64(timeouts) / float64(signed)
		m.ErrorRate = float64(errors) / float64(signed)
	}
	if negoAttempts > 0 {
		m.NegotiationFailureRate = float64(negoFail) / float64(negoAttempts)
	}
	m.P95LatencyMs = percentile(latencies, 0.95)
	m.PerRouteP95LatencyMs = make(map[string]int64, len(perRoute))
	for route, lats := range perRoute {
		m.PerRouteP95LatencyMs[route] = percentile(lats, 0.95)
	}
	return m
}

func percentile(values []int64, p float64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := int(p * float64(len(sorted)-1))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
