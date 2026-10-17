// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package checkoutescalation

import "testing"

// TestRedact_StripsCredentialAndPIIShapedFields is A3: a synthetic event
// containing credential-shaped and PII-shaped fields must produce a
// redacted payload with those fields absent, at every depth.
func TestRedact_StripsCredentialAndPIIShapedFields(t *testing.T) {
	payload := map[string]any{
		"checkout_session_id": "sess-1",
		"store_id":            "store-1",
		"payment": map[string]any{
			"handler_id":  "stripe_v1",
			"credential":  "tok_live_super_secret",
			"card_number": "4242424242424242",
			"cvv":         "123",
		},
		"buyer": map[string]any{
			"email":   "buyer@example.com",
			"phone":   "+15555550123",
			"address": "123 Main St",
			"name":    "Jane Doe",
		},
		"line_items": []any{
			map[string]any{"sku": "SKU-1", "quantity": 2},
			map[string]any{"sku": "SKU-2", "quantity": 1, "buyer_email": "leak@example.com"},
		},
		"access_token": "eyJhbGciOi...",
	}

	redacted := Redact(payload)

	// Top-level safe fields survive.
	if redacted["checkout_session_id"] != "sess-1" {
		t.Errorf("expected checkout_session_id to survive redaction")
	}
	if redacted["store_id"] != "store-1" {
		t.Errorf("expected store_id to survive redaction")
	}
	if _, present := redacted["access_token"]; present {
		t.Errorf("access_token must not survive redaction")
	}

	payment, ok := redacted["payment"].(map[string]any)
	if !ok {
		t.Fatalf("expected payment sub-object to survive (with fields stripped), got %T", redacted["payment"])
	}
	if payment["handler_id"] != "stripe_v1" {
		t.Errorf("expected handler_id (opaque reference, not a credential) to survive")
	}
	for _, field := range []string{"credential", "card_number", "cvv"} {
		if _, present := payment[field]; present {
			t.Errorf("payment.%s must not survive redaction", field)
		}
	}

	buyer, ok := redacted["buyer"].(map[string]any)
	if !ok {
		t.Fatalf("expected buyer sub-object to survive (with fields stripped), got %T", redacted["buyer"])
	}
	for _, field := range []string{"email", "phone", "address"} {
		if _, present := buyer[field]; present {
			t.Errorf("buyer.%s must not survive redaction", field)
		}
	}
	if buyer["name"] != "Jane Doe" {
		t.Errorf("expected buyer.name (not in the sensitive pattern list) to survive")
	}

	items, ok := redacted["line_items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("expected line_items array to survive with 2 entries, got %v", redacted["line_items"])
	}
	item2, ok := items[1].(map[string]any)
	if !ok {
		t.Fatalf("expected line_items[1] to be a map, got %T", items[1])
	}
	if _, present := item2["buyer_email"]; present {
		t.Errorf("line_items[1].buyer_email must not survive redaction (nested-in-array field)")
	}
	if item2["sku"] != "SKU-2" {
		t.Errorf("expected line_items[1].sku to survive redaction")
	}
}

func TestRedact_NilPayload(t *testing.T) {
	if Redact(nil) != nil {
		t.Errorf("expected Redact(nil) to return nil")
	}
}

func TestRedact_DoesNotMutateInput(t *testing.T) {
	payload := map[string]any{"credential": "secret", "store_id": "store-1"}
	_ = Redact(payload)
	if _, present := payload["credential"]; !present {
		t.Errorf("Redact must not mutate its input map")
	}
}
