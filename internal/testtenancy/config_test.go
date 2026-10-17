// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package testtenancy_test

import (
	"testing"

	"github.com/sarvexlabs/sarvex.cp/internal/testtenancy"
)

func TestLoadConfig_Defaults(t *testing.T) {
	t.Setenv(testtenancy.EnvNumTenants, "")
	t.Setenv(testtenancy.EnvStoresPerTenant, "")

	cfg := testtenancy.LoadConfig()
	if cfg.NumTenants != 5 {
		t.Fatalf("expected default NumTenants=5, got %d", cfg.NumTenants)
	}
	if cfg.StoresPerTenant != 3 {
		t.Fatalf("expected default StoresPerTenant=3, got %d", cfg.StoresPerTenant)
	}
}

func TestLoadConfig_EnvOverride(t *testing.T) {
	t.Setenv(testtenancy.EnvNumTenants, "8")
	t.Setenv(testtenancy.EnvStoresPerTenant, "4")

	cfg := testtenancy.LoadConfig()
	if cfg.NumTenants != 8 {
		t.Fatalf("expected NumTenants=8, got %d", cfg.NumTenants)
	}
	if cfg.StoresPerTenant != 4 {
		t.Fatalf("expected StoresPerTenant=4, got %d", cfg.StoresPerTenant)
	}
}

func TestLoadConfig_InvalidFallsBackToDefault(t *testing.T) {
	t.Setenv(testtenancy.EnvNumTenants, "not-a-number")
	t.Setenv(testtenancy.EnvStoresPerTenant, "-1")

	cfg := testtenancy.LoadConfig()
	if cfg.NumTenants != 5 {
		t.Fatalf("expected fallback NumTenants=5, got %d", cfg.NumTenants)
	}
	if cfg.StoresPerTenant != 3 {
		t.Fatalf("expected fallback StoresPerTenant=3, got %d", cfg.StoresPerTenant)
	}
}

func TestTokenRoundTrip(t *testing.T) {
	for i := range 10 {
		tok := testtenancy.Token(i)
		got, ok := testtenancy.TenantFromToken(tok)
		if !ok {
			t.Fatalf("TenantFromToken(%q) reported not-ok", tok)
		}
		if got != i {
			t.Fatalf("TenantFromToken(%q) = %d, want %d", tok, got, i)
		}
	}
}

func TestTenantFromToken_RejectsForeignTokens(t *testing.T) {
	for _, tok := range []string{"merchant-a-token", "operator-token", "", "tenancy-test-tenant-notanumber-token"} {
		if _, ok := testtenancy.TenantFromToken(tok); ok {
			t.Fatalf("TenantFromToken(%q) should not be recognized as a testtenancy token", tok)
		}
	}
}

func TestMatrix_Shape(t *testing.T) {
	cfg := testtenancy.Config{NumTenants: 5, StoresPerTenant: 3}
	tenants := testtenancy.Matrix(cfg)
	if len(tenants) != 5 {
		t.Fatalf("expected 5 tenants, got %d", len(tenants))
	}

	seenMerchants := make(map[string]bool)
	seenStores := make(map[string]bool)
	for _, tn := range tenants {
		if seenMerchants[tn.MerchantID] {
			t.Fatalf("duplicate merchant id %q", tn.MerchantID)
		}
		seenMerchants[tn.MerchantID] = true

		if len(tn.StoreIDs) != 3 {
			t.Fatalf("expected 3 stores for merchant %q, got %d", tn.MerchantID, len(tn.StoreIDs))
		}
		for _, sid := range tn.StoreIDs {
			if seenStores[sid] {
				t.Fatalf("duplicate store id %q across the whole matrix", sid)
			}
			seenStores[sid] = true
		}
	}
}
