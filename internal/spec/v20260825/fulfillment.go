// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package v20260825

import (
	"encoding/json"
	"fmt"
	"time"
)

// BusinessLocationDestination is a business-authored, response-only pickup
// destination: a LocationSummary plus the fixed wire discriminator
// "type": "business_location", which FulfillmentDestination's
// (Un)MarshalJSON owns adding/stripping (mirroring how ShippingDestination
// doesn't carry its own "type" field either — see FulfillmentDestination).
// Aliased directly to LocationSummary since location_destination.json
// contributes no fields beyond that fixed discriminator.
//
// Renamed from v20260408's "RetailLocation" to match this spec version's
// schema title ("Business Location Destination") and wire type constant.
type BusinessLocationDestination = LocationSummary

// FulfillmentDestination is a destination for fulfillment: either a
// ShippingDestination (a postal address) or a BusinessLocationDestination
// (a pickup point).
//
// Breaking change from v20260408: the wire shape now carries an explicit
// "type" discriminator ("shipping_address" or "business_location") —
// required in business responses, optional in platform requests — in
// place of v20260408's purely structural dispatch (RetailLocation always
// carries "name", ShippingDestination never does). UnmarshalJSON prefers
// the discriminator when present and falls back to the v20260408
// heuristic when it is absent, so platform requests that omit "type"
// (as the spec permits) still decode correctly.
type FulfillmentDestination struct {
	Shipping *ShippingDestination
	Retail   *BusinessLocationDestination
}

