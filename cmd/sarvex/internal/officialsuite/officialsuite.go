// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package officialsuite bridges an onboarded Sarvex store to the official
// UCP conformance suite (github.com/Universal-Commerce-Protocol/conformance).
//
// Sarvex used to carry its own protocol assertions (cmd/sarvex/internal/conform)
// as the only conformance check in CI. Now that UCP publishes its own suite,
// duplicating that effort is exactly the thing worth not doing — the
// official suite is the authority on protocol conformance, and
// cmd/sarvex/internal/conform's job narrows to what it is actually good at:
// fast, in-repo checks of Sarvex-specific failure paths (out-of-stock,
// broken catalog attributes) the official suite does not know about.
//
// This package writes the two JSON files the official suite reads
// (conformance_input.json, test_fixtures.json — see its README) from a
// store an onboarding run already produced, rather than hand-maintaining
// fixture files that would drift from whatever the simulated store actually
// seeded. Discount codes, known-customer addresses and dynamic fulfillment
// are deliberately left unconfigured: V1 does not implement those
// capabilities (CLAUDE.md §0), and the official suite skips the
// corresponding tests honestly when a fixture is absent — that is a
// documented gap, not a bug to work around here.
package officialsuite

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sarvexlab/sarvex/cmd/sarvex/internal/conform"
	ucpagent "github.com/sarvexlab/sarvex/internal/pkg/ucp/agent"
)

// RequiredCapabilities are exactly the capabilities the serving plane's
// profile declares (internal/pkg/ucp/catalogue/v1/backends.go's
// v1ImplementedCapabilities) — the checkout-first subset CLAUDE.md §0
// describes, not the full catalogue surface. Keep this in sync with that
// map; the official suite's discovery check fails honestly if it drifts.
var RequiredCapabilities = []string{
	"dev.ucp.shopping.catalog",
	"dev.ucp.shopping.checkout",
	"dev.ucp.shopping.order",
}

// nonExistentItemID is a catalog id skuFor (internal/planes/sync/sim) never
// produces, so it is guaranteed absent from any seeded store's catalog
// regardless of seed or product count.
const nonExistentItemID = "official-suite-non-existent-item-0000"

// Item is one entry of conformance_input.json's items array.
type Item struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Price int64  `json:"price"`
}

// ItemRef is a catalog reference conformance_input.json uses for the
// out-of-stock and non-existent item fields — an id plus a human label,
// nothing a checkout would need to price.
type ItemRef struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
}

// ConformanceInput is the official suite's conformance_input.json.
type ConformanceInput struct {
	UCPVersion           string   `json:"ucp_version"`
	RequiredCapabilities []string `json:"required_capabilities"`
	Currency             string   `json:"currency"`
	Items                []Item   `json:"items"`
	OutOfStockItem       ItemRef  `json:"out_of_stock_item"`
	NonExistentItem      ItemRef  `json:"non_existent_item"`
}

// ValidItem is test_fixtures.json's test_fixtures.valid_item.
type ValidItem struct {
	SKU string `json:"sku"`
	// ExpectedPrice is in major units (e.g. 10.0 for $10.00) — the official
	// suite's own convention, unlike conformance_input.json's minor units.
	ExpectedPrice float64 `json:"expected_price"`
	Quantity      int     `json:"quantity"`
}

// FixtureValues is test_fixtures.json's "test_fixtures" object. Only
// valid_item is populated: discount, known-customer and dynamic-fulfillment
// fixtures stay absent because V1 implements none of those capabilities,
// and the official suite skips their tests when a fixture is missing.
type FixtureValues struct {
	ValidItem ValidItem `json:"valid_item"`
}

// TestFixtures is the official suite's test_fixtures.json.
type TestFixtures struct {
	TestFixtures FixtureValues `json:"test_fixtures"`
}

