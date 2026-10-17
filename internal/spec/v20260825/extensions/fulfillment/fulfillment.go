// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package fulfillment implements the UCP Fulfillment extension
// (dev.ucp.shopping.fulfillment): hierarchical fulfillment (methods,
// destinations, groups) on Checkout, catalog-level fulfillment discovery
// on Product/Variant (new in this spec version), plus the capability
// descriptor itself.
//
// The extension schema's FulfillmentOption/Group/Method/AvailableMethod/
// Fulfillment $defs are bare $refs to the base type schemas — the
// extension contributes no new fields for them — so this package aliases
// the base package's types rather than redeclaring them, same as
// v20260408.
//
// New in this spec version: the extension also composes onto the catalog
// capabilities (Catalog Search, Catalog Lookup), attaching a
// CatalogFulfillment preview (a lighter-weight sibling of the checkout
// Fulfillment shape) to each catalog Variant, and extending SearchFilters
// with a fulfillment destination filter and a method-type filter. This
// package models those building blocks (CatalogFulfillmentMethod,
// CatalogFulfillment, SearchFiltersWithFulfillment) but not every
// generated request/response wrapper combination the schema spells out
// (fulfillment_search_response, fulfillment_lookup_product, etc.) — see
// DISCREPANCIES.md for why those are mechanical Product/Variant
// compositions callers can build themselves, the same way this package's
// base already expects callers to compose CatalogLookupProduct or
// DetailProduct from Product.
package fulfillment

import (
	"encoding/json"
	"fmt"

	v20260825 "github.com/sarvexlabs/sarvex.cp/internal/spec/v20260825"
)

// marshalWithExtra marshals named (typically a private alias struct
// mirroring a type's fixed fields) and overlays extra on top, so that
// unknown/additional properties round-trip losslessly. Named fields take
// precedence on key collision. Mirrors the base package's json.go helper
// of the same name/shape, duplicated here since this package intentionally
// has no dependency beyond the base v20260825 package.
func marshalWithExtra(named any, extra map[string]any) ([]byte, error) {
	body, err := json.Marshal(named)
	if err != nil {
		return nil, err
	}
	if len(extra) == 0 {
		return body, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	for k, v := range extra {
		if _, exists := m[k]; exists {
			continue
		}
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		m[k] = b
	}
	return json.Marshal(m)
}

// unmarshalWithExtra unmarshals data into named (a pointer to an alias
// struct mirroring a type's fixed fields), then returns every top-level
// key from data that is not in knownKeys as an "extra" map, preserving
// unknown fields for lossless round-tripping.
func unmarshalWithExtra(data []byte, named any, knownKeys []string) (map[string]any, error) {
	if err := json.Unmarshal(data, named); err != nil {
		return nil, err
	}
	var all map[string]any
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, err
	}
	for _, k := range knownKeys {
		delete(all, k)
	}
	if len(all) == 0 {
		return nil, nil
	}
	return all, nil
}

// FulfillmentOption aliases the base Fulfillment Option type; the
// extension schema contributes no additional fields.
type FulfillmentOption = v20260825.FulfillmentOption

// FulfillmentOptionBase aliases the base Fulfillment Option Base type,
// used directly by catalog-level fulfillment previews (new in this spec
// version — see CatalogFulfillmentMethod).
type FulfillmentOptionBase = v20260825.FulfillmentOptionBase

// FulfillmentGroup aliases the base Fulfillment Group type; the extension
// schema contributes no additional fields.
type FulfillmentGroup = v20260825.FulfillmentGroup

// FulfillmentMethod aliases the base Fulfillment Method type; the
// extension schema contributes no additional fields.
type FulfillmentMethod = v20260825.FulfillmentMethod

// FulfillmentAvailableMethod aliases the base Fulfillment Available
// Method type; the extension schema contributes no additional fields.
type FulfillmentAvailableMethod = v20260825.FulfillmentAvailableMethod

// Fulfillment aliases the base Fulfillment container type; the extension
// schema contributes no additional fields.
type Fulfillment = v20260825.Fulfillment

// CheckoutWithFulfillment is Checkout extended with hierarchical
// fulfillment. Composes the base Checkout type rather than duplicating
// its fields.
type CheckoutWithFulfillment struct {
	v20260825.Checkout
	Fulfillment *Fulfillment `json:"fulfillment,omitempty"`
}

