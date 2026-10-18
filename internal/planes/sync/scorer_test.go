// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"log/slog"
	"os"
	"testing"
)

// fakeOntologyStore is an in-memory test implementation.
type fakeOntologyStore struct {
	rules map[string]OntologyRule
}

func newFakeOntologyStore() *fakeOntologyStore {
	return &fakeOntologyStore{
		rules: make(map[string]OntologyRule),
	}
}

func (fos *fakeOntologyStore) GetRulesByPlatform(ctx context.Context, platform string) ([]OntologyRule, error) {
	var rules []OntologyRule
	for _, rule := range fos.rules {
		if rule.Platform == platform || rule.Platform == "*" {
			rules = append(rules, rule)
		}
	}
	return rules, nil
}

func (fos *fakeOntologyStore) GetRule(ctx context.Context, ruleID string) (*OntologyRule, error) {
	rule, ok := fos.rules[ruleID]
	if !ok {
		return nil, nil
	}
	return &rule, nil
}

func (fos *fakeOntologyStore) ApplyRule(rule OntologyRule, rawValue string) (string, error) {
	// Delegates to the real transform logic (MongoOntologyStore.ApplyRule
	// doesn't touch its db field), so fake-backed tests exercise the same
	// identity/lowercase/uppercase/enum_map/format_date behavior as production.
	return (&MongoOntologyStore{}).ApplyRule(rule, rawValue)
}

// fakeConfigStore is an in-memory test implementation.
type fakeConfigStore struct {
	weights *EligibilityWeights
}

func newFakeConfigStore(weights *EligibilityWeights) *fakeConfigStore {
	return &fakeConfigStore{weights: weights}
}

func (fcs *fakeConfigStore) GetEligibilityWeights(ctx context.Context) (*EligibilityWeights, error) {
	if fcs.weights != nil {
		return fcs.weights, nil
	}
	w := DefaultEligibilityWeights()
	return &w, nil
}

func TestProductScorer_ValidProduct(t *testing.T) {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ontology := newFakeOntologyStore()
	configStore := newFakeConfigStore(nil)

	scorer, err := NewProductScorer(ontology, configStore, log)
	if err != nil {
		t.Fatalf("create scorer: %v", err)
	}

	ctx := context.Background()
	product := &CatalogProduct{
		ID:            "merchant-1:SKU-001",
		MerchantID:    "merchant-1",
		SKU:           "SKU-001",
		Title:         "Valid Product",
		PriceMinor:    1999,
		Currency:      "USD",
		InStock:       true,
		RawAttributes: map[string]string{"color": "red"},
		MappedFields:  map[string]string{"color": "red"}, // Include mapped color to avoid penalty
		Findings:      make([]*Finding, 0),
	}

	result, err := scorer.Score(ctx, "merchant-1", product)
	if err != nil {
		t.Fatalf("score product: %v", err)
	}

	if result.Score != 100 {
		t.Errorf("expected score 100, got %d", result.Score)
	}
	if result.BlocksCheckout {
		t.Errorf("valid product should not block checkout")
	}
	if len(result.Findings) != 0 {
		t.Errorf("valid product should have no findings, got %d", len(result.Findings))
	}
}

