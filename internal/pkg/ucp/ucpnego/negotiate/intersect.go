// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package negotiate implements the UCP capability intersection algorithm.
//
// The algorithm is pure: given two profiles it returns the set of
// capabilities both parties support at mutually compatible versions,
// with extension dependencies satisfied. No I/O, no logging, no errors —
// an empty result is a valid (negotiation-failure) outcome, signaled
// separately by the caller.
//
// See https://ucp.dev/latest/specification/overview/#intersection-algorithm
package negotiate

import (
	"sort"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/wire"
)

// Result is the outcome of intersecting two profiles.
type Result struct {
	// ProtocolVersion is the negotiated protocol version. It is the older
	// of the two parties' declared versions, since UCP guarantees
	// backwards compatibility within a major series.
	ProtocolVersion wire.Version

	// Capabilities maps capability name to the selected (single) version
	// of that capability that both parties support. The Capability value
	// carries the business's spec/schema/extends so downstream schema
	// resolution has what it needs.
	Capabilities map[string]wire.Capability

	// PaymentHandlers contains handlers offered by the business AND
	// supported by the platform (matched by handler name only — the
	// instrument intersection is the business's authoritative response
	// per the payment handler spec).
	PaymentHandlers map[string]wire.PaymentHandler
}

// IsEmpty reports whether negotiation produced no usable capabilities.
// Callers translate this to capabilities_incompatible.
func (r Result) IsEmpty() bool { return len(r.Capabilities) == 0 }

// Names returns the negotiated capability names in deterministic order.
// Useful for logging and for building the response.
func (r Result) Names() []string {
	out := make([]string, 0, len(r.Capabilities))
	for name := range r.Capabilities {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Intersect runs the four-step algorithm on the two profiles. The
// business profile is treated as the source of truth (server-selects
// architecture): the spec/schema URLs in the result come from the
// business's declaration.
//
// Steps (from the spec):
//  1. Include each business capability whose name also appears on the platform.
//  2. For each, select the highest mutual version; if none, exclude.
//  3. Prune extensions whose parent(s) are not in the intersection.
//  4. Repeat (3) until stable (transitive chains).
func Intersect(business, platform *wire.Profile) Result {
	res := Result{
		ProtocolVersion: olderVersion(business.UCP.Version, platform.UCP.Version),
		Capabilities:    make(map[string]wire.Capability),
		PaymentHandlers: make(map[string]wire.PaymentHandler),
	}

	platformCaps := index(platform.UCP.Capabilities)

	// Steps 1 & 2: name + version intersection.
	for name, businessVersions := range business.UCP.Capabilities {
		platformVersions, ok := platformCaps[name]
		if !ok {
			continue
		}
		chosen, ok := pickHighestMutual(businessVersions, platformVersions)
		if !ok {
			continue
		}
		res.Capabilities[name] = chosen
	}

	// Steps 3 & 4: prune orphaned extensions until stable.
	for {
		removed := pruneOrphans(res.Capabilities)
		if removed == 0 {
			break
		}
	}

	// Payment handlers: business-side declarations, gated by platform support.
	platformHandlers := platform.UCP.PaymentHandlers
	for name, entries := range business.UCP.PaymentHandlers {
		if _, supported := platformHandlers[name]; !supported {
			continue
		}
		if len(entries) == 0 {
			continue
		}
		// Business declaration is authoritative; pick the first entry,
		// matching the spec's expectation that the business resolves
		// available_instruments in the response.
		res.PaymentHandlers[name] = entries[0]
	}

	return res
}

// pruneOrphans removes extensions whose parents are absent. For
// multi-parent extensions, at least one parent must remain.
// Returns the number of capabilities removed in this pass.
func pruneOrphans(caps map[string]wire.Capability) int {
	var removed int
	for name, c := range caps {
		parents := c.ExtendsList()
		if len(parents) == 0 {
			continue // root capability, keep
		}
		if !anyParentPresent(parents, caps) {
			delete(caps, name)
			removed++
		}
	}
	return removed
}

func anyParentPresent(parents []string, caps map[string]wire.Capability) bool {
	for _, p := range parents {
		if _, ok := caps[p]; ok {
			return true
		}
	}
	return false
}

// pickHighestMutual returns the highest version present in both lists.
// The returned capability is the business's declaration at that version
// (business is the server, owns the spec/schema URLs).
func pickHighestMutual(business, platform []wire.Capability) (wire.Capability, bool) {
	platformVersions := make(map[wire.Version]struct{}, len(platform))
	for _, c := range platform {
		platformVersions[c.Version] = struct{}{}
	}

	// Sort business entries by version descending, return first match.
	sorted := make([]wire.Capability, len(business))
	copy(sorted, business)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Version.Compare(sorted[j].Version) > 0
	})

	for _, c := range sorted {
		if _, ok := platformVersions[c.Version]; ok {
			return c, true
		}
	}
	return wire.Capability{}, false
}

func index(m map[string][]wire.Capability) map[string][]wire.Capability {
	if m == nil {
		return map[string][]wire.Capability{}
	}
	return m
}

func olderVersion(a, b wire.Version) wire.Version {
	if a.Compare(b) < 0 {
		return a
	}
	return b
}

// FilterResponseCapabilities returns the subset of negotiated capabilities
// relevant to a given operation type, per the Response Capability
// Selection rules in the spec. The Operation enumerates the categories;
// extensions are included when any parent matches.
//
// See https://ucp.dev/latest/specification/overview/#response-capability-selection
func FilterResponseCapabilities(r Result, op Operation) map[string]wire.Capability {
	roots := op.RootCapabilities()
	rootSet := make(map[string]struct{}, len(roots))
	for _, r := range roots {
		rootSet[r] = struct{}{}
	}

	out := make(map[string]wire.Capability)
	for name, c := range r.Capabilities {
		if _, isRoot := rootSet[name]; isRoot {
			out[name] = c
			continue
		}
		// Extension: include if any parent is in the relevant root set.
		for _, parent := range c.ExtendsList() {
			if _, ok := rootSet[parent]; ok {
				out[name] = c
				break
			}
		}
	}
	return out
}

// Operation is the response operation type, used to filter the
// negotiated set down to what's relevant for the response.
type Operation int

const (
	OpUnknown Operation = iota
	OpCheckout
	OpCart
	OpOrder
	OpCatalog
)

// RootCapabilities returns the root capability names relevant to op.
func (o Operation) RootCapabilities() []string {
	switch o {
	case OpCheckout:
		return []string{"dev.ucp.shopping.checkout"}
	case OpCart:
		return []string{"dev.ucp.shopping.cart"}
	case OpOrder:
		return []string{"dev.ucp.shopping.order"}
	case OpCatalog:
		return []string{"dev.ucp.shopping.catalog"}
	default:
		return nil
	}
}
