// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package wireintersect implements the UCP capability intersection
// algorithm generically, so that every UCP spec generation's wire
// package can reuse the same logic instead of each re-implementing it.
//
// See https://ucp.dev/latest/specification/overview/#intersection-algorithm
//
// The algorithm only needs two things from a capability type: its own
// per-entity (YYYY-MM-DD) version string, and the names of the parent
// capabilities it extends. Any UCP spec generation's wire.Capability type
// satisfies Capability by exposing those two accessors, so a future
// generation's wire package needs no changes here — only an
// implementation of this interface, exactly like every generation before
// it.
package wireintersect

// Capability is the minimal shape the intersection algorithm needs from a
// generation's capability type.
type Capability interface {
	// CapVersion returns this capability entry's own version string
	// (YYYY-MM-DD) — the per-capability version, not the overall UCP
	// protocol generation.
	CapVersion() string

	// ExtendsList returns the names of the parent capabilities this
	// capability extends. Empty for a root capability.
	ExtendsList() []string
}

// Capabilities intersects two capability sets, implementing the four-step
// algorithm from the UCP spec:
//  1. Include each business capability whose name also appears on the platform.
//  2. For each, select the highest mutually-supported version; exclude if none.
//  3. Prune extensions whose parent(s) are not in the intersection.
//  4. Repeat (3) until stable (handles transitive extension chains).
func Capabilities[C Capability](business, platform map[string][]C) map[string][]C {
	result := map[string][]C{}

	// Steps 1 & 2: name intersection with mutual-version selection.
	for name, bizCaps := range business {
		platCaps, ok := platform[name]
		if !ok {
			continue
		}
		if selected, ok := highestMutual(bizCaps, platCaps); ok {
			result[name] = []C{selected}
		}
	}

	// Steps 3 & 4: iteratively prune orphaned extensions. An extension is
	// orphaned when any parent capability it extends is absent from the
	// result.
	for {
		removed := false
		for name, caps := range result {
			for _, c := range caps {
				if orphaned(c, result) {
					delete(result, name)
					removed = true
					break
				}
			}
		}
		if !removed {
			break
		}
	}

	return result
}

// orphaned reports whether any parent capability c extends is missing
// from the active set.
func orphaned[C Capability](c C, active map[string][]C) bool {
	for _, parent := range c.ExtendsList() {
		if _, ok := active[parent]; !ok {
			return true
		}
	}
	return false
}

// highestMutual returns the single capability from business whose version
// is also present in platform and is the highest (latest date string).
// Returns ok=false if there is no mutual version.
func highestMutual[C Capability](business, platform []C) (best C, ok bool) {
	platVersions := make(map[string]struct{}, len(platform))
	for _, c := range platform {
		platVersions[c.CapVersion()] = struct{}{}
	}

	for _, c := range business {
		if _, present := platVersions[c.CapVersion()]; !present {
			continue
		}
		// Lexical comparison is sufficient for YYYY-MM-DD version strings.
		if !ok || c.CapVersion() > best.CapVersion() {
			best, ok = c, true
		}
	}
	return best, ok
}

// PaymentHandlers intersects two payment-handler sets by name only — the
// instrument intersection is the business's authoritative response, per
// the payment handler spec.
func PaymentHandlers[H any](business, platform map[string][]H) map[string][]H {
	result := map[string][]H{}
	for name, handlers := range business {
		if _, ok := platform[name]; ok {
			result[name] = handlers
		}
	}
	return result
}
