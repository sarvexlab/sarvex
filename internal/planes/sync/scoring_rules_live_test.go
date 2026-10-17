// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"strings"
	"testing"
)

// The attribute_quality rules must actually be able to fire: they previously
// matched conditions no product could satisfy (price/currency fell through
// isNonStandardEnum's default, title was only checked as a raw attribute).
func TestEvaluateReadiness_AttributeQualityRulesFire(t *testing.T) {
	rubric := testRubric(t)
	tests := []struct {
		name   string
		mutate func(p *CatalogProduct)
		want   string
		block  bool
		absent string // an issue that must NOT also be raised for the same defect
	}{
		{"negative price", func(p *CatalogProduct) { p.PriceMinor = -500 }, "rule_price_negative", true, "rule_price_missing"},
		{"non-ISO currency", func(p *CatalogProduct) { p.Currency = "ZZZ" }, "rule_currency_invalid", true, "rule_currency_missing"},
		{"title with stray whitespace", func(p *CatalogProduct) { p.Title = "  Widget   Pro " }, "rule_title_free_text", false, ""},
		{"title over the length limit", func(p *CatalogProduct) { p.Title = strings.Repeat("a", titleMaxLength+1) }, "rule_title_free_text", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := scoredProduct()
			tt.mutate(p)
			r := EvaluateReadiness(p, rubric, nil)
			ids := issueIDs(r)
			iss, ok := ids[tt.want]
			if !ok {
				t.Fatalf("expected %s, got %+v", tt.want, r.Issues)
			}
			if iss.BlocksCheckout != tt.block {
				t.Errorf("%s blocks_checkout = %v, want %v", tt.want, iss.BlocksCheckout, tt.block)
			}
			if tt.absent != "" {
				if _, dup := ids[tt.absent]; dup {
					t.Errorf("one defect must not raise both %s and %s", tt.want, tt.absent)
				}
			}
		})
	}
}

// Valid values must not trip the same rules.
func TestEvaluateReadiness_AttributeQualityRulesQuietOnValidData(t *testing.T) {
	rubric := testRubric(t)
	for _, cur := range []string{"USD", "EUR", "JPY", "GBP"} {
		p := scoredProduct()
		p.Currency = cur
		p.Title = "Widget Pro"
		r := EvaluateReadiness(p, rubric, nil)
		if len(r.Issues) != 0 {
			t.Errorf("currency %s: expected no issues, got %+v", cur, r.Issues)
		}
	}
}

func TestNormalizeTitle(t *testing.T) {
	long := strings.Repeat("é", titleMaxLength+20)
	cases := []struct{ in, want string }{
		{"  Widget   Pro ", "Widget Pro"},
		{"Widget Pro", "Widget Pro"},
		{long, strings.Repeat("é", titleMaxLength)},
		{strings.Repeat("a", titleMaxLength-1) + " b", strings.Repeat("a", titleMaxLength-1)}, // cut lands on the space; no trailing space kept
	}
	for _, c := range cases {
		if got := normalizeTitle(c.in, titleMaxLength); got != c.want {
			t.Errorf("normalizeTitle(%q...) = %q..., want %q...", c.in[:min(len(c.in), 12)], got[:min(len(got), 12)], c.want[:min(len(c.want), 12)])
		}
	}
	if titleNeedsNormalizing("") {
		t.Error("an empty title is missing, not unnormalized")
	}
}
