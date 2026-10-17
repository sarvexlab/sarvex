// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	v1cat "github.com/sarvexlab/sarvex/internal/pkg/ucp/catalogue/v1"
)

// fakeFieldCatalogue is a minimal FieldCatalogue test double: a fixed target
// field list, independent of the full UCP definitions catalogue.
type fakeFieldCatalogue struct {
	fields []v1cat.AttributeField
}

func (f fakeFieldCatalogue) AttributeFields() []v1cat.AttributeField { return f.fields }

func testCatalogue(fieldIDs ...string) fakeFieldCatalogue {
	fields := make([]v1cat.AttributeField, len(fieldIDs))
	for i, id := range fieldIDs {
		fields[i] = v1cat.AttributeField{ID: id}
	}
	return fakeFieldCatalogue{fields: fields}
}

func testResolveProduct(rawAttrs map[string]string) *CatalogProduct {
	return &CatalogProduct{
		ID:            "merch-1:store-1:SKU-1",
		MerchantID:    "merch-1",
		StoreID:       "store-1",
		SKU:           "SKU-1",
		RawAttributes: rawAttrs,
	}
}

func testLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestResolve_RuleTierHighConfidence verifies clean attributes map onto the
// catalogue's target fields via the rule tier at high confidence.
func TestResolve_RuleTierHighConfidence(t *testing.T) {
	ontology := newFakeOntologyStore()
	ontology.rules["r1"] = OntologyRule{
		ID: "r1", SourceAttribute: "color", TargetField: "color",
		Platform: "*", Tier: TierRule, Confidence: 0.97, TransformFn: "identity",
	}

	resolver := NewOntologyResolver(ontology, testCatalogue("color", "size"), testLog())
	product := testResolveProduct(map[string]string{"color": "Red"})

	mapped, err := resolver.Resolve(context.Background(), product)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	got := mapped.Fields["color"]
	if got.Tier != TierRule || got.Value != "Red" || got.SourceAttr != "color" || got.Confidence != 0.97 {
		t.Errorf("expected high-confidence rule-tier resolution, got %+v", got)
	}

	if mapped.Fields["size"].Tier != TierUnresolved {
		t.Errorf("expected size unresolved (no rule/raw attribute), got %+v", mapped.Fields["size"])
	}
}

// TestResolve_NormalisedNameMatch verifies a raw attribute matches an
// ontology rule's source_attr regardless of case/spacing (the rule tier's
// "normalised-name match").
func TestResolve_NormalisedNameMatch(t *testing.T) {
	ontology := newFakeOntologyStore()
	ontology.rules["r1"] = OntologyRule{
		ID: "r1", SourceAttribute: "product_color", TargetField: "color",
		Platform: "*", Tier: TierRule, Confidence: 0.9, TransformFn: "identity",
	}

	resolver := NewOntologyResolver(ontology, testCatalogue("color"), testLog())
	product := testResolveProduct(map[string]string{"Product-Color ": "Blue"})

	mapped, err := resolver.Resolve(context.Background(), product)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	got := mapped.Fields["color"]
	if got.Tier != TierRule || got.Value != "Blue" || got.SourceAttr != "Product-Color " {
		t.Errorf("expected normalised-name match to resolve, got %+v", got)
	}
}

// TestResolve_LearnedTierApplied verifies a known learned mapping (a
// previously hand-resolved non-standard size label) resolves via the
// learned tier with its confidence.
func TestResolve_LearnedTierApplied(t *testing.T) {
	ontology := newFakeOntologyStore()
	ontology.rules["learned-1"] = OntologyRule{
		ID: "learned-1", SourceAttribute: "sz", TargetField: "size",
		Platform: "*", Tier: TierLearned, Confidence: 0.82, TransformFn: "enum_map",
		TransformConfig: map[string]interface{}{
			"mapping": map[string]interface{}{"XL-EU52": "XL"},
		},
	}

	resolver := NewOntologyResolver(ontology, testCatalogue("size"), testLog())
	product := testResolveProduct(map[string]string{"sz": "XL-EU52"})

	mapped, err := resolver.Resolve(context.Background(), product)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	got := mapped.Fields["size"]
	if got.Tier != TierLearned || got.Value != "XL" || got.Confidence != 0.82 || got.SourceAttr != "sz" {
		t.Errorf("expected learned-tier resolution, got %+v", got)
	}
}

