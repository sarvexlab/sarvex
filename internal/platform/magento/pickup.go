// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package magento

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/commerce/model"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/connectors/base"
)

// Pickup detection reads Magento's Multi-Source Inventory (MSI) REST API — no
// extension is needed beyond the in-store-pickup module Magento ships with MSI.
// Pickup is a fulfillment method type (and a business-location destination)
// inside the fulfillment extension, so the result decides whether a checkout's
// fulfillment object offers it; it is not advertised in the profile.
//
// An inventory source flagged is_pickup_location_active is a place buyers can
// collect an order; its address is the destination. When more than one source
// is active the first (by source code) is offered — a checkout offers a single
// pickup location today. A store without MSI answers 404 for the sources
// endpoint, which simply means no pickup.

const (
	// pickupCarrierCode / pickupMethodCode are Magento's in-store-pickup
	// carrier and method; a pickup order's shipping method is their join.
	pickupCarrierCode = "instore"
	pickupMethodCode  = "pickup"
	pickupOptionID    = pickupCarrierCode + "_" + pickupMethodCode
	pickupTitle       = "In-store pickup"

	// pickupCacheTTL bounds how long checkout reuses a detection result.
	pickupCacheTTL = 5 * time.Minute
	// pickupFailureTTL is how long a failed detection is remembered, so an
	// unreachable endpoint costs one attempt a minute, not one per checkout.
	pickupFailureTTL = time.Minute
)

// pickupFromSources derives PickupInfo from the store's inventory sources.
// Pure, so it is testable without a store.
func pickupFromSources(sources []mgSource) *base.PickupInfo {
	var pick *mgSource
	for i := range sources {
		s := &sources[i]
		if !s.Enabled || s.ExtensionAttributes == nil || !s.ExtensionAttributes.IsPickupLocationActive {
			continue
		}
		if pick == nil || s.SourceCode < pick.SourceCode {
			pick = s
		}
	}
	if pick == nil {
		return &base.PickupInfo{}
	}
	name := pick.Name
	if pick.ExtensionAttributes.FrontendName != "" {
		name = pick.ExtensionAttributes.FrontendName
	}
	return &base.PickupInfo{
		Enabled: true,
		Location: &model.RetailLocation{
			ID:   pick.SourceCode,
			Name: name,
			Address: &model.PostalAddress{
				StreetAddress:   pick.Street,
				AddressLocality: pick.City,
				AddressRegion:   pick.Region,
				PostalCode:      pick.Postcode,
				AddressCountry:  pick.Country,
				PhoneNumber:     pick.Phone,
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
// endpoint must not break a checkout that does not need pickup.
func (c *Connector) checkoutPickup(ctx context.Context) *base.PickupInfo {
	c.pickup.mu.Lock()
	defer c.pickup.mu.Unlock()
	if c.pickup.info != nil && time.Now().Before(c.pickup.until) {
		return c.pickup.info
	}

	info, err := c.livePickup(ctx)
	switch {
	case err == nil:
		c.pickup.info, c.pickup.until = info, time.Now().Add(pickupCacheTTL)
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrForbidden):
		// No MSI (or no access to it): the store has no pickup locations.
		c.pickup.info, c.pickup.until = &base.PickupInfo{}, time.Now().Add(pickupCacheTTL)
	default:
		c.logger.Warn("pickup detection failed at checkout; not offering pickup", "error", err)
		c.pickup.info, c.pickup.until = &base.PickupInfo{}, time.Now().Add(pickupFailureTTL)
	}
	return c.pickup.info
}

func (c *Connector) livePickup(ctx context.Context) (*base.PickupInfo, error) {
	var list mgSourceList
	if err := c.client.GET(ctx, "/inventory/sources?searchCriteria[page_size]=100", &list); err != nil {
		return nil, err
	}
	return pickupFromSources(list.Items), nil
}

// applyFulfillmentSelection records the buyer's fulfillment choice on the
// quote and returns the shipping state the response should show. A pickup
// choice becomes the in-store-pickup method addressed to the pickup location;
// a shipping destination is estimated for rates and, when a rate is chosen,
// recorded with it.
func (c *Connector) applyFulfillmentSelection(ctx context.Context, cartID string, f *base.FulfillmentRequest) (*fulfillmentChoice, error) {
	if f == nil {
		return nil, nil
	}

	if f.MethodType == string(model.FulfillmentMethodTypePickup) {
		info := c.checkoutPickup(ctx)
		if !info.Enabled || info.Location == nil {
			return nil, &base.CheckoutRejectedError{Code: "pickup_unavailable", Detail: "this store does not offer pickup"}
		}
		if f.SelectedDestinationID != "" && f.SelectedDestinationID != info.Location.ID {
			return nil, &base.CheckoutRejectedError{Code: "invalid_destination", Detail: fmt.Sprintf("unknown pickup location %q", f.SelectedDestinationID)}
		}
		addr := ucpAddressToMgAddress(info.Location.Address)
		if addr == nil {
			addr = &mgAddress{}
		}
		addr.ExtensionAttributes = &mgAddressExtension{PickupLocationCode: info.Location.ID}
		if err := c.setShippingInformation(ctx, cartID, addr, pickupCarrierCode, pickupMethodCode); err != nil {
			return nil, err
		}
		return &fulfillmentChoice{}, nil
	}

	if f.SelectedDestination != nil {
		addr := ucpAddressToMgAddress(f.SelectedDestination)
		if f.SelectedMethodID != "" {
			carrier, method := parseShippingMethod(f.SelectedMethodID)
			if err := c.setShippingInformation(ctx, cartID, addr, carrier, method); err != nil {
				return nil, err
			}
		}
		return &fulfillmentChoice{Destination: addr}, nil
	}
	return nil, nil
}

// fulfillmentChoice is a destination the buyer supplied in this request that
// the quote may not have recorded yet (a destination is only recorded together
// with a chosen rate), so the response can still estimate rates for it.
type fulfillmentChoice struct {
	Destination *mgAddress
}
