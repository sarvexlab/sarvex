// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package negotiation

import (
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/wire"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/wireintersect"
)

// NegotiationResult holds the active capabilities after intersection.
// This is the value cached and injected into the Echo context.
type NegotiationResult struct {
	Version         string                     `json:"version"`
	Capabilities    map[string][]CapabilityRef `json:"capabilities,omitempty"`
	PaymentHandlers map[string][]HandlerRef    `json:"payment_handlers,omitempty"`
}

type CapabilityRef struct {
	Version string `json:"version"`
}

type HandlerRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

// IntersectCapabilities returns the capabilities active for both parties,
// with orphaned extensions pruned.
//
// Implements the four-step intersection algorithm from the UCP spec:
//  1. Name intersection
//  2. Version selection (highest mutual version; exclude if no mutual version)
//  3. Prune orphaned extensions (where parent is absent from intersection)
//  4. Repeat pruning until stable (handles transitive extension chains)
//
// The algorithm itself lives in wireintersect, generation-agnostic —
// this function just supplies this (v2026-04-08) generation's
// wire.Capability type, which already satisfies wireintersect.Capability
// via CapVersion/ExtendsList.
func IntersectCapabilities(
	business map[string][]wire.Capability,
	platform map[string][]wire.Capability,
) map[string][]wire.Capability {
	return wireintersect.Capabilities(business, platform)
}

// IntersectPaymentHandlers returns payment handlers supported by both parties.
// Intersection is by handler name (map key).
func IntersectPaymentHandlers(
	business map[string][]wire.PaymentHandler,
	platform map[string][]wire.PaymentHandler,
) map[string][]wire.PaymentHandler {
	return wireintersect.PaymentHandlers(business, platform)
}
