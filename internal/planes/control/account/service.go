// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package account owns merchant identity and account data.
package account

import (
	"context"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// Service manages merchant account data and team members.
type Service interface {
	Get(ctx context.Context, id domain.MerchantID) (domain.Account, error)
	Update(ctx context.Context, id domain.MerchantID, update domain.AccountUpdate) (domain.Account, error)
	ListMembers(ctx context.Context, id domain.MerchantID, cursor string, limit int) (domain.ListOf[domain.Member], error)

	// InviteTeamMember is the account owner's self-serve invite: always invites as
	// staff, and is capped at the merchant's billing-plan seat limit (SeatLimiter).
	InviteTeamMember(ctx context.Context, merchantID domain.MerchantID, email string) (domain.Member, error)

	// Operator provisioning — architecture §3 Domain 1. ownerEmail is used to
	// provision the entry-level trial subscription (TrialProvisioner) — a
	// merchant account never exists without one.
	Create(ctx context.Context, name string, businessFacts *domain.BusinessFacts, ownerEmail string) (domain.Account, error)
	List(ctx context.Context, cursor string, limit int) (domain.ListOf[domain.Account], error)
	InviteMember(ctx context.Context, merchantID domain.MerchantID, email string, role domain.MemberRole) (domain.Member, error)

	// BindInvitedMember binds an invited member's IdP identity on first sign-in.
	BindInvitedMember(ctx context.Context, email string, idpSub string) (domain.Member, domain.MerchantID, error)
}

// SeatLimiter resolves the team-member seat cap tied to a merchant's
// subscription. billing.Service satisfies this structurally.
type SeatLimiter interface {
	Get(ctx context.Context, id domain.MerchantID) (domain.Billing, error)
}

// TrialProvisioner creates the Stripe customer and entry-level trial
// subscription for a newly created merchant. billing.Service satisfies this
// structurally.
type TrialProvisioner interface {
	ProvisionTrial(ctx context.Context, id domain.MerchantID, email string) error
}
