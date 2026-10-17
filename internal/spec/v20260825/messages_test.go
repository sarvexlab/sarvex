// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package v20260825

import (
	"encoding/json"
	"testing"
)

func TestMessageErrorRoundTrip(t *testing.T) {
	data := []byte(`{
		"type": "error",
		"code": "out_of_stock",
		"path": "$.line_items[0]",
		"content_type": "plain",
		"content": "This item is out of stock.",
		"severity": "unrecoverable"
	}`)
	msg := roundTrip[Message](t, data)
	e, ok := msg.AsError()
	if !ok {
		t.Fatal("AsError() ok = false, want true")
	}
	if e.Code != ErrorCodeOutOfStock {
		t.Errorf("Code = %q, want %q", e.Code, ErrorCodeOutOfStock)
	}
	if e.Severity != MessageSeverityUnrecoverable {
		t.Errorf("Severity = %q, want %q", e.Severity, MessageSeverityUnrecoverable)
	}
	if _, ok := msg.AsWarning(); ok {
		t.Error("AsWarning() ok = true, want false")
	}
	if _, ok := msg.AsInfo(); ok {
		t.Error("AsInfo() ok = true, want false")
	}
}

func TestMessageWarningRoundTrip(t *testing.T) {
	data := []byte(`{
		"type": "warning",
		"code": "payment_term_changed",
		"content": "Payment terms have changed.",
		"presentation": "disclosure"
	}`)
	msg := roundTrip[Message](t, data)
	w, ok := msg.AsWarning()
	if !ok {
		t.Fatal("AsWarning() ok = false, want true")
	}
	if w.Presentation != WarningPresentationDisclosure {
		t.Errorf("Presentation = %q, want %q", w.Presentation, WarningPresentationDisclosure)
	}
	if w.Code != WarningCodePaymentTermChanged {
		t.Errorf("Code = %q, want %q", w.Code, WarningCodePaymentTermChanged)
	}
}

func TestMessageInfoRoundTrip(t *testing.T) {
	data := []byte(`{"type": "info", "code": "free_shipping", "content": "Your order qualifies for free shipping."}`)
	msg := roundTrip[Message](t, data)
	i, ok := msg.AsInfo()
	if !ok {
		t.Fatal("AsInfo() ok = false, want true")
	}
	if i.Code != InfoCodeFreeShipping {
		t.Errorf("Code = %q, want %q", i.Code, InfoCodeFreeShipping)
	}
}

func TestMessageUnknownTypeRejected(t *testing.T) {
	var m Message
	err := json.Unmarshal([]byte(`{"type": "bogus", "content": "x"}`), &m)
	if err == nil {
		t.Fatal("Unmarshal() error = nil, want error for unknown discriminator")
	}
}

func TestMessageConstructors(t *testing.T) {
	m := NewErrorMessage(MessageError{Code: ErrorCodeNotFound, Content: "gone", Severity: MessageSeverityUnrecoverable})
	if m.Type() != "error" {
		t.Errorf("Type() = %q, want %q", m.Type(), "error")
	}
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var back Message
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	e, ok := back.AsError()
	if !ok || e.Code != ErrorCodeNotFound {
		t.Errorf("round trip via constructor failed: %+v", back)
	}
}

func TestMessagesArrayRoundTrip(t *testing.T) {
	data := []byte(`[
		{"type": "error", "code": "payment_failed", "content": "Card declined.", "severity": "recoverable"},
		{"type": "warning", "code": "age_restricted", "content": "Must be 21+."},
		{"type": "info", "content": "Thanks for your order."}
	]`)
	var msgs []Message
	if err := json.Unmarshal(data, &msgs); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("len(msgs) = %d, want 3", len(msgs))
	}
	out, err := json.Marshal(msgs)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var back []Message
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatalf("re-Unmarshal() error = %v", err)
	}
	if len(back) != 3 {
		t.Fatalf("len(back) = %d, want 3", len(back))
	}
	for i, m := range back {
		if m.Type() != msgs[i].Type() {
			t.Errorf("msgs[%d].Type() = %q, want %q", i, m.Type(), msgs[i].Type())
		}
	}
}
