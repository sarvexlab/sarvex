// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package negotiation

import (
	"github.com/labstack/echo/v4"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

const (
	// StoreProfileKey is set by the upstream tenant middleware.
	StoreProfileKey = "_store_profile_instance"

	// NegotiationResultKey is set by this middleware after negotiation.
	NegotiationResultKey = "_negotiation_result"

	// PlatformProfileKey is set by this middleware after fetching the platform profile.
	PlatformProfileKey = "_platform_profile"

	// OutcomeKey stores the coarse negotiation outcome ("succeeded",
	// "failed") on every request carrying a UCP-Agent header, success or
	// failure — unlike NegotiationResultKey (only set on success), this
	// lets an outer telemetry middleware observe negotiation failures even
	// though WithNegotiation writes the error response itself and returns
	// nil rather than propagating an error. Absent entirely on requests
	// with no UCP-Agent header ("not_applicable" to the caller).
	OutcomeKey = "_negotiation_outcome"
)

// SetOutcome records the coarse negotiation outcome for this request.
func SetOutcome(c echo.Context, outcome string) {
	c.Set(OutcomeKey, outcome)
}

// GetOutcome retrieves the coarse negotiation outcome, if negotiation ran
// for this request at all (ok is false when there was no UCP-Agent header).
func GetOutcome(c echo.Context) (string, bool) {
	v, ok := c.Get(OutcomeKey).(string)
	return v, ok
}

// GetStoreProfile retrieves the StoreProvider set by the tenant middleware.
func GetStoreProfile(c echo.Context) (ucp.StoreProvider, bool) {
	v := c.Get(StoreProfileKey)
	if v == nil {
		return nil, false
	}
	inst, ok := v.(ucp.StoreProvider)
	return inst, ok
}

// SetStoreProfile retrieves the StoreProvider set by the tenant middleware.
func SetStoreProfile(c echo.Context, provider ucp.StoreProvider) {
	c.Set(StoreProfileKey, provider)
}

// SetNegotiationResult stores the negotiation result in the Echo context.
func SetNegotiationResult(c echo.Context, result *NegotiationResult) {
	c.Set(NegotiationResultKey, result)
}

// GetNegotiationResult retrieves the negotiation result from the Echo context.
func GetNegotiationResult(c echo.Context) (*NegotiationResult, bool) {
	v := c.Get(NegotiationResultKey)
	if v == nil {
		return nil, false
	}
	result, ok := v.(*NegotiationResult)
	return result, ok
}

// GetPlatformProfile retrieves the fetched platform profile from the Echo context.
func GetPlatformProfile(c echo.Context) (*wire.Profile, bool) {
	v := c.Get(PlatformProfileKey)
	if v == nil {
		return nil, false
	}
	profile, ok := v.(*wire.Profile)
	return profile, ok
}
