// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package account

import (
	"context"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// AccountRepository is the persistence interface for merchant account records.
// The single writer is AccountService; every other reader consumes events.
type AccountRepository interface {
	// Get returns ErrNotFound when no record exists for id.
	Get(ctx context.Context, id domain.MerchantID) (*AccountModel, error)
	// Upsert creates or replaces the account document.
	Upsert(ctx context.Context, m *AccountModel) error
}

// MemberRepository is the persistence interface for team member records.
type MemberRepository interface {
	// ListByMerchant returns up to limit members for id, ordered by user_id.
	// afterUserID is the cursor — pass "" for the first page.
	// hasMore is true when more records exist beyond the returned slice.
	ListByMerchant(ctx context.Context, id domain.MerchantID, afterUserID string, limit int) ([]*MemberModel, bool, error)
	// UpsertMember creates or replaces a member document.
	UpsertMember(ctx context.Context, m *MemberModel) error
	// GetByEmail returns the member with the given email for the merchant.
	// Returns ErrNotFound if no member exists.
	GetByEmail(ctx context.Context, merchantID domain.MerchantID, email string) (*MemberModel, error)
	// GetInvitedByEmail returns the invited member with the given email, searching
	// across all merchants. Used for binding invited members on first sign-in.
	// Returns ErrNotFound if no invited member exists with that email.
	GetInvitedByEmail(ctx context.Context, email string) (*MemberModel, error)
	// GetActiveByEmail returns the active member with the given email, searching
	// across all merchants. It is what makes binding idempotent: once first
	// sign-in has flipped a member from invited to active, GetInvitedByEmail no
	// longer matches it, and a repeated bind has to resolve to the same member
	// rather than failing. Returns ErrNotFound if no active member has that email.
	GetActiveByEmail(ctx context.Context, email string) (*MemberModel, error)
}

// AccountListRepository extends AccountRepository with cross-merchant listing for
// operator surfaces. Implemented by the same Mongo repo; kept separate so
// non-operator services can't accidentally list all merchants.
type AccountListRepository interface {
	AccountRepository
	// List returns all accounts, newest first, for the operator board.
	List(ctx context.Context, afterID string, limit int) ([]*AccountModel, bool, error)
}
