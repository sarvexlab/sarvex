// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"testing"
	"time"
)

// scoredProduct returns a product that satisfies every rule in the rubric.
// Tests knock fields out of it to trigger exactly the rules they are about.
func scoredProduct() *CatalogProduct {
	return &CatalogProduct{
		ID:         "m1:sku1",
		MerchantID: "m1",
		SKU:        "sku1",
		Title:      "Test Product",
		PriceMinor: 1000,
		Currency:   "USD",
		InStock:    true,
		Quantity:   intPtr(10),
		// Description and Images are typed fields the connector fills.
		Description:   "A nice test product",
		Images:        []string{"https://example.com/image.jpg"},
		RawAttributes: map[string]string{},
		MappedFields: map[string]string{
			"shipping_weight":     "0.5",
			"shipping_dimensions": "10x10x10",
		},
		UpdatedAt: time.Now(),
	}
}

func issueIDs(r *ReadinessResult) map[string]ProductIssue {
	out := make(map[string]ProductIssue, len(r.Issues))
	for _, iss := range r.Issues {
		out[iss.IssueID] = iss
	}
	return out
}

func TestEvaluateReadiness_AllFieldsValid(t *testing.T) {
	r := EvaluateReadiness(scoredProduct(), testRubric(t), nil)

	if r.Overall != 100 {
		t.Errorf("complete product should score 100, got %d (issues: %+v)", r.Overall, r.Issues)
	}
	if len(r.Issues) != 0 {
		t.Errorf("complete product should have no issues, got %+v", r.Issues)
	}
	if anyBlocksCheckout(r.Issues) {
		t.Error("complete product should not block checkout")
	}
}

func TestEvaluateReadiness_MissingBlockingFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(p *CatalogProduct)
		rule   string
	}{
		{"missing SKU", func(p *CatalogProduct) { p.SKU = "" }, "rule_sku_missing"},
		{"missing title", func(p *CatalogProduct) { p.Title = "" }, "rule_title_missing"},
		{"missing price", func(p *CatalogProduct) { p.PriceMinor = 0 }, "rule_price_missing"},
		{"missing currency", func(p *CatalogProduct) { p.Currency = "" }, "rule_currency_missing"},
	}

	rubric := testRubric(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := scoredProduct()
			tt.mutate(p)
			r := EvaluateReadiness(p, rubric, nil)

			iss, ok := issueIDs(r)[tt.rule]
			if !ok {
				t.Fatalf("expected issue %s, got %+v", tt.rule, r.Issues)
			}
			if !iss.BlocksCheckout || !anyBlocksCheckout(r.Issues) {
				t.Errorf("%s must block checkout", tt.rule)
			}
			if r.Overall >= 100 {
				t.Errorf("blocking product should have a reduced score, got %d", r.Overall)
			}
		})
	}
}

// The score is the dimension-weighted average of per-dimension scores, each
// the share of that dimension's rule weight that did not fail. This pins the
// arithmetic the console shows (the sim product: description + both shipping
// fields absent => 98/100/100/0 => 84).
func TestEvaluateReadiness_ScoreComposition(t *testing.T) {
	rubric := testRubric(t)

	t.Run("description and delivery data absent", func(t *testing.T) {
		p := scoredProduct()
		p.Description = ""
		p.MappedFields = map[string]string{}
		r := EvaluateReadiness(p, rubric, nil)

		want := ReadinessDimensions{Completeness: 98, Consistency: 100, AttributeQuality: 100, DeliveryData: 0}
		if r.Dimensions != want {
			t.Errorf("dimensions = %+v, want %+v", r.Dimensions, want)
		}
		if r.Overall != 84 {
			t.Errorf("overall = %d, want 84", r.Overall)
		}
		ids := issueIDs(r)
		for _, id := range []string{"rule_description_missing", "rule_shipping_weight_missing", "rule_shipping_dimensions_missing"} {
			if _, ok := ids[id]; !ok {
				t.Errorf("expected issue %s, got %+v", id, r.Issues)
			}
		}
		if len(r.Issues) != 3 || anyBlocksCheckout(r.Issues) {
			t.Errorf("want 3 non-blocking issues, got %+v", r.Issues)
		}
	})

	t.Run("missing title", func(t *testing.T) {
		p := scoredProduct()
		p.Title = ""
		r := EvaluateReadiness(p, rubric, nil)
		if r.Dimensions.Completeness != 83 || r.Overall != 94 {
			t.Errorf("completeness/overall = %d/%d, want 83/94", r.Dimensions.Completeness, r.Overall)
		}
	})
}

func TestEvaluateReadiness_NonBlockingIssueDoesNotBlock(t *testing.T) {
	p := scoredProduct()
	p.Description = ""
	r := EvaluateReadiness(p, testRubric(t), nil)

	if anyBlocksCheckout(r.Issues) {
		t.Error("a missing description must not block checkout")
	}
}

