// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package data

import (
	"reflect"
	"testing"

	v1 "github.com/sarvexlab/sarvex/internal/pkg/ucp/catalogue/v1"
)

// A rubric stored in Mongo and read back must score identically to the one in
// code: dimension weights and surface scoping both have to survive the trip.
func TestRubricDocRoundTrip(t *testing.T) {
	in := v1.DefaultScoringRubric()
	in.Rules = append(in.Rules, v1.ScoringRule{
		ID: "rule_brand_missing", Dimension: "completeness", Field: "brand",
		Condition: "missing", Weight: 10, Surface: "example_surface",
	})

	out := convertDocToRubric(convertRubricToDoc(in))

	if !reflect.DeepEqual(in.DimensionWeights, out.DimensionWeights) || len(out.DimensionWeights) == 0 {
		t.Errorf("dimension weights lost: in=%v out=%v", in.DimensionWeights, out.DimensionWeights)
	}
	if !reflect.DeepEqual(in.Rules, out.Rules) {
		t.Errorf("rules changed in the round trip:\n in=%+v\nout=%+v", in.Rules, out.Rules)
	}
}
