// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// decodeClaims pulls the payload out of a dev token the way the control
// plane's SimBearerResolver does — base64url, no signature check.
func decodeClaims(t *testing.T, token string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d parts, want 3: %q", len(parts), token)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return claims
}

func TestDevBearerTokenOwnerCarriesMerchantID(t *testing.T) {
	token, err := DevBearerToken(RoleOwner, "merch-123")
	if err != nil {
		t.Fatalf("DevBearerToken: %v", err)
	}
	claims := decodeClaims(t, token)
	if got := claims["merchant_id"]; got != "merch-123" {
		t.Errorf("merchant_id = %v, want merch-123", got)
	}
	if got := claims["role"]; got != "owner" {
		t.Errorf("role = %v, want owner", got)
	}
	if got, _ := claims["sub"].(string); got == "" {
		t.Error("sub claim is empty; SimBearerResolver rejects that")
	}
}

// The operator principal is the only one allowed to carry no merchant_id,
// and it must not carry one: a merchant-scoped token is rejected by the
// operator-only routes it exists to reach.
func TestDevBearerTokenOperatorOmitsMerchantID(t *testing.T) {
	token, err := DevBearerToken(RoleOperator, "")
	if err != nil {
		t.Fatalf("DevBearerToken: %v", err)
	}
	claims := decodeClaims(t, token)
	if _, present := claims["merchant_id"]; present {
		t.Errorf("operator token carries merchant_id = %v, want it absent", claims["merchant_id"])
	}
	if got := claims["role"]; got != "operator" {
		t.Errorf("role = %v, want operator", got)
	}
}

func TestDevBearerTokenRejectsMismatchedRoleAndMerchant(t *testing.T) {
	tests := []struct {
		name       string
		role       Role
		merchantID string
	}{
		{"operator with merchant", RoleOperator, "merch-123"},
		{"owner without merchant", RoleOwner, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := DevBearerToken(tt.role, tt.merchantID); err == nil {
				t.Fatal("expected an error, got nil")
			}
		})
	}
}