// Build derives a ConformanceInput and TestFixtures from a store's live
// catalog. It searches the catalog for at least two items — one stays
// purchasable, the other is forced out of stock through fault (the same
// sim mutation path `sarvex conform run`'s failure-path checks use) — so
// the fixtures describe exactly what the store actually seeded rather than
// a hand-picked SKU that may not exist under a different seed.
//
// ucpVersion is stamped into the result verbatim; callers write one
// ConformanceInput per UCP release they want the official suite to check,
// reusing the same TestFixtures for all of them since SKUs and prices do
// not vary by protocol version.
func Build(ctx context.Context, a *ucpagent.Agent, fault conform.Fault, storeID, ucpVersion string) (*ConformanceInput, *TestFixtures, error) {
	search, err := a.SearchCatalog(ctx, storeID, "", 5, "")
	if err != nil {
		return nil, nil, fmt.Errorf("search catalog: %w", err)
	}
	if len(search.Products) < 2 {
		return nil, nil, fmt.Errorf(
			"official conformance fixtures need at least 2 catalog items, got %d; seed the store with more products",
			len(search.Products))
	}

	valid := search.Products[0]
	outOfStock := search.Products[1]
	if fault != nil {
		if err := fault.SetStock(ctx, storeID, outOfStock.ID, 0); err != nil {
			return nil, nil, fmt.Errorf("force %s out of stock: %w", outOfStock.ID, err)
		}
	}

	items := make([]Item, 0, len(search.Products))
	for _, p := range search.Products {
		items = append(items, Item{ID: p.ID, Title: p.Title, Price: p.PriceRange.Min.Amount})
	}

	input := &ConformanceInput{
		UCPVersion:           ucpVersion,
		RequiredCapabilities: RequiredCapabilities,
		Currency:             valid.PriceRange.Min.Currency,
		Items:                items,
		OutOfStockItem:       ItemRef{ID: outOfStock.ID, Title: outOfStock.Title},
		NonExistentItem:      ItemRef{ID: nonExistentItemID, Title: "Non-existent Item"},
	}
	fixtures := &TestFixtures{TestFixtures: FixtureValues{
		ValidItem: ValidItem{
			SKU:           valid.ID,
			ExpectedPrice: float64(valid.PriceRange.Min.Amount) / 100,
			Quantity:      1,
		},
	}}
	return input, fixtures, nil
}

// WriteInput writes one conformance_input.json for the given UCP version to
// dir, named so a caller running the suite against several versions from
// the same store never overwrites a sibling run's file. It returns the
// path written.
func WriteInput(dir string, input *ConformanceInput) (string, error) {
	path := filepath.Join(dir, fmt.Sprintf("conformance_input-%s.json", input.UCPVersion))
	if err := writeJSON(path, input); err != nil {
		return "", err
	}
	return path, nil
}

// WriteFixtures writes test_fixtures.json to dir. It returns the path
// written.
func WriteFixtures(dir string, fixtures *TestFixtures) (string, error) {
	path := filepath.Join(dir, "test_fixtures.json")
	if err := writeJSON(path, fixtures); err != nil {
		return "", err
	}
	return path, nil
}

// WritePaymentInstruments writes a payment_instruments.csv to dir whose rows
// use handlerID as their handler_id. It returns the path written.
//
// The official suite completes a checkout with an instrument read from this
// CSV (integration_test_utils.get_valid_payment_payload), and the server
// rejects any handler its own profile does not advertise. The suite ships a
// flower_shop CSV whose handler_id is that reference server's handler
// (mock_payment_handler); a run against Sarvex must instead carry the handler
// Sarvex actually advertises, exactly as conformance_input.json carries the
// store's real catalog. The column set matches the suite's loader
// (id,type,brand,last_digits,token,handler_id); success_token / fail_token
// are the suite's own convention for the success and failure paths.
func WritePaymentInstruments(dir, handlerID string) (string, error) {
	path := filepath.Join(dir, "payment_instruments.csv")
	rows := "id,type,brand,last_digits,token,handler_id\n" +
		"instr_1,card,Visa,1234,success_token," + handlerID + "\n" +
		"instr_2,card,Mastercard,5678,success_token," + handlerID + "\n" +
		"instr_fail,card,Visa,0000,fail_token," + handlerID + "\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(rows), 0o600); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, nil
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	// These fixtures are handed straight to the official suite's own
	// pytest process and contain nothing secret, but 0600 costs nothing.
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