// CatalogFulfillmentMethod is a fulfillment method on a catalog Variant:
// how the variant can be fulfilled, and its availability. New in this
// spec version — a lighter-weight, catalog-context sibling of
// FulfillmentMethod (no destinations/groups/selection state, since no
// checkout session exists yet).
type CatalogFulfillmentMethod struct {
	Type v20260825.FulfillmentMethodType `json:"type"`
	// Description is a short buyer-facing summary (e.g. "Ships in 2-4
	// business days").
	Description v20260825.Description `json:"description,omitempty"`
	// Availability is this variant's availability via this method at the
	// specified or inferred location.
	Availability *v20260825.Availability `json:"availability,omitempty"`
	// Location is a stable, opaque identifier for the Business Location
	// resolved for this place-based fulfillment method.
	Location string `json:"location,omitempty"`
	// Options are representative fulfillment options for this method
	// (e.g. cheapest, fastest boundary options); more specific options
	// are negotiated in Checkout once line items and destination are known.
	Options []FulfillmentOptionBase `json:"options,omitempty"`

	// Extra carries any additional fields not modeled above; the schema
	// allows additional properties.
	Extra map[string]any `json:"-"`
}

type catalogFulfillmentMethodAlias struct {
	Type         v20260825.FulfillmentMethodType `json:"type"`
	Description  v20260825.Description           `json:"description,omitempty"`
	Availability *v20260825.Availability         `json:"availability,omitempty"`
	Location     string                          `json:"location,omitempty"`
	Options      []FulfillmentOptionBase         `json:"options,omitempty"`
}

var catalogFulfillmentMethodKnownKeys = []string{"type", "description", "availability", "location", "options"}

// UnmarshalJSON preserves any additional method properties in Extra.
func (m *CatalogFulfillmentMethod) UnmarshalJSON(data []byte) error {
	var alias catalogFulfillmentMethodAlias
	extra, err := unmarshalWithExtra(data, &alias, catalogFulfillmentMethodKnownKeys)
	if err != nil {
		return fmt.Errorf("ucp/fulfillment: catalog fulfillment method: %w", err)
	}
	*m = CatalogFulfillmentMethod{
		Type: alias.Type, Description: alias.Description, Availability: alias.Availability,
		Location: alias.Location, Options: alias.Options, Extra: extra,
	}
	return nil
}

// MarshalJSON re-adds any additional method properties from Extra.
func (m CatalogFulfillmentMethod) MarshalJSON() ([]byte, error) {
	alias := catalogFulfillmentMethodAlias{
		Type: m.Type, Description: m.Description, Availability: m.Availability,
		Location: m.Location, Options: m.Options,
	}
	return marshalWithExtra(alias, m.Extra)
}

// CatalogFulfillment is how a catalog Variant can be fulfilled, mirroring
// checkout Fulfillment. New in this spec version.
type CatalogFulfillment struct {
	Methods []CatalogFulfillmentMethod `json:"methods,omitempty"`

	// Extra carries any additional fields not modeled above; the schema
	// allows additional properties.
	Extra map[string]any `json:"-"`
}

type catalogFulfillmentAlias struct {
	Methods []CatalogFulfillmentMethod `json:"methods,omitempty"`
}

var catalogFulfillmentKnownKeys = []string{"methods"}

// UnmarshalJSON preserves any additional properties in Extra.
func (f *CatalogFulfillment) UnmarshalJSON(data []byte) error {
	var alias catalogFulfillmentAlias
	extra, err := unmarshalWithExtra(data, &alias, catalogFulfillmentKnownKeys)
	if err != nil {
		return fmt.Errorf("ucp/fulfillment: catalog fulfillment: %w", err)
	}
	*f = CatalogFulfillment{Methods: alias.Methods, Extra: extra}
	return nil
}

// MarshalJSON re-adds any additional properties from Extra.
func (f CatalogFulfillment) MarshalJSON() ([]byte, error) {
	alias := catalogFulfillmentAlias{Methods: f.Methods}
	return marshalWithExtra(alias, f.Extra)
}

// FulfillmentVariant is a catalog Variant enriched with a CatalogFulfillment
// preview. Composes the base Variant type rather than duplicating its
// fields.
type FulfillmentVariant struct {
	v20260825.Variant
	Fulfillment *CatalogFulfillment `json:"fulfillment,omitempty"`
}

