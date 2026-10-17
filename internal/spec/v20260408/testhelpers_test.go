// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package v20260408

import (
	"encoding/json"
	"reflect"
	"testing"
)

// roundTrip unmarshals data into a fresh *T, marshals it back out, and
// re-unmarshals that output into a second *T, asserting the two values
// are deep-equal. This is a stronger check than comparing bytes, since it
// tolerates key reordering while still catching any field that fails to
// round-trip.
func roundTrip[T any](t *testing.T, data []byte) T {
	t.Helper()
	var first T
	if err := json.Unmarshal(data, &first); err != nil {
		t.Fatalf("unmarshal: %v\ndata: %s", err, data)
	}
	out, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var second T
	if err := json.Unmarshal(out, &second); err != nil {
		t.Fatalf("re-unmarshal: %v\ndata: %s", err, out)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("round trip mismatch:\nfirst:  %#v\nsecond: %#v\nmarshaled: %s", first, second, out)
	}
	return first
}

// unmarshalFixture is a thin json.Unmarshal wrapper for tests that need
// to inspect or mutate a decoded value before asserting on it, rather
// than round-tripping it immediately.
func unmarshalFixture(t *testing.T, data []byte, v any) error {
	t.Helper()
	return json.Unmarshal(data, v)
}
