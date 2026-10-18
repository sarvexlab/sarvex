// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package woocommerce

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

// Pickup detection reads WooCommerce's stock REST API — no plugin is needed on
// the merchant's side. Pickup is a fulfillment method type (and a business
// location destination) inside the fulfillment extension, so the result decides
// whether a checkout's fulfillment object offers it; it is not advertised in
// the profile:
//
//   - /shipping/zones and /shipping/zones/{id}/methods list the shipping
//     methods each zone has, with an `enabled` flag; an enabled local-pickup
//     method means the store offers pickup.
//   - /settings/general carries the store's own address, which is where
//     WooCommerce's Local Pickup sends buyers to collect.
//
// The location's address is the store's; the store name is not in the general
// settings (it is a WordPress option), so the location is named generically.

const (
	// pickupMethodID is the shipping method id an order carries when its buyer
	// chose pickup (WooCommerce's own Local Pickup method id).
	pickupMethodID = "local_pickup"
	// pickupLocationID / pickupOptionID identify the single pickup location and
	// its option in the checkout's fulfillment object.
	pickupLocationID = "pickup_store"
	pickupOptionID   = pickupMethodID
	pickupTitle      = "Local pickup"

	// pickupCacheTTL bounds how long checkout reuses a detection result; the
	// sync plane's own probe is what keeps the profile current.
	pickupCacheTTL = 5 * time.Minute
	// pickupFailureTTL is how long a failed detection is remembered, so an
	// unreachable settings endpoint costs one attempt a minute, not one per
	// checkout call.
	pickupFailureTTL = time.Minute
)

// pickupMethodIDs are the WooCommerce shipping method ids that mean pickup:
// the classic Local Pickup method and the block-based Pickup Location method.
var pickupMethodIDs = map[string]bool{"local_pickup": true, "pickup_location": true}

type wcShippingZone struct {
	ID int `json:"id"`
}

type wcZoneMethod struct {
	MethodID string `json:"method_id"`
	Enabled  bool   `json:"enabled"`
}

type wcSetting struct {
	ID    string `json:"id"`
	Value any    `json:"value"`
}

// isPickupMethod reports whether a shipping method id (which may carry a
// ":instance" suffix, as on an order's shipping line) is a pickup method.
func isPickupMethod(methodID string) bool {
	base, _, _ := strings.Cut(methodID, ":")
	return pickupMethodIDs[base]
}

// pickupFromWC derives PickupInfo from the zones' methods and the general
// settings. Pure, so it is testable without a store.
func pickupFromWC(zoneMethods [][]wcZoneMethod, settings []wcSetting) *base.PickupInfo {
	enabled := false
	for _, methods := range zoneMethods {
		for _, m := range methods {
			if m.Enabled && isPickupMethod(m.MethodID) {
				enabled = true
			}
		}
	}
	if !enabled {
		return &base.PickupInfo{}
	}

	get := func(id string) string {
		for _, s := range settings {
			if s.ID == id {
				v, _ := s.Value.(string)
				return v
			}
		}
		return ""
	}
	// woocommerce_default_country is "CC" or "CC:REGION".
	country, region, _ := strings.Cut(get("woocommerce_default_country"), ":")
	return &base.PickupInfo{
		Enabled: true,
		Location: &model.RetailLocation{
			ID:   pickupLocationID,
			Name: pickupTitle,
			Address: &model.PostalAddress{
				StreetAddress:   get("woocommerce_store_address"),
				ExtendedAddress: get("woocommerce_store_address_2"),
				AddressLocality: get("woocommerce_store_city"),
				AddressRegion:   region,
				PostalCode:      get("woocommerce_store_postcode"),
				AddressCountry:  country,
			},
		},
	}
}

// pickupCache memoises the checkout-time detection result.
type pickupCache struct {
	mu    sync.Mutex
	info  *base.PickupInfo
	until time.Time
}

// checkoutPickup returns the store's pickup info for a checkout response, from
// a short-lived cache or a live read (checkout is where live merchant calls
// are permitted). A failed read is treated as "no pickup": an unreachable
// settings endpoint must not break a checkout that does not need pickup.
func (c *Connector) checkoutPickup(ctx context.Context) *base.PickupInfo {
	c.pickup.mu.Lock()
	defer c.pickup.mu.Unlock()
	if c.pickup.info != nil && time.Now().Before(c.pickup.until) {
		return c.pickup.info
	}

	info, err := c.livePickup(ctx)
	if err != nil {
		c.logger.Warn("pickup detection failed at checkout; not offering pickup", "error", err)
		c.pickup.info, c.pickup.until = &base.PickupInfo{}, time.Now().Add(pickupFailureTTL)
		return c.pickup.info
	}
	c.pickup.info, c.pickup.until = info, time.Now().Add(pickupCacheTTL)
	return info
}

func (c *Connector) livePickup(ctx context.Context) (*base.PickupInfo, error) {
	var zones []wcShippingZone
	if err := c.client.GET(ctx, "/shipping/zones", &zones); err != nil {
		return nil, err
	}
	ids := []int{0}
	for _, z := range zones {
		ids = append(ids, z.ID)
	}
	zoneMethods := make([][]wcZoneMethod, 0, len(ids))
	for _, id := range ids {
		var methods []wcZoneMethod
		if err := c.client.GET(ctx, fmt.Sprintf("/shipping/zones/%d/methods", id), &methods); err != nil {
			return nil, err
		}
		zoneMethods = append(zoneMethods, methods)
	}
	var settings []wcSetting
	if err := c.client.GET(ctx, "/settings/general", &settings); err != nil {
		return nil, err
	}
	return pickupFromWC(zoneMethods, settings), nil
}

// applyFulfillmentSelection records the buyer's fulfillment choice on an order
// write. A pickup choice becomes a local-pickup shipping line addressed to the
// pickup location; anything else keeps the shipping behaviour (destination
// address plus the chosen shipping rate).
func (c *Connector) applyFulfillmentSelection(ctx context.Context, payload map[string]any, f *base.FulfillmentRequest) error {
	if f == nil {
		return nil
	}

	if f.MethodType == string(model.FulfillmentMethodTypePickup) {
		info := c.checkoutPickup(ctx)
		if !info.Enabled || info.Location == nil {
			return &base.CheckoutRejectedError{Code: "pickup_unavailable", Detail: "this store does not offer pickup"}
		}
		if f.SelectedDestinationID != "" && f.SelectedDestinationID != info.Location.ID {
			return &base.CheckoutRejectedError{Code: "invalid_destination", Detail: fmt.Sprintf("unknown pickup location %q", f.SelectedDestinationID)}
		}
		if info.Location.Address != nil {
			payload["shipping"] = ucpAddressToWCAddress(info.Location.Address)
		}
		payload["shipping_lines"] = []map[string]any{{
			"method_id":    pickupMethodID,
			"method_title": pickupTitle,
		}}
		return nil
	}

	if f.SelectedDestination != nil {
		payload["shipping"] = ucpAddressToWCAddress(f.SelectedDestination)
		if f.SelectedMethodID != "" {
			payload["shipping_lines"] = []map[string]any{{
				"method_id": f.SelectedMethodID,
			}}
		}
	}
	return nil
}

// applyFulfillment builds the checkout's fulfillment object: the shipping
// method, plus a pickup method when the store offers pickup.
func (c *Connector) applyFulfillment(ctx context.Context, resp *base.CheckoutResponse, order *wcOrder) {
	m := mapper{}
	m.ApplyFulfillmentExtension(resp, order)
	if len(resp.LineItems) == 0 {
		return
	}
	m.ApplyPickup(resp, order, c.checkoutPickup(ctx))
}