// UnmarshalJSON implements the type-discriminator-first, name-heuristic-
// fallback dispatch documented on FulfillmentDestination.
func (d *FulfillmentDestination) UnmarshalJSON(data []byte) error {
	var probe struct {
		Type string  `json:"type"`
		Name *string `json:"name"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return fmt.Errorf("ucp: fulfillment destination: %w", err)
	}
	useRetail := probe.Type == string(FulfillmentDestinationTypeBusinessLocation)
	useShipping := probe.Type == string(FulfillmentDestinationTypeShippingAddress)
	if !useRetail && !useShipping {
		// Type absent or unrecognized: fall back to the structural
		// heuristic — presence of "name" selects a business location.
		useRetail = probe.Name != nil
	}
	// Strip the "type" discriminator before delegating: it is owned by
	// FulfillmentDestination itself (added back on MarshalJSON), not by
	// the wrapped variant. Left in place, it would round-trip into
	// BusinessLocationDestination's Extra passthrough (LocationSummary's
	// schema has no "type" property of its own), breaking
	// unmarshal->marshal->unmarshal stability.
	withoutType, err := stripKey(data, "type")
	if err != nil {
		return fmt.Errorf("ucp: fulfillment destination: %w", err)
	}
	if useRetail {
		var loc BusinessLocationDestination
		if err := json.Unmarshal(withoutType, &loc); err != nil {
			return fmt.Errorf("ucp: fulfillment destination: business location: %w", err)
		}
		*d = FulfillmentDestination{Retail: &loc}
		return nil
	}
	var ship ShippingDestination
	if err := json.Unmarshal(withoutType, &ship); err != nil {
		return fmt.Errorf("ucp: fulfillment destination: shipping destination: %w", err)
	}
	*d = FulfillmentDestination{Shipping: &ship}
	return nil
}

// stripKey removes one top-level key from a JSON object, returning the
// re-marshaled result.
func stripKey(data []byte, key string) ([]byte, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	delete(m, key)
	return json.Marshal(m)
}

// MarshalJSON round-trips whichever variant is set, injecting the "type"
// discriminator the wire shape now requires.
func (d FulfillmentDestination) MarshalJSON() ([]byte, error) {
	switch {
	case d.Retail != nil:
		m, err := d.Retail.FieldMap()
		if err != nil {
			return nil, err
		}
		typJSON, err := json.Marshal(FulfillmentDestinationTypeBusinessLocation)
		if err != nil {
			return nil, err
		}
		m["type"] = typJSON
		return json.Marshal(m)
	case d.Shipping != nil:
		body, err := json.Marshal(d.Shipping)
		if err != nil {
			return nil, err
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(body, &m); err != nil {
			return nil, err
		}
		typJSON, err := json.Marshal(FulfillmentDestinationTypeShippingAddress)
		if err != nil {
			return nil, err
		}
		m["type"] = typJSON
		return json.Marshal(m)
	default:
		return nil, fmt.Errorf("ucp: fulfillment destination: neither shipping nor retail variant set")
	}
}

// FulfillmentEvent is an append-only event representing an actual
// shipment. References line items by ID.
type FulfillmentEvent struct {
	ID         string `json:"id"`
	OccurredAt string `json:"occurred_at"`
	// Type is an open string. See FulfillmentEventType for well-known values.
	Type           FulfillmentEventType  `json:"type"`
	LineItems      []ExpectationLineItem `json:"line_items"`
	TrackingNumber string                `json:"tracking_number,omitempty"`
	TrackingURL    string                `json:"tracking_url,omitempty"`
	Carrier        string                `json:"carrier,omitempty"`
	Description    string                `json:"description,omitempty"`
}

// Time parses OccurredAt as an RFC 3339 timestamp.
func (e FulfillmentEvent) Time() (time.Time, error) {
	return time.Parse(time.RFC3339, e.OccurredAt)
}

// Validate enforces that TrackingNumber and TrackingURL are present when
// Type is anything other than "processing", per the spec.
func (e FulfillmentEvent) Validate() error {
	if e.Type == FulfillmentEventTypeProcessing {
		return nil
	}
	if e.TrackingNumber == "" {
		return fmt.Errorf("ucp: fulfillment event: tracking_number required when type != processing")
	}
	if e.TrackingURL == "" {
		return fmt.Errorf("ucp: fulfillment event: tracking_url required when type != processing")
	}
	return nil
}

// FulfillmentOptionBase is the common, addressable/renderable shape of a
// fulfillment option (id, title, description) shared between the catalog
// (which uses it directly, as a preview) and checkout (which composes it
// with cost and timing via FulfillmentOption). New in this spec version
// as an explicit base — v20260408 had only the checkout-level shape.
type FulfillmentOptionBase struct {
	ID          string      `json:"id,omitempty"`
	Title       string      `json:"title"`
	Description Description `json:"description,omitempty"`
}

// FulfillmentOption is a fulfillment option within a FulfillmentGroup
// (e.g. Standard Shipping $5, Express $15).
type FulfillmentOption struct {
	FulfillmentOptionBase
	Carrier                 string `json:"carrier,omitempty"`
	EarliestFulfillmentTime string `json:"earliest_fulfillment_time,omitempty"`
	LatestFulfillmentTime   string `json:"latest_fulfillment_time,omitempty"`
	Totals                  Totals `json:"totals"`
}

// FulfillmentGroup is a merchant-generated package/group of line items
// with fulfillment options.
type FulfillmentGroup struct {
	// ID identifies this group for referencing in updates.
	ID          string              `json:"id,omitempty"`
	LineItemIDs []string            `json:"line_item_ids"`
	Options     []FulfillmentOption `json:"options,omitempty"`
	// SelectedOptionID is the ID of the selected option for this group.
	// Nullable: the schema types it ["string", "null"].
	SelectedOptionID *string `json:"selected_option_id,omitempty"`
}

// FulfillmentMethod is a fulfillment method (shipping or pickup) with
// destinations and groups.
type FulfillmentMethod struct {
	// ID uniquely identifies this fulfillment method.
	ID          string                `json:"id,omitempty"`
	Type        FulfillmentMethodType `json:"type"`
	LineItemIDs []string              `json:"line_item_ids"`
	// Destinations lists available destinations: addresses for shipping,
	// business locations for pickup.
	Destinations []FulfillmentDestination `json:"destinations,omitempty"`
	// SelectedDestinationID is the ID of the selected destination.
	// Nullable: the schema types it ["string", "null"].
	SelectedDestinationID *string            `json:"selected_destination_id,omitempty"`
	Groups                []FulfillmentGroup `json:"groups,omitempty"`
}

// FulfillmentAvailableMethod is an inventory availability hint for a
// fulfillment method type.
type FulfillmentAvailableMethod struct {
	Type        FulfillmentMethodType `json:"type"`
	LineItemIDs []string              `json:"line_item_ids"`
	// FulfillableOn is "now" for immediate availability, or an ISO 8601
	// date for future availability (preorders, transfers). Nullable: the
	// schema types it ["string", "null"].
	FulfillableOn *string `json:"fulfillable_on,omitempty"`
	Description   string  `json:"description,omitempty"`
}

// Fulfillment is a container for fulfillment methods and inventory
// availability hints, embedded in Checkout by the fulfillment extension.
type Fulfillment struct {
	Methods []FulfillmentMethod `json:"methods,omitempty"`
	// AvailableMethods carries inventory availability hints.
	AvailableMethods []FulfillmentAvailableMethod `json:"available_methods,omitempty"`
}

// OrderFulfillment is the fulfillment data on an Order: buyer-facing
// expectations plus the append-only event log of what actually shipped.
// The spec still models this as a bare, untitled object (see
// DISCREPANCIES.md — unchanged from v20260408).
type OrderFulfillment struct {
	Expectations []Expectation      `json:"expectations,omitempty"`
	Events       []FulfillmentEvent `json:"events,omitempty"`
}

// MultiDestinationEntry permits one fulfillment method type to span
// multiple destinations within one cart (e.g. split shipping across
// addresses). Listing a method permits it; an omitted method does not.
type MultiDestinationEntry struct {
	Method FulfillmentMethodType `json:"method"`

	// Extra carries any additional per-method constraint fields; the
	// schema allows additional properties.
	Extra map[string]any `json:"-"`
}

// BusinessFulfillmentConfig is a business's fulfillment configuration,
// used as the fulfillment extension's business_schema config.
//
// Breaking change from v20260408: MultiDestination is now a list of
// {method, ...} entries (one per permitted method type) rather than a
// fixed {shipping, pickup} boolean map, and the "allows_" prefix was
// dropped from both field names (AllowsMultiDestination ->
// MultiDestination, AllowsMethodCombinations -> MethodCombinations). See
// DISCREPANCIES.md for how this also resolves v20260408's
// BusinessFulfillmentConfig/MerchantFulfillmentConfig duplication: this
// spec version's schema tree has only one such file.
type BusinessFulfillmentConfig struct {
	MultiDestination   []MultiDestinationEntry   `json:"multi_destination,omitempty"`
	MethodCombinations [][]FulfillmentMethodType `json:"method_combinations,omitempty"`
}

// PlatformFulfillmentConfig is a platform's fulfillment configuration,
// used as the fulfillment extension's platform_schema config.
type PlatformFulfillmentConfig struct {
	// SupportsMultiGroup enables multiple groups per method. Defaults to false.
	SupportsMultiGroup bool `json:"supports_multi_group,omitempty"`
}

// FulfillmentDestinationFilter names a specific destination for the
// fulfillment extension's catalog search/lookup filters (fulfills_to): a
// coarse locality (AddressCountry/AddressRegion/PostalCode), or a
// Location id reference. Platforms SHOULD provide one or the other, not
// both. New in this spec version.
type FulfillmentDestinationFilter struct {
	AddressCountry string `json:"address_country,omitempty"`
	AddressRegion  string `json:"address_region,omitempty"`
	PostalCode     string `json:"postal_code,omitempty"`
	// Location is a reference to the destination (e.g. store, pickup
	// location, saved address).
	Location string `json:"location,omitempty"`

	// Extra carries any additional fields not modeled above; the schema
	// allows additional properties.
	Extra map[string]any `json:"-"`
}