func TestEvaluateReadiness_FreeTextTitle(t *testing.T) {
	p := scoredProduct()
	p.RawAttributes["title"] = "RAW TITLE"
	p.MappedFields["title"] = "RAW TITLE" // mapped == raw: never normalised
	r := EvaluateReadiness(p, testRubric(t), nil)

	iss, ok := issueIDs(r)["rule_title_free_text"]
	if !ok {
		t.Fatalf("expected rule_title_free_text, got %+v", r.Issues)
	}
	if iss.BlocksCheckout || iss.Dimension != "attribute_quality" {
		t.Errorf("free-text title should be a non-blocking attribute_quality issue, got %+v", iss)
	}
	if r.Dimensions.AttributeQuality >= 100 {
		t.Errorf("attribute quality should drop, got %d", r.Dimensions.AttributeQuality)
	}
}

func TestEvaluateReadiness_MultipleBlockingIssuesAreClamped(t *testing.T) {
	p := &CatalogProduct{RawAttributes: map[string]string{}, MappedFields: map[string]string{}}
	r := EvaluateReadiness(p, testRubric(t), nil)

	if !anyBlocksCheckout(r.Issues) {
		t.Error("an empty product must block checkout")
	}
	for name, v := range map[string]int{
		"overall": r.Overall, "completeness": r.Dimensions.Completeness,
		"consistency": r.Dimensions.Consistency, "attribute_quality": r.Dimensions.AttributeQuality,
		"delivery_data": r.Dimensions.DeliveryData,
	} {
		if v < 0 || v > 100 {
			t.Errorf("%s = %d, want within [0,100]", name, v)
		}
	}
	if len(r.Issues) < 3 {
		t.Errorf("expected several issues, got %d", len(r.Issues))
	}
}

func TestEvaluateReadiness_IssueShape(t *testing.T) {
	p := scoredProduct()
	p.Title = ""
	r := EvaluateReadiness(p, testRubric(t), nil)

	iss, ok := issueIDs(r)["rule_title_missing"]
	if !ok {
		t.Fatalf("expected rule_title_missing, got %+v", r.Issues)
	}
	if iss.Field != "title" || iss.Problem != "missing" || iss.Dimension != "completeness" {
		t.Errorf("unexpected issue shape %+v", iss)
	}
	if iss.Remediable != "manual" {
		t.Errorf("a required title cannot be auto-remediated, got remediable=%q", iss.Remediable)
	}
	if iss.WhyItMatters == "" {
		t.Error("issue should carry a why-it-matters explanation")
	}
}

func TestEvaluateReadiness_Deterministic(t *testing.T) {
	rubric := testRubric(t)
	a := EvaluateReadiness(scoredProduct(), rubric, nil)
	b := EvaluateReadiness(scoredProduct(), rubric, nil)
	if a.Overall != b.Overall || a.Dimensions != b.Dimensions || len(a.Issues) != len(b.Issues) {
		t.Errorf("same input produced different results: %+v vs %+v", a, b)
	}
}

// Utility helper
func intPtr(i int) *int {
	return &i
}

// Every "missing" rule in the rubric must be answerable: knocking its field
// out of an otherwise complete product has to raise exactly that rule. This is
// what catches a rule whose field the evaluator has no case for (it would sit
// in the rubric forever and never fire).
func TestEvaluateReadiness_EveryMissingRuleCanFire(t *testing.T) {
	knockOut := map[string]func(p *CatalogProduct){
		"sku":                 func(p *CatalogProduct) { p.SKU = "" },
		"title":               func(p *CatalogProduct) { p.Title = "" },
		"price":               func(p *CatalogProduct) { p.PriceMinor = 0 },
		"currency":            func(p *CatalogProduct) { p.Currency = "" },
		"description":         func(p *CatalogProduct) { p.Description = "" },
		"images":              func(p *CatalogProduct) { p.Images = nil },
		"inventory":           func(p *CatalogProduct) { p.Quantity, p.InStock = nil, false },
		"shipping_weight":     func(p *CatalogProduct) { delete(p.MappedFields, "shipping_weight") },
		"shipping_dimensions": func(p *CatalogProduct) { delete(p.MappedFields, "shipping_dimensions") },
	}

	rubric := testRubric(t)
	for _, rule := range rubric.ScoringRules {
		if rule.Condition != "missing" {
			continue
		}
		t.Run(rule.ID, func(t *testing.T) {
			mutate, ok := knockOut[rule.Field]
			if !ok {
				t.Fatalf("rule %s targets field %q but this test has no way to knock it out — add one", rule.ID, rule.Field)
			}
			p := scoredProduct()
			mutate(p)
			if _, fired := issueIDs(EvaluateReadiness(p, rubric, nil))[rule.ID]; !fired {
				t.Errorf("rule %s never fires when %q is absent", rule.ID, rule.Field)
			}
		})
	}
}

func TestEvaluateReadiness_UntrackedInStockIsNotMissingInventory(t *testing.T) {
	p := scoredProduct()
	p.Quantity, p.InStock = nil, true // manage_stock off, in stock
	if _, fired := issueIDs(EvaluateReadiness(p, testRubric(t), nil))["rule_inventory_missing"]; fired {
		t.Error("an in-stock product with untracked quantity must not raise rule_inventory_missing")
	}
}