// SearchFiltersWithFulfillment is SearchFilters extended with a
// fulfillment destination filter and a method-type filter, used by the
// Catalog Search/Lookup requests when the fulfillment extension is
// active. Composes the base SearchFilters type rather than duplicating
// its fields.
type SearchFiltersWithFulfillment struct {
	v20260825.SearchFilters
	// FulfillsTo is the explicit destination where items are fulfilled;
	// it may differ from the locality or business location supplied in
	// Context (e.g. a gift delivered directly to the recipient).
	FulfillsTo *v20260825.FulfillmentDestinationFilter `json:"fulfills_to,omitempty"`
	// Methods restricts results to these fulfillment method types (e.g.
	// ["pickup"]).
	Methods []string `json:"methods,omitempty"`
}

// UnmarshalJSON populates the embedded SearchFilters (including its
// Extra passthrough), FulfillsTo, and Methods.
func (f *SearchFiltersWithFulfillment) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &f.SearchFilters); err != nil {
		return err
	}
	var overlay struct {
		FulfillsTo *v20260825.FulfillmentDestinationFilter `json:"fulfills_to"`
		Methods    []string                                `json:"methods"`
	}
	if err := json.Unmarshal(data, &overlay); err != nil {
		return fmt.Errorf("ucp/fulfillment: search filters with fulfillment: %w", err)
	}
	f.FulfillsTo = overlay.FulfillsTo
	f.Methods = overlay.Methods
	return nil
}

// MarshalJSON layers FulfillsTo and Methods on top of the embedded
// SearchFilters' fields. Defined explicitly so SearchFilters.MarshalJSON
// is not promoted verbatim, which would otherwise silently drop them.
func (f SearchFiltersWithFulfillment) MarshalJSON() ([]byte, error) {
	body, err := json.Marshal(f.SearchFilters)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	if f.FulfillsTo != nil {
		b, err := json.Marshal(f.FulfillsTo)
		if err != nil {
			return nil, err
		}
		m["fulfills_to"] = b
	}
	if f.Methods != nil {
		b, err := json.Marshal(f.Methods)
		if err != nil {
			return nil, err
		}
		m["methods"] = b
	}
	return json.Marshal(m)
}

// PlatformCapability is the dev.ucp.shopping.fulfillment capability
// descriptor for platform-level discovery. Composes the base Capability
// type rather than duplicating its fields; Config shadows
// Capability.Entity.Config with the typed platform fulfillment config.
type PlatformCapability struct {
	v20260825.Capability
	Config *v20260825.PlatformFulfillmentConfig `json:"config,omitempty"`
}

// UnmarshalJSON populates the embedded Capability and the typed Config.
func (c *PlatformCapability) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &c.Capability); err != nil {
		return err
	}
	var cfg struct {
		Config *v20260825.PlatformFulfillmentConfig `json:"config"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("ucp/fulfillment: platform capability: %w", err)
	}
	c.Config = cfg.Config
	return nil
}

// MarshalJSON layers the typed Config on top of the embedded Capability's
// fields. Defined explicitly so Capability.MarshalJSON is not promoted
// verbatim, which would otherwise silently drop the typed Config.
func (c PlatformCapability) MarshalJSON() ([]byte, error) {
	m, err := c.FieldMap()
	if err != nil {
		return nil, err
	}
	if c.Config != nil {
		body, err := json.Marshal(c.Config)
		if err != nil {
			return nil, err
		}
		m["config"] = body
	}
	return json.Marshal(m)
}

// BusinessCapability is the dev.ucp.shopping.fulfillment capability
// descriptor for business-level configuration. Composes the base
// Capability type rather than duplicating its fields; Config shadows
// Capability.Entity.Config with the typed business fulfillment config.
type BusinessCapability struct {
	v20260825.Capability
	Config *v20260825.BusinessFulfillmentConfig `json:"config,omitempty"`
}

// UnmarshalJSON populates the embedded Capability and the typed Config.
func (c *BusinessCapability) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &c.Capability); err != nil {
		return err
	}
	var cfg struct {
		Config *v20260825.BusinessFulfillmentConfig `json:"config"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("ucp/fulfillment: business capability: %w", err)
	}
	c.Config = cfg.Config
	return nil
}

// MarshalJSON layers the typed Config on top of the embedded Capability's
// fields. Defined explicitly so Capability.MarshalJSON is not promoted
// verbatim, which would otherwise silently drop the typed Config.
func (c BusinessCapability) MarshalJSON() ([]byte, error) {
	m, err := c.FieldMap()
	if err != nil {
		return nil, err
	}
	if c.Config != nil {
		body, err := json.Marshal(c.Config)
		if err != nil {
			return nil, err
		}
		m["config"] = body
	}
	return json.Marshal(m)
}
