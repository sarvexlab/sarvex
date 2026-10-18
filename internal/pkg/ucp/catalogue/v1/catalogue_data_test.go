// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package v1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultCatalogueStructure(t *testing.T) {
	cat := DefaultCatalogue()
	require.NotNil(t, cat)

	// Verify basic fields
	assert.NotEmpty(t, cat.Version)
	assert.NotEmpty(t, cat.SpecVersion)
	assert.Equal(t, cat.Version, cat.SpecVersion)
	assert.NotZero(t, cat.BuiltAt)
	assert.True(t, cat.BuiltAt.Before(time.Now().Add(1*time.Second)))
}

func TestDefaultCatalogueHasServices(t *testing.T) {
	cat := DefaultCatalogue()
	require.NotNil(t, cat)

	// Should have at least one service from backend registry
	assert.Greater(t, len(cat.Services), 0)

	// Each service should have required fields
	for _, svc := range cat.Services {
		assert.NotEmpty(t, svc.ID)
		assert.NotEmpty(t, svc.Version)
	}
}

func TestDefaultCatalogueServiceFields(t *testing.T) {
	cat := DefaultCatalogue()
	require.NotNil(t, cat)

	// Verify each service has expected fields
	for _, svc := range cat.Services {
		assert.NotEmpty(t, svc.ID)
		assert.NotEmpty(t, svc.Version)
	}
}

func TestDefaultCatalogueCapabilities(t *testing.T) {
	cat := DefaultCatalogue()
	require.NotNil(t, cat)

	// Capabilities are dynamically built from backend registry
	// They may be empty depending on backend configuration

	// If capabilities exist, verify they are valid
	for _, cap := range cat.Capabilities {
		assert.NotEmpty(t, cap.ID)
		// Name and Version may be generated from backend registry, ensure at least ID is present
	}
}

func TestDefaultCataloguePaymentHandlers(t *testing.T) {
	cat := DefaultCatalogue()
	require.NotNil(t, cat)

	// Payment handlers are dynamically built from backend registry
	// They may be empty depending on backend configuration

	// If handlers exist, verify they have required fields
	for _, handler := range cat.PaymentHandlers {
		assert.NotEmpty(t, handler.ID)
		assert.NotEmpty(t, handler.Version)
	}
}

func TestDefaultCatalogueScoringRubric(t *testing.T) {
	cat := DefaultCatalogue()
	require.NotNil(t, cat)

	// Should have scoring rubric
	require.NotNil(t, cat.ScoringRubric)
	assert.NotEmpty(t, cat.ScoringRubric.Version)
	assert.Greater(t, len(cat.ScoringRubric.Rules), 0)
	assert.Greater(t, len(cat.ScoringRubric.Remediations), 0)
}

func TestDefaultCatalogueConsistency(t *testing.T) {
	// Call twice to ensure consistency
	cat1 := DefaultCatalogue()
	cat2 := DefaultCatalogue()

	require.NotNil(t, cat1)
	require.NotNil(t, cat2)

	// Same number of services, capabilities, extensions, handlers
	assert.Equal(t, len(cat1.Services), len(cat2.Services))
	assert.Equal(t, len(cat1.Capabilities), len(cat2.Capabilities))
	assert.Equal(t, len(cat1.PaymentHandlers), len(cat2.PaymentHandlers))

	// Same scoring rules
	assert.Equal(t, len(cat1.ScoringRubric.Rules), len(cat2.ScoringRubric.Rules))
	assert.Equal(t, len(cat1.ScoringRubric.Remediations), len(cat2.ScoringRubric.Remediations))
}

func TestDefaultCatalogueNoNilFields(t *testing.T) {
	cat := DefaultCatalogue()
	require.NotNil(t, cat)
	require.NotNil(t, cat.ScoringRubric)
	require.NotNil(t, cat.Services)
	require.NotNil(t, cat.Capabilities)
	require.NotNil(t, cat.PaymentHandlers)
}
