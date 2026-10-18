// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package sync

import "fmt"

// remediationField ties a rubric field to where its value lives on the
// canonical product and to the key the connectors' UpdateProduct understands
// (name, description, stock_quantity, ... — the vocabulary documented on
// woocommerce.Connector.UpdateProduct). The rubric speaks in its own field
// names; a fix is only as real as its path to the merchant's store, so a field
// absent from this table has no rule-based fix.
type remediationField struct {
	get      func(*CatalogProduct) string
	set      func(*CatalogProduct, string)
	storeKey string
}

// remediationFields is the set of fields a rule-based fix can safely change in
// the merchant's store. The rest (stock, shipping weight and dimensions) are
// deliberately absent: a stock count or a shipping weight is the merchant's
// fact, and writing a default over it — a 0 weight that zeroes shipping quotes
// — would be worse than the gap it fills.
var remediationFields = map[string]remediationField{
	"title": {
		get:      func(p *CatalogProduct) string { return p.Title },
		set:      func(p *CatalogProduct, v string) { p.Title = v },
		storeKey: "name",
	},
	"description": {
		get:      func(p *CatalogProduct) string { return p.Description },
		set:      func(p *CatalogProduct, v string) { p.Description = v },
		storeKey: "description",
	},
}

// remediationApplicable reports whether rem can be applied as a rule: it must
// change a field with a store write-back path, and do something — a default
// needs a value to write, and normalize is only defined for titles.
func remediationApplicable(rem *RemediationRule) bool {
	if rem == nil {
		return false
	}
	if _, ok := remediationFields[rem.Field]; !ok {
		return false
	}
	switch rem.Action {
	case "default":
		return rem.Value != ""
	case "normalize":
		return rem.Field == "title"
	default:
		return false
	}
}

// remediatedValue is the value rem leaves in its field on p, and whether rem
// applies at all. It does not modify p.
func remediatedValue(p *CatalogProduct, rem *RemediationRule) (string, bool) {
	if !remediationApplicable(rem) {
		return "", false
	}
	current := remediationFields[rem.Field].get(p)
	switch rem.Action {
	case "default":
		if current != "" {
			return current, true // a default fills a gap; it never overwrites
		}
		return rem.Value, true
	case "normalize":
		return normalizeTitle(current, remediationMaxLength(rem)), true
	}
	return "", false
}

// remediationMaxLength reads a normalize remediation's max_length param,
// falling back to the length the title rule checks against.
func remediationMaxLength(rem *RemediationRule) int {
	switch v := rem.Params["max_length"].(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return titleMaxLength
}

// ApplyRemediation applies a remediation rule to the product's field in place.
// It returns an error when the remediation is not one a rule can apply (the
// merchant has to fix it).
func ApplyRemediation(product *CatalogProduct, remRule *RemediationRule) error {
	value, ok := remediatedValue(product, remRule)
	if !ok {
		return fmt.Errorf("manual intervention required: %s", remRule.Field)
	}
	remediationFields[remRule.Field].set(product, value)
	return nil
}
