// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package v1

// OntologySeedRule is one rule-tier mapping the ontology ships with: a
// merchant platform's own attribute name resolved onto a catalogue target
// field. It is versioned catalogue content, like the attribute field model it
// targets — the sync plane (the ontology's one writer) loads it into the
// ontology store at startup, so a fresh environment resolves the common
// attributes without anyone hand-seeding Mongo.
//
// Source names are matched case-, space- and dash-insensitively
// (normalizeAttrName in the sync plane), so "Colour", "colour" and "COLOUR"
// are one entry. Platform "*" applies to every platform: the connectors pass
// each store's attribute names through untouched, and the same merchandising
// vocabulary recurs across WooCommerce and Magento.
type OntologySeedRule struct {
	SourceAttribute string
	TargetField     string
	Confidence      float64
}

// Seed-rule ids are namespaced so the seeder only ever touches rows it owns;
// ontology rules authored elsewhere (learned-tier, ops-added) are left alone.
const OntologySeedIDPrefix = "seed:"

// DefaultOntologySeed returns the V1 rule-tier mappings. Every TargetField is
// an id in DefaultAttributeFields (enforced by test).
func DefaultOntologySeed() []OntologySeedRule {
	return []OntologySeedRule{
		// Shipping data. WooCommerce and Magento both serve "weight"; the
		// connectors fold the store's dimension fields into one "dimensions".
		{"weight", "shipping_weight", 0.95},
		{"dimensions", "shipping_dimensions", 0.95},

		// Variant options. WooCommerce global attributes arrive as pa_<name>.
		{"color", "color", 0.95}, {"colour", "color", 0.9}, {"pa_color", "color", 0.9}, {"pa_colour", "color", 0.9},
		{"size", "size", 0.95}, {"pa_size", "size", 0.9},
		{"material", "material", 0.95}, {"pa_material", "material", 0.9}, {"fabric", "material", 0.7},

		// Identifiers.
		{"gtin", "gtin", 0.95}, {"ean", "gtin", 0.9}, {"upc", "gtin", 0.9}, {"isbn", "gtin", 0.8}, {"barcode", "gtin", 0.8},
	}
}