// TestResolve_Unresolved verifies attributes with no matching rule/learned
// candidate are marked unresolved, not guessed and not dropped from output.
func TestResolve_Unresolved(t *testing.T) {
	ontology := newFakeOntologyStore()
	resolver := NewOntologyResolver(ontology, testCatalogue("color", "brand"), testLog())
	product := testResolveProduct(map[string]string{"color": "Red"})

	mapped, err := resolver.Resolve(context.Background(), product)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	got, ok := mapped.Fields["brand"]
	if !ok {
		t.Fatalf("expected unresolved 'brand' field to still be present in output")
	}
	if got.Tier != TierUnresolved || got.Value != "" || got.SourceAttr != "" || got.Confidence != 0 {
		t.Errorf("expected zero-value unresolved field, got %+v", got)
	}
}

// TestResolve_RulePrecedenceOverLearned verifies rule vs. learned conflicts
// resolve by precedence — rule always wins, even when the learned
// candidate has higher confidence — deterministically.
func TestResolve_RulePrecedenceOverLearned(t *testing.T) {
	ontology := newFakeOntologyStore()
	ontology.rules["rule-1"] = OntologyRule{
		ID: "rule-1", SourceAttribute: "color", TargetField: "color",
		Platform: "*", Tier: TierRule, Confidence: 0.6, TransformFn: "identity",
	}
	ontology.rules["learned-1"] = OntologyRule{
		ID: "learned-1", SourceAttribute: "colour_code", TargetField: "color",
		Platform: "*", Tier: TierLearned, Confidence: 0.99, TransformFn: "identity",
	}

	resolver := NewOntologyResolver(ontology, testCatalogue("color"), testLog())
	product := testResolveProduct(map[string]string{
		"color":       "Red",
		"colour_code": "RED-99",
	})

	mapped, err := resolver.Resolve(context.Background(), product)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	got := mapped.Fields["color"]
	if got.Tier != TierRule || got.SourceAttr != "color" || got.Value != "Red" {
		t.Errorf("expected rule tier to win over higher-confidence learned tier, got %+v", got)
	}
}

// TestResolve_Deterministic verifies identical input + ontology rule set
// always produces an identical MappedProduct, and that tie-breaking within
// a tier never depends on rule iteration order.
func TestResolve_Deterministic(t *testing.T) {
	ontology := newFakeOntologyStore()
	ontology.rules["rule-b"] = OntologyRule{
		ID: "rule-b", SourceAttribute: "color", TargetField: "color",
		Platform: "*", Tier: TierRule, Confidence: 0.9, TransformFn: "identity",
	}
	ontology.rules["rule-a"] = OntologyRule{
		ID: "rule-a", SourceAttribute: "colour", TargetField: "color",
		Platform: "*", Tier: TierRule, Confidence: 0.9, TransformFn: "identity",
	}

	resolver := NewOntologyResolver(ontology, testCatalogue("color"), testLog())
	product := testResolveProduct(map[string]string{"color": "Red", "colour": "Rouge"})

	var results []*MappedProduct
	for i := 0; i < 20; i++ {
		mapped, err := resolver.Resolve(context.Background(), product)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		results = append(results, mapped)
	}

	first := results[0].Fields["color"]
	if first.SourceAttr != "colour" {
		t.Fatalf("expected the smaller rule ID (rule-a, source colour) to win the tie, got %+v", first)
	}
	for i, r := range results {
		if r.Fields["color"] != first {
			t.Errorf("run %d: non-deterministic result: %+v vs %+v", i, r.Fields["color"], first)
		}
	}
}

// TestMappedProduct_AttributeMappings_Sorted verifies the persisted mappings
// slot is written in deterministic (sorted) field order.
func TestMappedProduct_AttributeMappings_Sorted(t *testing.T) {
	mapped := &MappedProduct{Fields: map[string]FieldResolution{
		"size":  {Tier: TierUnresolved},
		"color": {SourceAttr: "color", Value: "Red", Confidence: 0.9, Tier: TierRule},
		"brand": {Tier: TierUnresolved},
	}}

	mappings := mapped.AttributeMappings()
	if len(mappings) != 3 {
		t.Fatalf("expected 3 attribute mappings, got %d", len(mappings))
	}
	for i := 1; i < len(mappings); i++ {
		if mappings[i-1].CatalogAttribute >= mappings[i].CatalogAttribute {
			t.Errorf("expected sorted CatalogAttribute order, got %v then %v", mappings[i-1].CatalogAttribute, mappings[i].CatalogAttribute)
		}
	}
}

