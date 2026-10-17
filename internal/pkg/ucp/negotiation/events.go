// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package negotiation

import (
	"encoding/json"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/domain"
	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
)

// CapabilitySets carries the observational requested-vs-declared-vs-active
// capability and payment-handler sets for one negotiation attempt. Names
// are the wire package's map keys — the same reverse-domain capability
// names and payment-handler names the UCP profile itself uses, never free
// text. This exists to close the gap identified in the Agent Behavior
// Insights audit: prior to this, Negotiate computed the platform's
// requested set and the business's declared set only to intersect and
// discard them — nothing recorded either side, only the post-intersection
// result (ActiveCapabilities) or a bare failure reason string. Active
// fields are nil on a failed negotiation (there is no post-intersection
// result to report).
type CapabilitySets struct {
	RequestedCapabilities []string // platform profile's full capability set
	DeclaredCapabilities  []string // business profile's full capability set
	ActiveCapabilities    []string // post-intersection set; nil on failure

	RequestedPaymentHandlers []string // platform profile's full payment-handler set
	DeclaredPaymentHandlers  []string // business profile's full payment-handler set
	ActivePaymentHandlers    []string // post-intersection set; nil on failure

	// Note on payment-handler mismatch: an empty intersection between
	// RequestedPaymentHandlers and DeclaredPaymentHandlers does NOT fail
	// negotiation today (Negotiate assigns the intersection unconditionally)
	// — that behavior is deliberately unchanged here. These fields are
	// observational data for behavior-insights analytics only; comparing
	// Requested/DeclaredPaymentHandlers at read time is how a
	// no_common_payment_handler condition is detected, not a new hard
	// failure mode on the wire.
}

func (s CapabilitySets) asPayload() map[string]any {
	return map[string]any{
		"requested_capabilities":     s.RequestedCapabilities,
		"declared_capabilities":      s.DeclaredCapabilities,
		"active_capabilities":        s.ActiveCapabilities,
		"requested_payment_handlers": s.RequestedPaymentHandlers,
		"declared_payment_handlers":  s.DeclaredPaymentHandlers,
		"active_payment_handlers":    s.ActivePaymentHandlers,
	}
}

// NegotiationSucceededEvent is published when capability negotiation succeeds.
type NegotiationSucceededEvent struct {
	merchantID             string
	storeID                string
	platformProfileURI     string
	sets                   CapabilitySets
	occurredAt             time.Time
	baseDomainEventPayload map[string]any
}

// NewNegotiationSucceededEvent creates a new negotiation succeeded event.
func NewNegotiationSucceededEvent(merchantID, storeID, platformProfileURI string, sets CapabilitySets) *NegotiationSucceededEvent {
	payload := sets.asPayload()
	payload["instance_id"] = storeID
	payload["platform_profile_uri"] = platformProfileURI
	return &NegotiationSucceededEvent{
		merchantID:             merchantID,
		storeID:                storeID,
		platformProfileURI:     platformProfileURI,
		sets:                   sets,
		occurredAt:             time.Now(),
		baseDomainEventPayload: payload,
	}
}

func (e *NegotiationSucceededEvent) EventType() string {
	return domain.EventTypeUCPNegotiationSucceeded
}

func (e *NegotiationSucceededEvent) StoreID() string {
	return e.storeID
}

func (e *NegotiationSucceededEvent) MerchantID() string {
	return e.merchantID
}

func (e *NegotiationSucceededEvent) OccurredAt() time.Time {
	return e.occurredAt
}

// CapabilitySets returns the requested/declared/active capability and
// payment-handler sets recorded for this negotiation.
func (e *NegotiationSucceededEvent) CapabilitySets() CapabilitySets {
	return e.sets
}

func (e *NegotiationSucceededEvent) Payload() []byte {
	b, _ := json.Marshal(e.baseDomainEventPayload)
	return b
}

var _ eventbus.DomainEvent = (*NegotiationSucceededEvent)(nil)

// NegotiationFailedEvent is published when capability negotiation fails.
type NegotiationFailedEvent struct {
	merchantID             string
	storeID                string
	platformProfileURI     string
	reason                 string
	sets                   CapabilitySets
	occurredAt             time.Time
	baseDomainEventPayload map[string]any
}

// NewNegotiationFailedEvent creates a new negotiation failed event. sets is
// the zero value when the failure occurred before the requested/declared
// sets could be computed (see Negotiate's doc comment for exactly which
// failure paths that applies to).
func NewNegotiationFailedEvent(merchantID, storeID, platformProfileURI, reason string, sets CapabilitySets) *NegotiationFailedEvent {
	payload := sets.asPayload()
	payload["store_id"] = storeID
	payload["platform_profile_uri"] = platformProfileURI
	payload["reason"] = reason
	return &NegotiationFailedEvent{
		merchantID:             merchantID,
		storeID:                storeID,
		platformProfileURI:     platformProfileURI,
		reason:                 reason,
		sets:                   sets,
		occurredAt:             time.Now(),
		baseDomainEventPayload: payload,
	}
}

func (e *NegotiationFailedEvent) EventType() string {
	return domain.EventTypeUCPNegotiationFailed
}

func (e *NegotiationFailedEvent) StoreID() string {
	return e.storeID
}

func (e *NegotiationFailedEvent) MerchantID() string {
	return e.merchantID
}

func (e *NegotiationFailedEvent) OccurredAt() time.Time {
	return e.occurredAt
}

// Reason returns the closed failure-reason string ("version_unsupported" or
// "capabilities_incompatible" — see service.go's Negotiate for the
// complete, exhaustive set this codebase actually produces).
func (e *NegotiationFailedEvent) Reason() string {
	return e.reason
}

// CapabilitySets returns the requested/declared sets recorded for this
// failure (ActiveCapabilities/ActivePaymentHandlers are nil — negotiation
// never reached an intersection).
func (e *NegotiationFailedEvent) CapabilitySets() CapabilitySets {
	return e.sets
}

func (e *NegotiationFailedEvent) Payload() []byte {
	b, _ := json.Marshal(e.baseDomainEventPayload)
	return b
}

var _ eventbus.DomainEvent = (*NegotiationFailedEvent)(nil)
