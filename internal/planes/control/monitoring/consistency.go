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

// ConsistencySource provides data for detecting divergence. All methods are
// scoped by storeID: catalog/serving data is partitioned by store, not
// merchant (architecture §4.5 invariant 4 — a merchant may own more than one
// store).
type ConsistencySource interface {
	// GetStorePrice returns the current price from the merchant's live store.
	GetStorePrice(ctx context.Context, storeID, sku string) (*PriceData, error)
	// GetServedPrice returns the price from the serving cache.
	GetServedPrice(ctx context.Context, storeID, sku string) (*PriceData, error)
	// GetStockStatus returns inventory status from the merchant's live store.
	GetStockStatus(ctx context.Context, storeID, sku string) (*StockData, error)
	// ListChangedProducts returns SKUs to check for storeID. since is
	// advisory — a full-catalog sweep implementation may ignore it.
	ListChangedProducts(ctx context.Context, storeID string, since time.Time) ([]string, error)
}

// PriceData represents a price value.
type PriceData struct {
	AmountMinor int
	Currency    string
	UpdatedAt   time.Time
}

// StockData represents stock information.
type StockData struct {
	InStock   bool
	Quantity  *int
	UpdatedAt time.Time
}

// ConsistencyEngine detects divergence across sources (store, served cache).
type ConsistencyEngine struct {
	source ConsistencySource
	logger *slog.Logger
}

// NewConsistencyEngine creates a consistency engine.
func NewConsistencyEngine(source ConsistencySource, logger *slog.Logger) *ConsistencyEngine {
	return &ConsistencyEngine{
		source: source,
		logger: logger,
	}
}

// CheckProductConsistency validates that a product's data is consistent across all sources.
func (ce *ConsistencyEngine) CheckProductConsistency(ctx context.Context, storeID, sku string) ([]*domain.ConsistencyFinding, error) {
	findings := make([]*domain.ConsistencyFinding, 0)

	// Check price consistency
	priceFindings, err := ce.checkPriceConsistency(ctx, storeID, sku)
	if err != nil {
		ce.logger.Warn("price consistency check failed", "error", err, "store_id", storeID, "sku", sku)
	} else {
		findings = append(findings, priceFindings...)
	}

	// Check stock consistency
	stockFindings, err := ce.checkStockConsistency(ctx, storeID, sku)
	if err != nil {
		ce.logger.Warn("stock consistency check failed", "error", err, "store_id", storeID, "sku", sku)
	} else {
		findings = append(findings, stockFindings...)
	}

	return findings, nil
}

// checkPriceConsistency detects price drift between sources.
func (ce *ConsistencyEngine) checkPriceConsistency(ctx context.Context, storeID, sku string) ([]*domain.ConsistencyFinding, error) {
	findings := make([]*domain.ConsistencyFinding, 0)

	storePrice, err := ce.source.GetStorePrice(ctx, storeID, sku)
	if err != nil {
		return nil, fmt.Errorf("get store price: %w", err)
	}

	servedPrice, err := ce.source.GetServedPrice(ctx, storeID, sku)
	if err != nil {
		return nil, fmt.Errorf("get served price: %w", err)
	}

	// Compare store vs served
	if storePrice.AmountMinor != servedPrice.AmountMinor || storePrice.Currency != servedPrice.Currency {
		findings = append(findings, &domain.ConsistencyFinding{
			SKU:   sku,
			Field: "price",
			Sources: map[string]string{
				"store":  fmt.Sprintf("%d %s", storePrice.AmountMinor, storePrice.Currency),
				"served": fmt.Sprintf("%d %s", servedPrice.AmountMinor, servedPrice.Currency),
			},
			Severity:   domain.SeverityWarning,
			DetectedAt: time.Now().UTC(),
		})
	}

	return findings, nil
}

// checkStockConsistency detects stock mismatches and compliance violations.
func (ce *ConsistencyEngine) checkStockConsistency(ctx context.Context, storeID, sku string) ([]*domain.ConsistencyFinding, error) {
	findings := make([]*domain.ConsistencyFinding, 0)

	stock, err := ce.source.GetStockStatus(ctx, storeID, sku)
	if err != nil {
		return nil, fmt.Errorf("get stock status: %w", err)
	}

	// Check UCP compliance: a store claiming in-stock with a tracked count must
	// have stock. A nil quantity is a store that does not track stock (e.g.
	// WooCommerce manage_stock off) — in stock with no count is its normal
	// state, not a violation, and this finding is critical (it blocks checkout).
	if stock.InStock && stock.Quantity != nil && *stock.Quantity <= 0 {
		findings = append(findings, &domain.ConsistencyFinding{
			SKU:   sku,
			Field: "stock",
			Sources: map[string]string{
				"claim":    "in_stock",
				"quantity": fmt.Sprintf("%d", *stock.Quantity),
			},
			Severity:   domain.SeverityCritical,
			DetectedAt: time.Now().UTC(),
		})
	}

	return findings, nil
}