// TestMappedProduct_ApplyTo verifies ApplyTo writes the rich mappings slot
// and mirrors only resolved values into the legacy MappedFields map that
// scoring (stage 3) reads.
func TestMappedProduct_ApplyTo(t *testing.T) {
	mapped := &MappedProduct{Fields: map[string]FieldResolution{
		"color": {SourceAttr: "color", Value: "Red", Confidence: 0.9, Tier: TierRule},
		"brand": {Tier: TierUnresolved},
	}}

	product := testResolveProduct(map[string]string{"color": "Red"})
	mapped.ApplyTo(product)

	if len(product.AttributeMappings) != 2 {
		t.Fatalf("expected 2 attribute mappings persisted, got %d", len(product.AttributeMappings))
	}
	if product.MappedFields["color"] != "Red" {
		t.Errorf("expected resolved color mirrored into MappedFields, got %q", product.MappedFields["color"])
	}
	if _, ok := product.MappedFields["brand"]; ok {
		t.Errorf("expected unresolved brand to be absent from MappedFields, got %q", product.MappedFields["brand"])
	}
}

// TestOntologyRule_NoProductOrTenantData guards the cross-tenant invariant:
// the ontology library's persisted rule/learned records must never carry
// product values or store/merchant identity — only attribute name/shape and
// confidence.
func TestOntologyRule_NoProductOrTenantData(t *testing.T) {
	forbidden := []string{"store_id", "merchant_id", "sku", "product_id", "value"}

	rt := reflect.TypeOf(OntologyRule{})
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("bson")
		name := strings.Split(tag, ",")[0]
		for _, f := range forbidden {
			if name == f {
				t.Errorf("OntologyRule field %q looks like product/tenant data, violating the cross-tenant ontology library invariant", name)
			}
		}
	}
}

// TestResolveAndPersist_WritesMappingsSlot is the integration test: a
// normalised product + a target field catalogue + the ontology (rule and
// learned tiers) produce a MappedProduct that is written to the product's
// mappings slot via the real CatalogStore write seam.
func TestResolveAndPersist_WritesMappingsSlot(t *testing.T) {
	ontology := newFakeOntologyStore()
	ontology.rules["rule-color"] = OntologyRule{
		ID: "rule-color", SourceAttribute: "color", TargetField: "color",
		Platform: "*", Tier: TierRule, Confidence: 0.95, TransformFn: "identity",
	}
	ontology.rules["learned-size"] = OntologyRule{
		ID: "learned-size", SourceAttribute: "sz", TargetField: "size",
		Platform: "*", Tier: TierLearned, Confidence: 0.8, TransformFn: "enum_map",
		TransformConfig: map[string]interface{}{
			"mapping": map[string]interface{}{"XL-EU52": "XL"},
		},
	}

	resolver := NewOntologyResolver(ontology, testCatalogue("color", "size", "brand"), testLog())
	store := newFakeCatalogStore()

	product := testResolveProduct(map[string]string{
		"color": "Red",
		"sz":    "XL-EU52",
	})

	mapped, err := ResolveAndPersist(context.Background(), resolver, store, product)
	if err != nil {
		t.Fatalf("resolve and persist: %v", err)
	}
	if len(mapped.Fields) != 3 {
		t.Fatalf("expected 3 resolved fields, got %d", len(mapped.Fields))
	}

	persisted, err := store.GetProduct(context.Background(), "store-1", "SKU-1")
	if err != nil {
		t.Fatalf("get persisted product: %v", err)
	}

	byField := make(map[string]AttributeMapping)
	for _, am := range persisted.AttributeMappings {
		byField[am.CatalogAttribute] = am
	}

	if am := byField["color"]; am.Tier != TierRule || am.Value != "Red" || am.SourceAttribute != "color" || am.Confidence != 0.95 {
		t.Errorf("expected color resolved via rule tier and persisted, got %+v", am)
	}
	if am := byField["size"]; am.Tier != TierLearned || am.Value != "XL" || am.SourceAttribute != "sz" || am.Confidence != 0.8 {
		t.Errorf("expected size resolved via learned tier and persisted, got %+v", am)
	}
	if am := byField["brand"]; am.Tier != TierUnresolved || am.Value != "" || am.SourceAttribute != "" {
		t.Errorf("expected brand unresolved and persisted (not dropped), got %+v", am)
	}

	// No product values or tenant identity leaked into the ontology library
	// itself: assert the rule documents used still carry none.
	for _, rule := range ontology.rules {
		if rule.SourceAttribute == persisted.SKU || rule.TargetField == persisted.StoreID {
			t.Errorf("ontology rule appears to carry product/tenant data: %+v", rule)
		}
	}
}
