// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package webbaseline

import (
	"context"
	"time"
)

// DailyBaselineResult is what a connector's optional Source capability
// returns for one store/day.
type DailyBaselineResult struct {
	Orders       int64
	RevenueMinor int64
	Currency     string
	// Partial signals the connector could only report part of the day
	// (R11's confidence=partial) — e.g. a paginated report that was
	// truncated. false means the connector is confident this is the full
	// day's data.
	Partial bool
}

// Source is the optional per-connector capability this package looks for
// (via a type assertion at the call site, see StoreLister/Syncer) beyond
// base.Connector. No connector in this codebase implements it today — see
// model.go's package doc comment for the audited reason — so Syncer
// treats every store as not implementing Source and honestly records
// confidence=unavailable. The interface exists so the day a connector
// does implement it, no schema or worker change is required: only that
// connector's factory needs to also satisfy this interface.
type Source interface {
	// DailyBaseline returns the store's platform-reported order
	// count/revenue for the UTC calendar day `date` (any time value
	// within that day; implementations should normalise). Distinct from
	// base.Connector so a connector that can't yet support it simply
	// doesn't implement this interface — no error path, no stub method to
	// maintain.
	DailyBaseline(ctx context.Context, storeID string, date time.Time) (DailyBaselineResult, error)
}

// SourceResolver resolves a store's connector and reports whether it
// implements Source. Decoupled from any concrete connector registry so
// this package stays testable without wiring credentials/secret
// management — entry.go supplies the real implementation.
type SourceResolver func(ctx context.Context, storeID, platform string) (Source, bool, error)

// NoSourceResolver is the resolver entry.go wires today: no connector in
// this codebase implements Source (see model.go's audit), so this always
// reports "not available" rather than guessing. Swapping in a real
// resolver later (once a connector implements Source) requires no change
// to Syncer or the BaselineDay schema.
func NoSourceResolver(context.Context, string, string) (Source, bool, error) {
	return nil, false, nil
}
