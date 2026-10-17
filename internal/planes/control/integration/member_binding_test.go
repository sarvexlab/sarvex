// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/account"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// TestMemberBinding_Invite_CreatesInvitedMember verifies that POST /operator/merchants/{merchant_id}/members
// creates a member record with status=invited and user_id=pending:email.
func TestMemberBinding_Invite_CreatesInvitedMember(t *testing.T) {
	seedAccount(t, merchantAID, "Test Merchant")

	resp := apiPostOperateAs(t, "/v1/operator/merchants/"+string(merchantAID)+"/members", tokenOperator, string(merchantAID), map[string]any{
		"email": "invited@example.com",
		"role":  "staff",
	})

	var result map[string]any
	decodeJSON(t, resp, http.StatusCreated, &result)

	assert.Equal(t, "invited@example.com", result["email"])
	assert.Equal(t, "invited", result["status"])
	assert.NotEmpty(t, result["user_id"], "user_id should be set to pending:email until first sign-in")
}

// TestMemberBinding_Invite_ReinviteActiveReturns409 verifies that re-inviting an already-active
// member returns 409 Conflict.
func TestMemberBinding_Invite_ReinviteActiveReturns409(t *testing.T) {
	seedAccount(t, merchantAID, "Test Merchant")
	seedMember(t, merchantAID, "active@example.com", "user-123", domain.MemberStatusActive, domain.MemberRoleOwner)

	resp := apiPostOperateAs(t, "/v1/operator/merchants/"+string(merchantAID)+"/members", tokenOperator, string(merchantAID), map[string]any{
		"email": "active@example.com",
		"role":  "staff",
	})

	assertErrorEnvelope(t, resp, http.StatusConflict)
}

// TestMemberBinding_NoAutoCreateMerchant verifies that inviting a member doesn't
// auto-create the merchant account if it doesn't exist.
func TestMemberBinding_NoAutoCreateMerchant(t *testing.T) {
	nonExistentMerchant := domain.MerchantID("merch-does-not-exist")

	resp := apiPostOperateAs(t, "/v1/operator/merchants/"+string(nonExistentMerchant)+"/members", tokenOperator, string(nonExistentMerchant), map[string]any{
		"email": "invited@example.com",
		"role":  "owner",
	})

	// Should return 404 since merchant doesn't exist.
	assertErrorEnvelope(t, resp, http.StatusNotFound)
}

// TestMemberBinding_BindInvitedMember_UnitsTest tests the member binding logic at the service level.
func TestMemberBinding_BindInvitedMember_UnitTest(t *testing.T) {
	accountRepo, _ := account.NewMongoAccountRepository(env.db)
	memberRepo, _ := account.NewMongoMemberRepository(env.db)
	accountSvc := account.NewAccountService(accountRepo, memberRepo, nil, nil, nil) // seatLimiter, trialProvisioner unused: InviteTeamMember/Create not exercised here

	ctx := context.Background()

	// The merchant account has to exist first: inviting into an unprovisioned
	// account is refused, precisely so that BindInvitedMember below can never
	// be handed a member whose merchant was never created.
	merchantID := domain.MerchantID("merch-bind-test")
	seedAccount(t, merchantID, "Bind Test Merchant")

	// Create an invited member.
	member, err := accountSvc.InviteMember(ctx, merchantID, "bind@example.com", domain.MemberRoleStaff)
	require.NoError(t, err)
	assert.Equal(t, domain.MemberStatusInvited, member.Status)
	// user_id is a "pending:<email>" placeholder until first sign-in swaps it
	// for the IdP sub claim — which is exactly what the bind below asserts.
	assert.Equal(t, "pending:bind@example.com", member.UserID)

	// Bind the member on first sign-in.
	boundMember, boundMerchantID, err := accountSvc.BindInvitedMember(ctx, "bind@example.com", "idp-sub-123")
	require.NoError(t, err)
	assert.Equal(t, domain.MemberStatusActive, boundMember.Status)
	assert.Equal(t, "idp-sub-123", boundMember.UserID)
	assert.Equal(t, merchantID, boundMerchantID)

	// Bind again with same email (idempotent check) — should not error.
	boundMember2, boundMerchantID2, err := accountSvc.BindInvitedMember(ctx, "bind@example.com", "idp-sub-123")
	require.NoError(t, err)
	assert.Equal(t, domain.MemberStatusActive, boundMember2.Status)
	assert.Equal(t, "idp-sub-123", boundMember2.UserID)
	assert.Equal(t, merchantID, boundMerchantID2)
}

// TestMemberBinding_BindNonExistentEmail returns ErrNotFound.
func TestMemberBinding_BindNonExistentEmail_UnitTest(t *testing.T) {
	accountRepo, _ := account.NewMongoAccountRepository(env.db)
	memberRepo, _ := account.NewMongoMemberRepository(env.db)
	accountSvc := account.NewAccountService(accountRepo, memberRepo, nil, nil, nil) // seatLimiter, trialProvisioner unused: InviteTeamMember/Create not exercised here

	ctx := context.Background()

	// Try to bind an email that was never invited.
	_, _, err := accountSvc.BindInvitedMember(ctx, "never-invited@example.com", "idp-sub-456")
	assert.Equal(t, account.ErrNotFound, err)
}

// ── Helpers ────────────────────────────────────────────────────────────────

// seedMember writes a member directly to MongoDB for test setup.
func seedMember(t *testing.T, merchantID domain.MerchantID, email string, userID string, status domain.MemberStatus, role domain.MemberRole) {
	t.Helper()
	err := env.memberRepo.UpsertMember(context.Background(), &account.MemberModel{
		UserID:     userID,
		MerchantID: string(merchantID),
		Email:      email,
		Roles:      []string{string(role)},
		Status:     string(status),
	})
	require.NoError(t, err)
}
