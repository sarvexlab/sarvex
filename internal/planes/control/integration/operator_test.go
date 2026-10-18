// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestOperator_Board verifies GET /operator/board is accessible with the operator
// token and returns the standard list envelope.
func TestOperator_Board(t *testing.T) {
	resp := apiGet(t, "/v1/operator/board", tokenOperator)
	data, hasMore, _ := assertListEnvelope(t, resp)
	assert.False(t, hasMore)
	assert.NotNil(t, data)
}

// TestOperator_Board_ForbiddenForMerchant verifies that the board endpoint
// rejects a merchant token with 403.
func TestOperator_Board_ForbiddenForMerchant(t *testing.T) {
	resp := apiGet(t, "/v1/operator/board", tokenMerchantA)
	assertErrorEnvelope(t, resp, http.StatusForbidden)
}

// TestOperator_CaptureManualStep verifies POST /operator/manual-steps records
// the step and returns a well-formed response.
// The handler requires X-Operate-As to identify the target merchant.
func TestOperator_CaptureManualStep(t *testing.T) {
	body := map[string]any{
		"description": "manually updated DNS settings",
	}
	// Send via doWithOperateAs-style helper with a body.
	resp := apiPostOperateAs(t, "/v1/operator/manual-steps", tokenOperator, string(merchantAID), body)
	var result map[string]any
	decodeJSON(t, resp, http.StatusCreated, &result)
	assert.NotEmpty(t, result["manual_step_id"], "manual_step_id must be returned")
	assert.Equal(t, "manually updated DNS settings", result["description"])
}

// TestOperator_AuditLog verifies GET /operator/audit-log returns the list envelope.
func TestOperator_AuditLog(t *testing.T) {
	resp := apiGet(t, "/v1/operator/audit-log", tokenOperator)
	data, hasMore, _ := assertListEnvelope(t, resp)
	assert.False(t, hasMore)
	assert.NotNil(t, data)
}

// TestOperator_Elevation_ScopesRequest verifies that an operator using
// X-Operate-As sees the target merchant's resources, not a different merchant's.
func TestOperator_Elevation_ScopesRequest(t *testing.T) {
	seedAccount(t, merchantAID, "Elevation Test Corp")

	// Operator + X-Operate-As: merch-a → sees merch-a's account.
	resp, _ := doWithOperateAs(t, http.MethodGet, "/v1/account", tokenOperator, string(merchantAID))
	var result map[string]any
	decodeJSON(t, resp, http.StatusOK, &result)
	assert.Equal(t, "Elevation Test Corp", result["name"])
}
