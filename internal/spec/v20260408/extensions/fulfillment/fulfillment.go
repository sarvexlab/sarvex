// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package fulfillment implements the UCP Fulfillment extension
// (dev.ucp.shopping.fulfillment): hierarchical fulfillment (methods,
// destinations, groups) on Checkout, plus the capability descriptor
// itself.
//
// The extension schema's FulfillmentOption/Group/Method/AvailableMethod/
// Fulfillment $defs are bare $refs to the base type schemas — the
// extension contributes no new fields for them — so this package aliases
// the base package's types rather than redeclaring them.
package fulfillment

import (
	"encoding/json"
	"fmt"

	v20260408 "github.com/sarvexlab/sarvex/internal/spec/v20260408"
)

// FulfillmentOption aliases the base Fulfillment Option type; the
// extension schema contributes no additional fields.
type FulfillmentOption = v20260408.FulfillmentOption

// FulfillmentGroup aliases the base Fulfillment Group type; the extension
// schema contributes no additional fields.
type FulfillmentGroup = v20260408.FulfillmentGroup

// FulfillmentMethod aliases the base Fulfillment Method type; the
// extension schema contributes no additional fields.
type FulfillmentMethod = v20260408.FulfillmentMethod

// FulfillmentAvailableMethod aliases the base Fulfillment Available
// Method type; the extension schema contributes no additional fields.
type FulfillmentAvailableMethod = v20260408.FulfillmentAvailableMethod

// Fulfillment aliases the base Fulfillment container type; the extension
// schema contributes no additional fields.
type Fulfillment = v20260408.Fulfillment

// CheckoutWithFulfillment is Checkout extended with hierarchical
// fulfillment. Composes the base Checkout type rather than duplicating
// its fields.
type CheckoutWithFulfillment struct {
	v20260408.Checkout
	Fulfillment *Fulfillment `json:"fulfillment,omitempty"`
}

// PlatformCapability is the dev.ucp.shopping.fulfillment capability
// descriptor for platform-level discovery. Composes the base Capability
// type rather than duplicating its fields; Config shadows
// Capability.Entity.Config with the typed platform fulfillment config.
type PlatformCapability struct {
	v20260408.Capability
	Config *v20260408.PlatformFulfillmentConfig `json:"config,omitempty"`
}

// UnmarshalJSON populates the embedded Capability and the typed Config.
func (c *PlatformCapability) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &c.Capability); err != nil {
		return err
	}
	var cfg struct {
		Config *v20260408.PlatformFulfillmentConfig `json:"config"`
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
	m, err := c.Capability.FieldMap()
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
	v20260408.Capability
	Config *v20260408.BusinessFulfillmentConfig `json:"config,omitempty"`
}

// UnmarshalJSON populates the embedded Capability and the typed Config.
func (c *BusinessCapability) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &c.Capability); err != nil {
		return err
	}
	var cfg struct {
		Config *v20260408.BusinessFulfillmentConfig `json:"config"`
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
	m, err := c.Capability.FieldMap()
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
