// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package paymenthandler

import (
	"testing"

	"github.com/sarvexlab/sarvex/internal/pkg/domain"
)

func TestLookup_ByIDAndName(t *testing.T) {
	for _, key := range []string{"google_pay", "com.google.pay"} {
		s, ok := Lookup(key)
		if !ok || s.ID != "google_pay" {
			t.Fatalf("Lookup(%q) = %+v, %v", key, s, ok)
		}
	}
	if _, ok := Lookup("stripe"); ok {
		t.Fatal("stripe must not resolve: it has no handler spec")
	}
}

// A spec restricted to some platforms applies only to them; an unrestricted
// one applies to every platform.
func TestAppliesTo(t *testing.T) {
	only := Spec{Platforms: []domain.CommerceSystemType{"woocommerce"}}
	if !only.AppliesTo("woocommerce") || only.AppliesTo("magento") {
		t.Fatal("a restricted handler must apply to its platforms only")
	}
	g, _ := Lookup("google_pay")
	if !g.AppliesTo("woocommerce") || !g.AppliesTo("magento") {
		t.Fatal("google_pay applies to every platform")
	}
}

func TestValidateParams_RejectsUnknownKeys(t *testing.T) {
	g, _ := Lookup("google_pay")
	if err := g.ValidateParams(map[string]string{"api_secret": "sk_live_x"}); err == nil {
		t.Fatal("an undeclared (potentially secret) key must be rejected, not stored")
	}
}

func TestStatusAndEnabledValidation(t *testing.T) {
	g, _ := Lookup("google_pay")
	full := map[string]string{
		"merchant_name": "M", "merchant_id": "1", "merchant_origin": "https://m.example",
		"gateway": "stripe", "gateway_merchant_id": "acct",
	}
	if got := g.Status(false, nil); got != StatusNotStarted {
		t.Errorf("status = %s", got)
	}
	if got := g.Status(true, map[string]string{"merchant_id": "1"}); got != StatusIncomplete {
		t.Errorf("status = %s", got)
	}
	if got := g.Status(true, full); got != StatusReady {
		t.Errorf("status = %s", got)
	}
	if err := g.ValidateEnabled(map[string]string{"merchant_id": "1"}); err == nil {
		t.Error("enabling without required parameters must fail")
	}
	if err := g.ValidateEnabled(full); err != nil {
		t.Errorf("full parameters must validate: %v", err)
	}
}

func TestProfileConfig_GooglePay(t *testing.T) {
	g, _ := Lookup("google_pay")
	cfg, err := g.ProfileConfig(map[string]string{
		"merchant_name": "M", "merchant_id": "1", "merchant_origin": "https://m.example",
		"gateway": "stripe", "gateway_merchant_id": "acct",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg["environment"] != "PRODUCTION" {
		t.Errorf("environment = %v, want PRODUCTION default", cfg["environment"])
	}
	mi, ok := cfg["merchant_info"].(map[string]any)
	if !ok {
		t.Fatalf("merchant_info missing or wrong type: %v", cfg["merchant_info"])
	}
	if mi["merchant_id"] != "1" || mi["merchant_origin"] != "https://m.example" {
		t.Errorf("merchant_info = %v", mi)
	}
	if _, err := g.ProfileConfig(nil); err == nil {
		t.Error("an unconfigured handler has no profile config")
	}
}
