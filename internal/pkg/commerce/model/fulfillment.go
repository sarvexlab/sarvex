// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"encoding/json"
	"fmt"
	"time"
)

// FulfillmentDestination is a destination for fulfillment: either a
// ShippingDestination (a postal address) or a RetailLocation (a pickup
// point). The two are distinguished structurally, since the wire format
// has no discriminator field: a RetailLocation always carries "name"
// (required by its schema), which ShippingDestination — a PostalAddress
// plus an id — never does. Presence of "name" therefore selects
// RetailLocation; its absence selects ShippingDestination.
type FulfillmentDestination struct {
	Shipping *ShippingDestination
	Retail   *RetailLocation
}

// UnmarshalJSON implements the RetailLocation-has-"name" heuristic
// documented on FulfillmentDestination.
func (d *FulfillmentDestination) UnmarshalJSON(data []byte) error {
	var probe struct {
		Name *string `json:"name"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return fmt.Errorf("ucp: fulfillment destination: %w", err)
	}
	if probe.Name != nil {
		var loc RetailLocation
		if err := json.Unmarshal(data, &loc); err != nil {
			return fmt.Errorf("ucp: fulfillment destination: retail location: %w", err)
		}
		*d = FulfillmentDestination{Retail: &loc}
		return nil
	}
	var ship ShippingDestination
	if err := json.Unmarshal(data, &ship); err != nil {
		return fmt.Errorf("ucp: fulfillment destination: shipping destination: %w", err)
	}
	*d = FulfillmentDestination{Shipping: &ship}
	return nil
}

// MarshalJSON round-trips whichever variant is set.
func (d FulfillmentDestination) MarshalJSON() ([]byte, error) {
	switch {
	case d.Retail != nil:
		return json.Marshal(d.Retail)
	case d.Shipping != nil:
		return json.Marshal(d.Shipping)
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

// FulfillmentOption is a fulfillment option within a FulfillmentGroup
// (e.g. Standard Shipping $5, Express $15).
type FulfillmentOption struct {
	ID                      string `json:"id"`
	Title                   string `json:"title"`
	Description             string `json:"description,omitempty"`
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
	// retail locations for pickup.
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
// The spec models this as a bare, untitled object (see DISCREPANCIES.md).
type OrderFulfillment struct {
	Expectations []Expectation      `json:"expectations,omitempty"`
	Events       []FulfillmentEvent `json:"events,omitempty"`
}
