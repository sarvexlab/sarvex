// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package v20260408

import "testing"

func TestReverseDomainNameValidate(t *testing.T) {
	valid := []ReverseDomainName{"dev.ucp.shopping.checkout", "com.example.loyalty_gold", "a.b.c"}
	for _, v := range valid {
		if err := v.Validate(); err != nil {
			t.Errorf("Validate(%q) error = %v, want nil", v, err)
		}
	}
	invalid := []ReverseDomainName{"nodots", "Dev.Ucp", "dev..ucp", "", "1dev.ucp"}
	for _, v := range invalid {
		if err := v.Validate(); err == nil {
			t.Errorf("Validate(%q) error = nil, want error", v)
		}
	}
}

func TestTotalsValidate(t *testing.T) {
	tests := []struct {
		name    string
		totals  Totals
		wantErr bool
	}{
		{
			name:    "exactly one subtotal and total passes",
			totals:  Totals{{Type: TotalTypeSubtotal, Amount: 1000}, {Type: TotalTypeTax, Amount: 80}, {Type: TotalTypeTotal, Amount: 1080}},
			wantErr: false,
		},
		{
			name:    "missing subtotal fails",
			totals:  Totals{{Type: TotalTypeTotal, Amount: 1000}},
			wantErr: true,
		},
		{
			name:    "missing total fails",
			totals:  Totals{{Type: TotalTypeSubtotal, Amount: 1000}},
			wantErr: true,
		},
		{
			name:    "duplicate subtotal fails",
			totals:  Totals{{Type: TotalTypeSubtotal, Amount: 1000}, {Type: TotalTypeSubtotal, Amount: 500}, {Type: TotalTypeTotal, Amount: 1500}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.totals.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSignalsUnknownKeyRoundTrip(t *testing.T) {
	data := []byte(`{
		"dev.ucp.buyer_ip": "203.0.113.5",
		"dev.ucp.user_agent": "TestAgent/1.0",
		"com.example.device_id": "device-123",
		"com.example.risk_score": 42
	}`)
	s := roundTrip[Signals](t, data)
	if s.BuyerIP != "203.0.113.5" {
		t.Errorf("BuyerIP = %q, want %q", s.BuyerIP, "203.0.113.5")
	}
	if s.UserAgent != "TestAgent/1.0" {
		t.Errorf("UserAgent = %q, want %q", s.UserAgent, "TestAgent/1.0")
	}
	if s.Extra["com.example.device_id"] != "device-123" {
		t.Errorf("Extra[device_id] = %v, want device-123", s.Extra["com.example.device_id"])
	}
	if s.Extra["com.example.risk_score"] != float64(42) {
		t.Errorf("Extra[risk_score] = %v, want 42", s.Extra["com.example.risk_score"])
	}
}

func TestBuyerExtraRoundTrip(t *testing.T) {
	data := []byte(`{"first_name": "Jane", "email": "jane@example.com", "loyalty_tier": "gold"}`)
	b := roundTrip[Buyer](t, data)
	if b.FirstName != "Jane" {
		t.Errorf("FirstName = %q, want %q", b.FirstName, "Jane")
	}
	if b.Extra["loyalty_tier"] != "gold" {
		t.Errorf("Extra[loyalty_tier] = %v, want gold", b.Extra["loyalty_tier"])
	}
}

func TestContextExtraRoundTrip(t *testing.T) {
	data := []byte(`{"currency": "USD", "eligibility": ["com.example.loyalty_gold"], "custom_hint": true}`)
	c := roundTrip[Context](t, data)
	if c.Currency != "USD" {
		t.Errorf("Currency = %q, want %q", c.Currency, "USD")
	}
	if len(c.Eligibility) != 1 || c.Eligibility[0] != "com.example.loyalty_gold" {
		t.Errorf("Eligibility = %v", c.Eligibility)
	}
	if c.Extra["custom_hint"] != true {
		t.Errorf("Extra[custom_hint] = %v, want true", c.Extra["custom_hint"])
	}
}

func TestRetailLocationExtraRoundTrip(t *testing.T) {
	data := []byte(`{"id": "loc_1", "name": "Downtown", "hours": "9-5"}`)
	r := roundTrip[RetailLocation](t, data)
	if r.Name != "Downtown" {
		t.Errorf("Name = %q, want %q", r.Name, "Downtown")
	}
	if r.Extra["hours"] != "9-5" {
		t.Errorf("Extra[hours] = %v, want 9-5", r.Extra["hours"])
	}
}

func TestSearchFiltersExtraRoundTrip(t *testing.T) {
	data := []byte(`{"categories": ["shoes"], "price": {"min": 1000, "max": 5000}, "in_stock_only": true}`)
	f := roundTrip[SearchFilters](t, data)
	if len(f.Categories) != 1 || f.Categories[0] != "shoes" {
		t.Errorf("Categories = %v", f.Categories)
	}
	if f.Price == nil || *f.Price.Min != 1000 {
		t.Errorf("Price = %+v", f.Price)
	}
	if f.Extra["in_stock_only"] != true {
		t.Errorf("Extra[in_stock_only] = %v, want true", f.Extra["in_stock_only"])
	}
}

func TestDescriptionValidate(t *testing.T) {
	if err := (Description{}).Validate(); err == nil {
		t.Error("Validate() error = nil, want error for empty description")
	}
	if err := (Description{Plain: "hello"}).Validate(); err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}
}

func TestErrorResponseValidate(t *testing.T) {
	r := ErrorResponse{UCP: ErrorResponseMetadata{UCPMetadataBase: UCPMetadataBase{Version: "2026-04-08", Status: UCPStatusError}}}
	if err := r.Validate(); err == nil {
		t.Error("Validate() error = nil, want error for zero messages")
	}
	r.Messages = []Message{NewErrorMessage(MessageError{Code: ErrorCodeNotFound, Content: "x", Severity: MessageSeverityUnrecoverable})}
	if err := r.Validate(); err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}
}

func TestItemPricePointerDistinguishesAbsentFromFree(t *testing.T) {
	withPrice := roundTrip[Item](t, []byte(`{"id": "sku_1", "price": 0}`))
	if withPrice.Price == nil || *withPrice.Price != 0 {
		t.Errorf("Price = %v, want pointer to 0 (explicit free item)", withPrice.Price)
	}
	withoutPrice := roundTrip[Item](t, []byte(`{"id": "sku_1"}`))
	if withoutPrice.Price != nil {
		t.Errorf("Price = %v, want nil (absent)", withoutPrice.Price)
	}
}

func TestVariantRoundTrip(t *testing.T) {
	data := []byte(`{
		"id": "var_1",
		"sku": "SKU-1",
		"title": "Blue / Large",
		"description": {"plain": "A nice widget."},
		"price": {"amount": 1999, "currency": "USD"},
		"options": [{"name": "Color", "label": "Blue"}, {"name": "Size", "label": "Large"}],
		"availability": {"available": true, "status": "in_stock"},
		"barcodes": [{"type": "UPC", "value": "012345678905"}]
	}`)
	v := roundTrip[Variant](t, data)
	if v.Availability == nil || v.Availability.Status != VariantAvailabilityStatusInStock {
		t.Errorf("Availability = %+v", v.Availability)
	}
	if len(v.Barcodes) != 1 || v.Barcodes[0].Type != BarcodeTypeUPC {
		t.Errorf("Barcodes = %+v", v.Barcodes)
	}
}

func TestAdjustmentRoundTrip(t *testing.T) {
	data := []byte(`{
		"id": "adj_1",
		"type": "refund",
		"occurred_at": "2026-04-08T12:00:00Z",
		"status": "completed",
		"line_items": [{"id": "li_1", "quantity": -1}],
		"totals": [{"type": "total", "amount": -1000}],
		"description": "Customer requested"
	}`)
	a := roundTrip[Adjustment](t, data)
	if a.Type != AdjustmentTypeRefund {
		t.Errorf("Type = %q, want %q", a.Type, AdjustmentTypeRefund)
	}
	tm, err := a.Time()
	if err != nil {
		t.Fatalf("Time() error = %v", err)
	}
	if tm.Year() != 2026 {
		t.Errorf("Time().Year() = %d, want 2026", tm.Year())
	}
}
