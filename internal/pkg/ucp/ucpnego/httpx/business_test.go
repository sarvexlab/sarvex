// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package httpx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

// TestFailureOutcome_DiscoveryBody verifies the JSON shape an adapter
// will write for discovery/version errors.
func TestFailureOutcome_DiscoveryBody(t *testing.T) {
	f := &FailureOutcome{
		Code:        CodeInvalidProfileURL,
		HTTPStatus:  http.StatusBadRequest,
		Content:     "malformed UCP-Agent",
		ContinueURL: "https://merchant/cart",
	}

	body := f.DiscoveryBody()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	mustContain(t, got, `"code":"invalid_profile_url"`)
	mustContain(t, got, `"content":"malformed UCP-Agent"`)
	mustContain(t, got, `"continue_url":"https://merchant/cart"`)
}

// TestFailureOutcome_NegotiationBody verifies the JSON shape for the
// 200-OK capabilities_incompatible response.
func TestFailureOutcome_NegotiationBody(t *testing.T) {
	f := &FailureOutcome{
		Code:            CodeCapabilitiesIncompatible,
		HTTPStatus:      http.StatusOK,
		Content:         "no overlap",
		ContinueURL:     "https://merchant/cart",
		ProtocolVersion: wire.Version("2026-04-08"),
	}
	if !f.IsNegotiationFailure() {
		t.Fatal("expected IsNegotiationFailure to be true")
	}

	body := f.NegotiationBody()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	mustContain(t, got, `"version":"2026-04-08"`)
	mustContain(t, got, `"status":"error"`)
	mustContain(t, got, `"code":"capabilities_incompatible"`)
	mustContain(t, got, `"severity":"unrecoverable"`)
	mustContain(t, got, `"continue_url":"https://merchant/cart"`)
}

// TestNegotiate_Direct verifies the transport-neutral entry point Adapters
// (Echo, Chi, Fiber) call. Success case returns a non-empty Result and
// nil failure.
func TestNegotiate_Direct_Success(t *testing.T) {
	// Build a profile source that returns a matching platform profile.
	source := stubSource{
		profile: &wire.Profile{
			UCP: wire.UCP{
				Version: "2026-04-08",
				Capabilities: map[string][]wire.Capability{
					"dev.ucp.shopping.checkout": {{
						Version: "2026-04-08",
						Spec:    "https://ucp.dev/spec/checkout",
						Schema:  "https://ucp.dev/schemas/checkout.json",
					}},
				},
			},
		},
	}
	own := &wire.Profile{
		UCP: wire.UCP{
			Version: "2026-04-08",
			Capabilities: map[string][]wire.Capability{
				"dev.ucp.shopping.checkout": {{
					Version: "2026-04-08",
					Spec:    "https://ucp.dev/spec/checkout",
					Schema:  "https://ucp.dev/schemas/checkout.json",
				}},
			},
		},
	}

	neg, err := NewBusinessNegotiator(own, source)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("UCP-Agent", `profile="https://platform.example/.well-known/ucp"`)

	result, failure := neg.Negotiate(req)
	if failure != nil {
		t.Fatalf("unexpected failure: %+v", failure)
	}
	if result.IsEmpty() {
		t.Fatal("expected non-empty result")
	}
}

// TestNegotiate_Direct_MissingHeader verifies the failure path used by
// every adapter.
func TestNegotiate_Direct_MissingHeader(t *testing.T) {
	own := &wire.Profile{
		UCP: wire.UCP{
			Version: "2026-04-08",
			Capabilities: map[string][]wire.Capability{
				"dev.ucp.shopping.checkout": {{Version: "2026-04-08", Spec: "https://ucp.dev/s", Schema: "https://ucp.dev/sc"}},
			},
		},
	}
	neg, _ := NewBusinessNegotiator(own, stubSource{})
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	// No UCP-Agent header.

	_, failure := neg.Negotiate(req)
	if failure == nil {
		t.Fatal("expected failure for missing UCP-Agent")
	}
	if failure.Code != CodeInvalidProfileURL {
		t.Errorf("got code %q, want %q", failure.Code, CodeInvalidProfileURL)
	}
	if failure.IsNegotiationFailure() {
		t.Error("should be a discovery failure, not a negotiation failure")
	}
}

// stubSource is a ProfileSource that returns a fixed profile.
type stubSource struct {
	profile *wire.Profile
	err     error
}

func (s stubSource) Get(_ context.Context, _ string) (*wire.Profile, error) {
	return s.profile, s.err
}

func mustContain(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("body missing %q\ngot: %s", want, got)
	}
}
