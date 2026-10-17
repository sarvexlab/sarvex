// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/currency"
)

// checkRuleCondition evaluates if a rule's condition is met in the product.
func checkRuleCondition(product *CatalogProduct, rule ScoringRule) bool {
	switch rule.Condition {
	case "missing":
		return isMissing(product, rule.Field)
	case "free_text":
		return isFreeText(product, rule.Field)
	case "non_standard_enum":
		return isNonStandardEnum(product, rule.Field)
	case "inconsistent":
		// Consistency checks are fed by external consistency engine (not implemented here)
		// For now, return false (no inconsistencies detected)
		return false
	default:
		return false
	}
}

// isMissing checks if a field is empty or zero-valued in the product.
func isMissing(product *CatalogProduct, field string) bool {
	switch field {
	case "sku":
		return product.SKU == ""
	case "title", "name":
		return product.Title == ""
	case "price":
		// Zero (or unparseable) only: a negative price is present but invalid,
		// which rule_price_negative reports — counting it here too would raise
		// two blocking tasks for one defect.
		return product.PriceMinor == 0
	case "currency":
		return product.Currency == ""
	case "description":
		return product.Description == "" && product.MappedFields["description"] == ""
	case "images":
		return len(product.Images) == 0 && product.RawAttributes["image_url"] == "" && product.MappedFields["image_url"] == ""
	case "inventory":
		// Nothing says how many are in stock and the store does not say it is
		// in stock: the agent has no stock signal at all. An untracked-stock
		// product that is in stock (Quantity nil, InStock true) is fine.
		return product.Quantity == nil && !product.InStock
	case "image_url":
		return product.RawAttributes["image_url"] == "" && product.MappedFields["image_url"] == ""
	case "product_url":
		return product.RawAttributes["product_url"] == "" && product.MappedFields["product_url"] == ""
	case "availability":
		// Missing if not explicitly set
		return product.MappedFields["availability"] == ""
	case "shipping_weight":
		return product.MappedFields["shipping_weight"] == ""
	case "shipping_dimensions":
		return product.MappedFields["shipping_dimensions"] == ""
	case "stock_quantity":
		return product.Quantity == nil || *product.Quantity == 0
	case "brand":
		return product.MappedFields["brand"] == ""
	case "gtin":
		return product.MappedFields["gtin"] == ""
	case "mpn":
		return product.MappedFields["mpn"] == ""
	default:
		// Fail closed: a field this function has no explicit case for is
		// missing unless a value exists for it (an absent key used to count as
		// present, so a rule on such a field could never fire).
		return product.MappedFields[field] == "" && product.RawAttributes[field] == ""
	}
}

// isFreeText checks if an attribute is unstructured (free-text) instead of normalized.
func isFreeText(product *CatalogProduct, field string) bool {
	if field == "title" && titleNeedsNormalizing(product.Title) {
		// The title is a canonical product field, not only a raw attribute: it
		// is unstructured when it carries stray whitespace or runs past the
		// length the normalize remediation enforces.
		return true
	}
	// Free-text is detected when mapped field matches raw attribute (not normalized)
	// OR when raw attribute exists but mapped field is empty (not yet processed)
	if raw, hasRaw := product.RawAttributes[field]; hasRaw && raw != "" {
		mapped, hasMapped := product.MappedFields[field]
		// Free-text if: not mapped yet, or mapped same as raw (no normalization done)
		if !hasMapped || mapped == raw {
			return true
		}
		// Also consider non-standard enum as free-text for this field
		return isNonStandardEnum(product, field)
	}
	return false
}

// isNonStandardEnum checks if a field value is not in the standard enum for that field.
func isNonStandardEnum(product *CatalogProduct, field string) bool {
	// price and currency are canonical product fields, not mapped attributes:
	// "standard" for them means a usable amount and an ISO 4217 code.
	switch field {
	case "price":
		return product.PriceMinor < 0
	case "currency":
		if product.Currency == "" {
			return false // missing, reported by the "missing" condition
		}
		_, err := currency.ParseISO(product.Currency)
		return err != nil
	}

	value := product.MappedFields[field]
	if value == "" {
		// Missing values are handled by "missing" condition, not this one
		return false
	}

	// Check against known enums
	switch field {
	case "size":
		sizeEnums := []string{"XS", "S", "M", "L", "XL", "XXL", "OneSize"}
		return !slices.Contains(sizeEnums, value)
	case "color":
		colorEnums := []string{"Black", "White", "Red", "Blue", "Green", "Yellow", "Grey", "Brown", "Pink", "Purple", "Orange", "Beige", "Gold", "Silver", "Navy", "Turquoise"}
		return !slices.Contains(colorEnums, value)
	case "material":
		materialEnums := []string{"Cotton", "Polyester", "Wool", "Silk", "Linen", "Leather", "Nylon", "Spandex", "Acrylic"}
		return !slices.Contains(materialEnums, value)
	case "gtin":
		// GTIN must be 12-13 digits
		return len(value) < 12 || len(value) > 13 || !isDigitsOnly(value)
	default:
		return false
	}
}

// isDigitsOnly checks if a string contains only digits.
func isDigitsOnly(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

// titleMaxLength is the longest title the normalize remediation
// (rem_title_normalize, params.max_length) allows.
const titleMaxLength = 150

// normalizeTitle trims a title, collapses runs of whitespace to one space and
// cuts it to maxLen characters (never mid-character, never leaving trailing
// space).
func normalizeTitle(title string, maxLen int) string {
	out := strings.Join(strings.Fields(title), " ")
	if maxLen > 0 && utf8.RuneCountInString(out) > maxLen {
		out = strings.TrimSpace(string([]rune(out)[:maxLen]))
	}
	return out
}

// titleNeedsNormalizing reports whether normalizeTitle would change the title.
func titleNeedsNormalizing(title string) bool {
	return title != "" && normalizeTitle(title, titleMaxLength) != title
}
