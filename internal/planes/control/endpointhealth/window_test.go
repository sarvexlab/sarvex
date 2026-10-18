// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package endpointhealth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComputeMetrics_SignatureFailureRate(t *testing.T) {
	records := []windowRecord{
		{Route: "/catalog/:itemId", Signature: "valid", Negotiate: "succeeded", Status: 200, LatencyMs: 100},
		{Route: "/catalog/:itemId", Signature: "valid", Negotiate: "succeeded", Status: 200, LatencyMs: 100},
		{Route: "/catalog/:itemId", Signature: "valid", Negotiate: "succeeded", Status: 200, LatencyMs: 100},
		{Route: "/catalog/:itemId", Signature: "invalid", Negotiate: "not_applicable", Status: 401, LatencyMs: 5},
	}
	m := ComputeMetrics(records)
	assert.Equal(t, 4, m.SampleSize)
	assert.InDelta(t, 0.25, m.SignatureFailureRate, 0.001)
}

func TestComputeMetrics_ExcludesUnsignedRequests(t *testing.T) {
	records := []windowRecord{
		{Route: "/.well-known/ucp", Signature: "not_applicable", Negotiate: "not_applicable", Status: 200, LatencyMs: 10},
		{Route: "/.well-known/ucp", Signature: "not_applicable", Negotiate: "not_applicable", Status: 200, LatencyMs: 10},
		{Route: "/catalog/:itemId", Signature: "valid", Negotiate: "succeeded", Status: 200, LatencyMs: 100},
	}
	m := ComputeMetrics(records)
	assert.Equal(t, 1, m.SampleSize, "unsigned requests (e.g. GET /.well-known/ucp) don't count toward the signed-traffic sample")
}

func TestComputeMetrics_NegotiationFailureRate_OnlyCountsAttempts(t *testing.T) {
	records := []windowRecord{
		{Signature: "valid", Negotiate: "not_applicable", Status: 200, LatencyMs: 10}, // signed but no UCP-Agent
		{Signature: "valid", Negotiate: "succeeded", Status: 200, LatencyMs: 10},
		{Signature: "valid", Negotiate: "failed", Status: 424, LatencyMs: 10},
	}
	m := ComputeMetrics(records)
	assert.Equal(t, 3, m.SampleSize)
	assert.InDelta(t, 0.5, m.NegotiationFailureRate, 0.001, "1 failure out of 2 negotiation attempts, not out of 3 samples")
}

func TestComputeMetrics_TimeoutAndErrorRate(t *testing.T) {
	records := []windowRecord{
		{Signature: "valid", Status: 200, LatencyMs: 50},
		{Signature: "valid", Status: 500, LatencyMs: 50},
		{Signature: "valid", Status: 200, LatencyMs: 50, Timeout: true},
		{Signature: "valid", Status: 200, LatencyMs: 50},
	}
	m := ComputeMetrics(records)
	assert.InDelta(t, 0.25, m.ErrorRate, 0.001)
	assert.InDelta(t, 0.25, m.TimeoutRate, 0.001)
}

func TestComputeMetrics_P95Latency_PerRouteAndAggregate(t *testing.T) {
	var records []windowRecord
	for i := 0; i < 100; i++ {
		lat := int64(100)
		if i >= 95 {
			lat = 5000 // top 5% are slow
		}
		records = append(records, windowRecord{Signature: "valid", Route: "/catalog/:itemId", Status: 200, LatencyMs: lat})
	}
	m := ComputeMetrics(records)
	assert.GreaterOrEqual(t, m.P95LatencyMs, int64(100))
	assert.Equal(t, m.P95LatencyMs, m.PerRouteP95LatencyMs["/catalog/:itemId"])
}

func TestComputeMetrics_Empty(t *testing.T) {
	m := ComputeMetrics(nil)
	assert.Equal(t, 0, m.SampleSize)
	assert.Zero(t, m.SignatureFailureRate)
	assert.Zero(t, m.P95LatencyMs)
}
