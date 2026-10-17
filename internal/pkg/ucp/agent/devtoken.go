// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// Role is the principal role a dev token claims. The control plane's
// SimBearerResolver reads it straight out of the payload
// (internal/planes/control/auth/bearer.go).
type Role string

const (
	// RoleOwner is a merchant's own principal: every store-scoped call
	// (create, configure, pair, verify) is made as this. Owner rather than
	// staff because billing-gated writes pass on a dev merchant's default
	// trial billing status.
	RoleOwner Role = "owner"
	// RoleOperator is Sarvex's own staff principal. It carries no
	// merchant_id — which SimBearerResolver explicitly permits — and is the
	// only role that may call /operator/*, including the merchant-creation
	// endpoint a new tenant has to come from.
	RoleOperator Role = "operator"
)

// DevBearerToken mints the same unsigned, alg:none JWT-shaped token
// scripts/dev/token.sh does — accepted only by the control plane's
// SimBearerResolver (dev mode, KINDE_ISSUER_URL unset). It is never valid
// against a real deployment: nothing signs it and nothing verifies it.
//
// merchantID must be empty for RoleOperator and non-empty for every other
// role, mirroring the resolver's own rule; getting that backwards fails at
// the server with a message that does not say which side was wrong, so it
// is rejected here instead.
func DevBearerToken(role Role, merchantID string) (string, error) {
	switch {
	case role == RoleOperator && merchantID != "":
		return "", fmt.Errorf("dev token: operator role takes no merchant_id, got %q", merchantID)
	case role != RoleOperator && merchantID == "":
		return "", fmt.Errorf("dev token: role %q requires a merchant_id", role)
	}

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	claims := map[string]any{
		"sub":       "sarvex-cli",
		"email":     "cli@example.com",
		"role":      string(role),
		"store_ids": []string{},
	}
	if merchantID != "" {
		claims["merchant_id"] = merchantID
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".sig", nil
}
