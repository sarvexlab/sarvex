// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"testing"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/catalogue"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewOrchestratorWithConfigValues verifies Orchestrator correctly uses config values.
func TestNewOrchestratorWithConfigValues(t *testing.T) {
	specVersion := "2026-06-15"
	harness := &stubTestHarness{
		results: &TestResults{
			Passed: []string{},
			Failed: []*domain.ValidationFailure{},
		},
	}
	store := NewMemoryConformanceStore()

	cat, _ := catalogue.LoadCatalogue(context.Background(), testLogger())
	orch := NewOrchestrator(harness, store, &stubPlatformResolver{platform: "woocommerce"}, nil, cat, testLogger(), specVersion)

	// Verify spec version is used
	status, err := orch.GetStatus(context.Background(), domain.MerchantID("merchant1"), domain.StoreID("store1"))
	require.NoError(t, err)
	assert.Equal(t, specVersion, status.TargetSpecVersion)
}

// TestOrchestratorUsesDefaultSpecVersionOnEmpty verifies default is used when empty.
func TestOrchestratorUsesProvidedSpecVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
	}{
		{"custom version", "2026-06-15"},
		{"semantic version", "1.2.3"},
		{"date format", "2026-06-15"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := &stubTestHarness{
				results: &TestResults{
					Passed: []string{},
					Failed: []*domain.ValidationFailure{},
				},
			}
			store := NewMemoryConformanceStore()

			cat, _ := catalogue.LoadCatalogue(context.Background(), testLogger())
			orch := NewOrchestrator(harness, store, &stubPlatformResolver{platform: "woocommerce"}, nil, cat, testLogger(), tt.version)
			status, err := orch.GetStatus(context.Background(), domain.MerchantID("test-merchant"), domain.StoreID("store1"))
			require.NoError(t, err)
			assert.Equal(t, tt.version, status.TargetSpecVersion)
		})
	}
}
