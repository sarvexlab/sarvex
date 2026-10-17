// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package v1

// AttributeField declares one canonical product attribute a merchant's raw
// attributes may resolve onto — the map stage's (Domain 2a stage 1,
// "resolve") target field model. Loadable, versioned catalogue content:
// adding, removing, or renaming a field here changes what the ontology
// resolves onto with no change to the mapping pipeline's code.
//
// These are *merchandising* attributes, not fields of the UCP protocol, and
// the distinction matters because the naming here previously implied
// otherwise. The pinned spec (internal/spec/v20260408) has no
// brand, mpn, condition, age_group or gender field at all; it carries gtin
// as a Variant barcode of type GTIN, and color/size/material as product
// options. The set below is the merchant-data vocabulary the ontology
// normalises onto — closest in origin to a product feed specification —
// which is a separate concern from what the serving plane publishes.
//
// Surface scopes a field to one agent surface (control's surface registry id).
// Empty means core: it applies to every store. A non-empty Surface means the
// field applies only to stores enrolled on that surface — so a store that
// never uses a given surface is never shown, or scored on, that surface's
// vocabulary.
type AttributeField struct {
	ID          string `json:"id"`                // canonical attribute name, e.g. "color"
	Description string `json:"description"`       // brief description of the attribute
	Surface     string `json:"surface,omitempty"` // "" = core; else the surface id it applies to
}

// DefaultAttributeFields returns the V1 target field set for the map stage:
// the canonical product attributes the ontology (rule + learned tiers)
// resolves a merchant's raw attributes onto.
func DefaultAttributeFields() []AttributeField {
	return []AttributeField{
		// Core: backed by the UCP spec (gtin is a variant barcode; color, size
		// and material are product options) or needed to quote shipping.
		{ID: "gtin", Description: "Global Trade Item Number"},
		{ID: "color", Description: "Product color, standard enum"},
		{ID: "size", Description: "Product size, standard enum"},
		{ID: "material", Description: "Primary material, standard enum"},
		{ID: "shipping_weight", Description: "Shipping weight for fulfillment"},
		{ID: "shipping_dimensions", Description: "Shipping dimensions for fulfillment"},
	}
}