func TestProductScorer_MissingPrice(t *testing.T) {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ontology := newFakeOntologyStore()
	configStore := newFakeConfigStore(nil)

	scorer, err := NewProductScorer(ontology, configStore, log)
	if err != nil {
		t.Fatalf("create scorer: %v", err)
	}

	ctx := context.Background()
	product := &CatalogProduct{
		ID:            "merchant-1:SKU-002",
		MerchantID:    "merchant-1",
		SKU:           "SKU-002",
		Title:         "Product No Price",
		PriceMinor:    0, // Missing price
		Currency:      "USD",
		InStock:       true,
		RawAttributes: make(map[string]string),
		MappedFields:  make(map[string]string),
		Findings:      make([]*Finding, 0),
	}

	result, err := scorer.Score(ctx, "merchant-1", product)
	if err != nil {
		t.Fatalf("score product: %v", err)
	}

	if result.Score >= 100 {
		t.Errorf("product with missing price should have score < 100, got %d", result.Score)
	}
	if !result.BlocksCheckout {
		t.Errorf("product with missing price should block checkout")
	}

	// Find the missing_price finding
	found := false
	for _, f := range result.Findings {
		if f.Code == "missing_or_invalid_price" && f.BlocksCheckout {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected finding 'missing_or_invalid_price' with BlocksCheckout=true")
	}
}

func TestProductScorer_MissingTitle(t *testing.T) {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ontology := newFakeOntologyStore()
	configStore := newFakeConfigStore(nil)

	scorer, err := NewProductScorer(ontology, configStore, log)
	if err != nil {
		t.Fatalf("create scorer: %v", err)
	}

	ctx := context.Background()
	product := &CatalogProduct{
		ID:            "merchant-1:SKU-003",
		MerchantID:    "merchant-1",
		SKU:           "SKU-003",
		Title:         "", // Missing title
		PriceMinor:    1999,
		Currency:      "USD",
		InStock:       true,
		RawAttributes: make(map[string]string),
		MappedFields:  make(map[string]string),
		Findings:      make([]*Finding, 0),
	}

	result, err := scorer.Score(ctx, "merchant-1", product)
	if err != nil {
		t.Fatalf("score product: %v", err)
	}

	// Should penalize but not block
	if result.Score >= 100 {
		t.Errorf("product with missing title should have score < 100, got %d", result.Score)
	}
	if result.BlocksCheckout {
		t.Errorf("product with missing title should not block checkout")
	}

	// Find the missing_title finding
	found := false
	for _, f := range result.Findings {
		if f.Code == "missing_title" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected finding 'missing_title'")
	}
}

func TestProductScorer_InventoryInconsistency(t *testing.T) {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ontology := newFakeOntologyStore()
	configStore := newFakeConfigStore(nil)

	scorer, err := NewProductScorer(ontology, configStore, log)
	if err != nil {
		t.Fatalf("create scorer: %v", err)
	}

	ctx := context.Background()
	qty := 5
	product := &CatalogProduct{
		ID:            "merchant-1:SKU-004",
		MerchantID:    "merchant-1",
		SKU:           "SKU-004",
		Title:         "Product",
		PriceMinor:    1999,
		Currency:      "USD",
		InStock:       false, // Out of stock but quantity > 0
		Quantity:      &qty,
		RawAttributes: make(map[string]string),
		MappedFields:  make(map[string]string),
		Findings:      make([]*Finding, 0),
	}

	result, err := scorer.Score(ctx, "merchant-1", product)
	if err != nil {
		t.Fatalf("score product: %v", err)
	}

	// Should penalize
	if result.Score >= 100 {
		t.Errorf("product with inventory inconsistency should have score < 100, got %d", result.Score)
	}

	// Find the inventory_inconsistency finding
	found := false
	for _, f := range result.Findings {
		if f.Code == "inventory_inconsistency" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected finding 'inventory_inconsistency'")
	}
}

func TestFindingBuilder(t *testing.T) {
	finding := NewFinding("test_code", "Test message", "warning").
		Field("test_field").
		RequiredFor("ucp_checkout").
		ActionableBy("merchant_action").
		BlocksCheckout(true).
		Details(map[string]interface{}{"key": "value"}).
		Build()

	if finding.Code != "test_code" {
		t.Errorf("expected code 'test_code', got '%s'", finding.Code)
	}
	if finding.Field != "test_field" {
		t.Errorf("expected field 'test_field', got '%s'", finding.Field)
	}
	if !finding.BlocksCheckout {
		t.Errorf("expected BlocksCheckout=true")
	}
}
