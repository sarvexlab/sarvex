// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package base

import "fmt"

// CatalogEntity is the normalised product entity the store-API pull methods
// return, shaped to match the sync-plane catalog store model (see
// internal/planes/sync.CatalogProduct) so ingestion can map it directly.
type CatalogEntity struct {
	SKU        string
	Name       string
	PriceMinor int64
	Currency   string
	InStock    bool
	Quantity   *int

	// Description, Category and Images are core product data every platform
	// has a native home for, so they travel as typed fields rather than as
	// raw attributes. Images are absolute URLs, first image first.
	Description string
	Category    string
	Images      []string

	// RawAttributes holds the platform's own attribute names/values
	// (weight, dimensions, colour, ...), untouched: the ontology — not the
	// connector — decides what they mean.
	RawAttributes map[string]string
}

// InventoryEntity is the normalised stock-only projection for a single SKU.
type InventoryEntity struct {
	SKU      string
	InStock  bool
	Quantity *int
}

// PricingEntity is the normalised price-only projection for a single SKU.
type PricingEntity struct {
	SKU        string
	PriceMinor int64
	Currency   string
}

// HealthStatus is the coarse reachability state Ping reports, mirroring the
// control-plane connector status vocabulary (healthy/degraded/down).
type HealthStatus string

const (
	HealthHealthy  HealthStatus = "healthy"
	HealthDegraded HealthStatus = "degraded"
	HealthFailed   HealthStatus = "failed"
)

// ConnectorErrorKind classifies why a store-API pull call failed, so callers
// can decide policy (serve last-known-good / degrade / alert) without
// parsing error strings.
type ConnectorErrorKind string

const (
	// ConnectorErrCircuitOpen means the per-store circuit breaker is open;
	// the call was rejected without hitting the network.
	ConnectorErrCircuitOpen ConnectorErrorKind = "circuit_open"
	// ConnectorErrRateLimited means the store's own API rate-limited the
	// request (after retries) or our client-side limiter's wait was
	// cancelled.
	ConnectorErrRateLimited ConnectorErrorKind = "rate_limited"
	// ConnectorErrNotFound means the requested entity does not exist at
	// the store.
	ConnectorErrNotFound ConnectorErrorKind = "not_found"
	// ConnectorErrUnavailable means the store was unreachable or returned
	// transient errors until retries were exhausted.
	ConnectorErrUnavailable ConnectorErrorKind = "unavailable"
	// ConnectorErrCredentials means the store's credentials could not be
	// resolved or were rejected by the store.
	ConnectorErrCredentials ConnectorErrorKind = "credentials"
)

// ConnectorError is the typed error the store-API pull methods return on
// failure. Callers should use errors.As to inspect Kind rather than matching
// on error strings.
type ConnectorError struct {
	Kind    ConnectorErrorKind
	StoreID string
	Op      string
	Err     error
}

func (e *ConnectorError) Error() string {
	return fmt.Sprintf("connector: store=%s op=%s kind=%s: %v", e.StoreID, e.Op, e.Kind, e.Err)
}

func (e *ConnectorError) Unwrap() error { return e.Err }

// CheckoutRejectedError is a business-level rejection of a checkout operation
// by the merchant backend — the request reached the store and the store said
// no (out of stock, item not found, payment declined, a missing required
// selection). It is deliberately distinct from a connector/transport failure
// (ConnectorError / an unreachable store), which is retryable and surfaces as
// 5xx: a rejection is the buyer's problem to resolve, not the platform's, and
// the serving plane maps it to a 4xx carrying Detail rather than "store
// unavailable".
type CheckoutRejectedError struct {
	// Code is a UCP standard error code where one applies (e.g. "out_of_stock",
	// "not_found", "payment_failed", "field_required"); may be empty.
	Code string
	// Detail is a human-readable description carrying the store's own wording,
	// so a 4xx body stays diagnostic.
	Detail string
}

func (e *CheckoutRejectedError) Error() string { return e.Detail }
