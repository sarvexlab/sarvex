// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package insights

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound mirrors ledger.ErrNotFound/channel.ErrNotFound's role for
// this package.
var ErrNotFound = errors.New("insights: not found")

// CatalogQuery is one serving.catalog.query_recorded fold's input (R2).
// ProductIDs and AgentPlatformHost have already passed through
// sanitizeIdentifiers/sanitizeIdentifier by the time they reach the
// Store — see consumer.go.
type CatalogQuery struct {
	EventID           string
	MerchantID        string
	StoreID           string
	ProductIDs        []string
	AgentPlatformHost string
	OccurredAt        time.Time
}

// NegotiationOutcome is one ucp.negotiation.succeeded/failed fold's input
// (R5/R6/R8). Succeeded is false for a failure; Reason/MissingCapabilities
// are only meaningful when Succeeded is false. RequestedCapabilities/
// DeclaredCapabilities/RequestedPaymentHandlers/DeclaredPaymentHandlers
// are the negotiation.CapabilitySets fields, already sanitized.
type NegotiationOutcome struct {
	// DedupKey is a composite of stable payload fields (merchant_id,
	// store_id, platform_profile_uri, occurred_at) — negotiation events
	// carry no event_id field today (see consumer.go's doc comment), so
	// this is the idempotency key redelivery-safety (A1/A2) is built on.
	DedupKey   string
	MerchantID string
	StoreID    string
	// PlatformProfileURI is the raw negotiation platform_profile_uri —
	// used both as the DistinctPlatformURIs member and to derive Surface.
	PlatformProfileURI       string
	OccurredAt               time.Time
	Succeeded                bool
	FailureReason            NegotiationFailureReason
	RequestedCapabilities    []string
	DeclaredCapabilities     []string
	RequestedPaymentHandlers []string
	DeclaredPaymentHandlers  []string
}

// Store is Agent Behavior Insights' Mongo-backed persistence. Every method
// is an idempotent fold keyed by an event-derived id (A1/A2) — a
// redelivered/duplicate fold is a no-op, never a double-count.
type Store interface {
	// FoldCatalogQuery applies q to control_product_demand_daily: one increment
	// per (store, date, product_id) touched, deduped by q.EventID as a
	// whole (a redelivered event increments nothing a second time).
	FoldCatalogQuery(ctx context.Context, q CatalogQuery) error

	// FoldNegotiationOutcome applies out to control_negotiation_daily,
	// control_payment_handler_mismatch_daily, and (for a failure whose missing
	// capabilities are known) control_capability_gaps_monthly — all in one call,
	// deduped by out.DedupKey.
	FoldNegotiationOutcome(ctx context.Context, out NegotiationOutcome) error

	// ── Reads (R15) — every method serves pre-computed aggregates only,
	// never a raw event scan. ──

	// ListProductDemand returns every ProductDemandDaily row for
	// merchantID in [from,to] (inclusive, yyyy_mm_dd). storeID == "" lists
	// every store the merchant has data for.
	ListProductDemand(ctx context.Context, merchantID, storeID, from, to string) ([]ProductDemandDaily, error)

	// ListNegotiationDaily returns every NegotiationDaily row for
	// merchantID in [from,to].
	ListNegotiationDaily(ctx context.Context, merchantID, storeID, from, to string) ([]NegotiationDaily, error)

	// ListPaymentHandlerMismatchDaily returns every
	// PaymentHandlerMismatchDaily row for merchantID in [from,to].
	ListPaymentHandlerMismatchDaily(ctx context.Context, merchantID, storeID, from, to string) ([]PaymentHandlerMismatchDaily, error)

	// ListCapabilityGaps returns every CapabilityGapMonthly row for
	// merchantID in month (yyyy_mm).
	ListCapabilityGaps(ctx context.Context, merchantID, storeID, month string) ([]CapabilityGapMonthly, error)
}
