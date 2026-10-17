// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package v1

import (
	"testing"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/surfaces"
)

func TestOntologySeedTargetsAreCatalogueFields(t *testing.T) {
	fields := map[string]bool{}
	for _, f := range DefaultAttributeFields() {
		fields[f.ID] = true
	}

	seen := map[string]bool{}
	for _, r := range DefaultOntologySeed() {
		if !fields[r.TargetField] {
			t.Errorf("seed rule %q targets %q, which is not a catalogue attribute field", r.SourceAttribute, r.TargetField)
		}
		key := r.SourceAttribute + "->" + r.TargetField
		if seen[key] {
			t.Errorf("duplicate seed rule %s", key)
		}
		seen[key] = true
		if r.Confidence <= 0 || r.Confidence > 1 {
			t.Errorf("seed rule %s has confidence %v outside (0,1]", key, r.Confidence)
		}
	}
}

// Every attribute field must be reachable: a field no seed rule can resolve
// would show "not mapped" for every store forever.
func TestEveryAttributeFieldHasASeedRule(t *testing.T) {
	covered := map[string]bool{}
	for _, r := range DefaultOntologySeed() {
		covered[r.TargetField] = true
	}
	for _, f := range DefaultAttributeFields() {
		if !covered[f.ID] {
			t.Errorf("attribute field %q has no ontology seed rule", f.ID)
		}
	}
}

// A field scoped to a surface must name a surface that exists, or it would be
// hidden from every store forever.
func TestSurfaceScopedFieldsNameARegisteredSurface(t *testing.T) {
	known := map[string]bool{}
	for _, id := range surfaces.All {
		known[id] = true
	}
	for _, f := range DefaultAttributeFields() {
		if f.Surface != "" && !known[f.Surface] {
			t.Errorf("field %q is scoped to unknown surface %q", f.ID, f.Surface)
		}
	}
}
