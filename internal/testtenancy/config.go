// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package testtenancy provides a configurable tenant/store matrix for
// multi-tenant isolation negative tests (cross-tenant read, cross-tenant
// event consumption, cache-key collision), shared across the control, sync,
// and serving plane test suites plus the scenarios suite.
//
// It intentionally holds no Mongo/Redis/HTTP wiring of its own: each suite
// already has its own harness (see internal/planes/control/integration,
// internal/planes/sync/integration, internal/planes/serving/integration,
// internal/planes/scenarios), and this package's own scope stays outside
// internal/planes/ so it can be imported by all of them without any one
// plane importing another — the same precedent internal/planes/scenarios
// already sets by spanning multiple planes for black-box testing. Each
// suite seeds the tenants/stores this package describes using its own
// existing repo/seed helpers, looping over Config's matrix.
package testtenancy

import (
	"fmt"
	"os"
	"strconv"
)

// Config sizes the tenant/store matrix a test run seeds and asserts
// isolation across. Defaults (5 tenants x 3 stores/merchant) satisfy the
// task's stated minimum; both are overridable per run without a code
// change, matching loadtest/'s existing LOADTEST_STORES/
// LOADTEST_PRODUCTS_PER_STORE precedent (see loadtest/README.md).
type Config struct {
	NumTenants      int
	StoresPerTenant int
}

const (
	// EnvNumTenants overrides Config.NumTenants. Default 5.
	EnvNumTenants = "TENANCY_TEST_TENANTS"
	// EnvStoresPerTenant overrides Config.StoresPerTenant. Default 3.
	EnvStoresPerTenant = "TENANCY_TEST_STORES_PER_TENANT"

	defaultNumTenants      = 5
	defaultStoresPerTenant = 3
)

// LoadConfig reads Config from the environment, falling back to the
// defaults (5 tenants x 3 stores/merchant) for any unset or unparsable
// variable.
func LoadConfig() Config {
	return Config{
		NumTenants:      envInt(EnvNumTenants, defaultNumTenants),
		StoresPerTenant: envInt(EnvStoresPerTenant, defaultStoresPerTenant),
	}
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

// MerchantID returns the deterministic merchant id for tenant index i
// (0-based, i < cfg.NumTenants).
func MerchantID(i int) string {
	return fmt.Sprintf("tenancy-test-merch-%d", i)
}

// StoreID returns the deterministic store id for store index j (0-based,
// j < cfg.StoresPerTenant) belonging to tenant index i.
func StoreID(i, j int) string {
	return fmt.Sprintf("tenancy-test-store-%d-%d", i, j)
}

// Token returns the deterministic fake bearer token for tenant index i, in
// the same fixed-token style each suite's own testBearerResolver already
// uses (e.g. "merchant-a-token" -> merch-a). Suites wire this by extending
// their existing resolver to recognize TokenPrefix and delegate to
// TenantFromToken.
func Token(i int) string {
	return fmt.Sprintf("%s%d-token", TokenPrefix, i)
}

// TokenPrefix identifies a testtenancy-minted token so a suite's bearer
// resolver can recognize and delegate it alongside its own fixed tokens.
const TokenPrefix = "tenancy-test-tenant-"

// TenantFromToken extracts tenant index i from a token minted by Token, and
// reports whether tok was one of testtenancy's own tokens at all.
func TenantFromToken(tok string) (i int, ok bool) {
	if len(tok) <= len(TokenPrefix)+len("-token") || tok[:len(TokenPrefix)] != TokenPrefix {
		return 0, false
	}
	rest := tok[len(TokenPrefix):]
	const suffix = "-token"
	if len(rest) <= len(suffix) || rest[len(rest)-len(suffix):] != suffix {
		return 0, false
	}
	n, err := strconv.Atoi(rest[:len(rest)-len(suffix)])
	if err != nil {
		return 0, false
	}
	return n, true
}

// Tenant describes one seeded tenant: its merchant id and the store ids
// belonging to it.
type Tenant struct {
	MerchantID string
	StoreIDs   []string
}

// Matrix returns the full tenant/store matrix cfg describes, in
// deterministic order — the shape every new isolation test iterates to
// seed and then cross-check.
func Matrix(cfg Config) []Tenant {
	tenants := make([]Tenant, cfg.NumTenants)
	for i := range tenants {
		storeIDs := make([]string, cfg.StoresPerTenant)
		for j := range storeIDs {
			storeIDs[j] = StoreID(i, j)
		}
		tenants[i] = Tenant{MerchantID: MerchantID(i), StoreIDs: storeIDs}
	}
	return tenants
}
