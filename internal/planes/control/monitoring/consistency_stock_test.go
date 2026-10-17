// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package monitoring

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

type stockOnlySource struct{ stock StockData }

func (s stockOnlySource) GetStorePrice(context.Context, string, string) (*PriceData, error) {
	return &PriceData{AmountMinor: 100, Currency: "USD"}, nil
}
func (s stockOnlySource) GetServedPrice(context.Context, string, string) (*PriceData, error) {
	return &PriceData{AmountMinor: 100, Currency: "USD"}, nil
}
func (s stockOnlySource) GetStockStatus(context.Context, string, string) (*StockData, error) {
	return &s.stock, nil
}
func (s stockOnlySource) ListChangedProducts(context.Context, string, time.Time) ([]string, error) {
	return nil, nil
}

// An in-stock product with no quantity is a store that doesn't track stock.
// Reporting it as a critical (checkout-blocking) finding put a blocking task on
// every product of such a store.
func TestCheckProductConsistency_StockClaims(t *testing.T) {
	zero, five := 0, 5
	cases := []struct {
		name     string
		stock    StockData
		critical bool
	}{
		{"in stock, untracked quantity", StockData{InStock: true}, false},
		{"in stock, tracked and positive", StockData{InStock: true, Quantity: &five}, false},
		{"in stock, tracked zero", StockData{InStock: true, Quantity: &zero}, true},
		{"out of stock", StockData{InStock: false}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ce := NewConsistencyEngine(stockOnlySource{stock: tc.stock}, slog.New(slog.DiscardHandler))
			findings, err := ce.CheckProductConsistency(context.Background(), "store1", "sku1")
			if err != nil {
				t.Fatal(err)
			}
			if got := len(findings) > 0; got != tc.critical {
				t.Fatalf("critical stock finding = %v, want %v (%+v)", got, tc.critical, findings)
			}
		})
	}
}
