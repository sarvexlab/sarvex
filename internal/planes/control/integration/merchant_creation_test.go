// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// TestMerchantCreation_ProvisionsTrial verifies that POST /operator/merchants
// never creates a merchant without provisioning its entry-level trial
// subscription — a merchant account must never exist without one.
func TestMerchantCreation_ProvisionsTrial(t *testing.T) {
	resp := apiPost(t, "/v1/operator/merchants", tokenOperator, map[string]any{
		"name":        "Trial Test Merchant",
		"owner_email": "owner@trialtest.example.com",
	})

	var result map[string]any
	decodeJSON(t, resp, http.StatusCreated, &result)

	merchantID := domain.MerchantID(result["merchant_id"].(string))
	require.NotEmpty(t, merchantID)

	email, ok := env.trialProvisioner.calls[merchantID]
	require.True(t, ok, "expected ProvisionTrial to have been called for %s", merchantID)
	assert.Equal(t, "owner@trialtest.example.com", email)
}
