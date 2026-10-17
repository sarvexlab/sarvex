// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package monitoring

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// ReconciliationSweeper performs scheduled full-catalog diffs to catch lost ingest signals.
type ReconciliationSweeper struct {
	source ConsistencySource
	logger *slog.Logger
}

// NewReconciliationSweeper creates a reconciliation sweeper.
func NewReconciliationSweeper(source ConsistencySource, logger *slog.Logger) *ReconciliationSweeper {
	return &ReconciliationSweeper{
		source: source,
		logger: logger,
	}
}

// SweepStore performs a full-catalog reconciliation for a store.
// This is the safety net under event-driven pipelines to catch lost ingest signals.
func (rs *ReconciliationSweeper) SweepStore(ctx context.Context, storeID string) ([]*domain.ConsistencyFinding, error) {
	start := time.Now()

	rs.logger.Info("starting reconciliation sweep", "store_id", storeID)

	findings := make([]*domain.ConsistencyFinding, 0)

	// since is advisory; ConsistencySource.ListChangedProducts's default
	// implementation runs a full-catalog sweep and ignores it (see
	// consistency_source.go) — a true incremental diff needs a "changed
	// since" index the catalog store doesn't expose yet.
	since := start.Add(-24 * time.Hour)

	changedSkus, err := rs.source.ListChangedProducts(ctx, storeID, since)
	if err != nil {
		rs.logger.Error("failed to list changed products", "error", err, "store_id", storeID)
		return nil, fmt.Errorf("list changed products: %w", err)
	}

	rs.logger.Debug("reconciliation sweep", "store_id", storeID, "checked_products", len(changedSkus))

	// Check each product for divergence
	for _, sku := range changedSkus {
		// Price divergence: store ↔ catalog store ↔ served cache
		storePrice, err := rs.source.GetStorePrice(ctx, storeID, sku)
		if err != nil {
			rs.logger.Debug("failed to get store price", "error", err, "sku", sku)
			continue
		}

		servedPrice, err := rs.source.GetServedPrice(ctx, storeID, sku)
		if err != nil {
			rs.logger.Debug("failed to get served price", "error", err, "sku", sku)
			continue
		}

		if storePrice.AmountMinor != servedPrice.AmountMinor {
			findings = append(findings, &domain.ConsistencyFinding{
				SKU:   sku,
				Field: "price_rebuild_lag",
				Sources: map[string]string{
					"store":  fmt.Sprintf("%d", storePrice.AmountMinor),
					"served": fmt.Sprintf("%d", servedPrice.AmountMinor),
					"note":   "missed rebuild signal",
				},
				Severity:   domain.SeverityWarning,
				DetectedAt: start,
			})
		}

		// Stock divergence: in-stock claim without inventory
		stock, err := rs.source.GetStockStatus(ctx, storeID, sku)
		if err != nil {
			rs.logger.Debug("failed to get stock", "error", err, "sku", sku)
			continue
		}

		if stock.InStock && (stock.Quantity == nil || *stock.Quantity <= 0) {
			findings = append(findings, &domain.ConsistencyFinding{
				SKU:   sku,
				Field: "stock_safety",
				Sources: map[string]string{
					"claim":    "in_stock",
					"quantity": "zero_or_missing",
				},
				Severity:   domain.SeverityCritical,
				DetectedAt: start,
			})
		}
	}

	elapsed := time.Since(start)
	rs.logger.Info("reconciliation sweep completed",
		"store_id", storeID,
		"findings", len(findings),
		"elapsed_ms", elapsed.Milliseconds(),
	)

	return findings, nil
}
