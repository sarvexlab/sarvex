// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package account

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"
	"github.com/samber/lo"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// AccountService is the production implementation of Service.
type AccountService struct {
	accounts         AccountListRepository
	members          MemberRepository
	mailer           Mailer
	seatLimiter      SeatLimiter
	trialProvisioner TrialProvisioner
}

// NewAccountService constructs a Service backed by MongoDB.
func NewAccountService(accounts AccountListRepository, members MemberRepository, mailer Mailer, seatLimiter SeatLimiter, trialProvisioner TrialProvisioner) Service {
	if mailer == nil {
		mailer = NopMailer{}
	}
	return &AccountService{
		accounts:         accounts,
		members:          members,
		mailer:           mailer,
		seatLimiter:      seatLimiter,
		trialProvisioner: trialProvisioner,
	}
}

func (s *AccountService) Get(ctx context.Context, id domain.MerchantID) (domain.Account, error) {
	m, err := s.accounts.Get(ctx, id)
	if err != nil {
		return domain.Account{}, err
	}
	return m.ToDomain(), nil
}

func (s *AccountService) Update(ctx context.Context, id domain.MerchantID, update domain.AccountUpdate) (domain.Account, error) {
	m, err := s.accounts.Get(ctx, id)
	if errors.Is(err, ErrNotFound) {
		m = &AccountModel{
			ID:        string(id),
			CreatedAt: time.Now().UTC(),
		}
	} else if err != nil {
		return domain.Account{}, err
	}

	if update.Name != nil {
		m.Name = *update.Name
	}
	if update.Timezone != nil {
		m.Timezone = *update.Timezone
	}
	if update.CountryCode != nil {
		m.CountryCode = *update.CountryCode
	}
	if update.BusinessFacts != nil {
		m.BusinessFacts = BusinessFactsModel{
			LegalEntityName:    update.BusinessFacts.LegalEntityName,
			SupportEmail:       update.BusinessFacts.SupportEmail,
			SupportPhone:       update.BusinessFacts.SupportPhone,
			FulfillmentRegions: update.BusinessFacts.FulfillmentRegions,
		}
	}
	m.UpdatedAt = time.Now().UTC()

	if err := s.accounts.Upsert(ctx, m); err != nil {
		return domain.Account{}, err
	}
	return m.ToDomain(), nil
}

func (s *AccountService) ListMembers(ctx context.Context, id domain.MerchantID, cursor string, limit int) (domain.ListOf[domain.Member], error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	members, hasMore, err := s.members.ListByMerchant(ctx, id, cursor, limit)
	if err != nil {
		return domain.ListOf[domain.Member]{}, err
	}

	items := lo.Map(members, func(m *MemberModel, _ int) domain.Member { return m.ToDomain() })

	var nextCursor *string
	if hasMore && len(members) > 0 {
		last := members[len(members)-1].UserID
		nextCursor = &last
	}

	return domain.ListOf[domain.Member]{
		Data: items,
		Page: domain.Page{Cursor: nextCursor, HasMore: hasMore},
	}, nil
}

func (s *AccountService) Create(ctx context.Context, name string, businessFacts *domain.BusinessFacts, ownerEmail string) (domain.Account, error) {
	if strings.TrimSpace(name) == "" {
		return domain.Account{}, fmt.Errorf("name is required")
	}

	id, err := gonanoid.New(12)
	if err != nil {
		return domain.Account{}, fmt.Errorf("generate merchant id: %w", err)
	}
	merchantID := "merch_" + id

	now := time.Now().UTC()
	m := &AccountModel{
		ID:        merchantID,
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if businessFacts != nil {
		m.BusinessFacts = BusinessFactsModel{
			LegalEntityName:    businessFacts.LegalEntityName,
			SupportEmail:       businessFacts.SupportEmail,
			SupportPhone:       businessFacts.SupportPhone,
			FulfillmentRegions: businessFacts.FulfillmentRegions,
		}
	}

	if err := s.accounts.Upsert(ctx, m); err != nil {
		return domain.Account{}, fmt.Errorf("create merchant: %w", err)
	}

	// A merchant account never exists without an active subscription — provision
	// the entry-level trial synchronously, so account creation as a whole fails
	// if billing can't be set up.
	if err := s.trialProvisioner.ProvisionTrial(ctx, domain.MerchantID(merchantID), ownerEmail); err != nil {
		return domain.Account{}, fmt.Errorf("provision trial subscription for %s: %w", merchantID, err)
	}

	return m.ToDomain(), nil
}

func (s *AccountService) List(ctx context.Context, cursor string, limit int) (domain.ListOf[domain.Account], error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	accounts, hasMore, err := s.accounts.List(ctx, cursor, limit)
	if err != nil {
		return domain.ListOf[domain.Account]{}, err
	}
	items := lo.Map(accounts, func(a *AccountModel, _ int) domain.Account { return a.ToDomain() })
	var nextCursor *string
	if hasMore && len(accounts) > 0 {
		last := accounts[len(accounts)-1].ID
		nextCursor = &last
	}
	return domain.ListOf[domain.Account]{
		Data: items,
		Page: domain.Page{Cursor: nextCursor, HasMore: hasMore},
	}, nil
}

// InviteMember creates a Sarvex member record authorizing email for this merchant
// and sends a sign-in link invitation email.
// BindInvitedMember binds an invited member's IdP identity (sub claim) on first sign-in.
// Returns the bound Member and the MerchantID for scope resolution.
// If no invited member exists for this email, returns ErrNotFound.
// If the member is already active, returns the active member without changes (idempotent).
func (s *AccountService) BindInvitedMember(ctx context.Context, email string, idpSub string) (domain.Member, domain.MerchantID, error) {
	// Find the invited member by email.
	m, err := s.members.GetInvitedByEmail(ctx, email)
	if errors.Is(err, ErrNotFound) {
		// No *invited* member — but this is the sign-in path, which retries,
		// and the first successful bind already flipped the member to active.
		// Without this fallback the bind worked exactly once: every later
		// request whose token the primary resolver can't resolve fell through
		// to here, got ErrNotFound, and 401'd a legitimately bound user.
		return s.rebindActiveMember(ctx, email, idpSub)
	}
	if err != nil {
		return domain.Member{}, "", err
	}

	// Update the member to bind the IdP identity and activate.
	// Idempotent: if already active, this is a no-op.
	m.UserID = idpSub
	m.Status = string(domain.MemberStatusActive)

	if err := s.members.UpsertMember(ctx, m); err != nil {
		return domain.Member{}, "", fmt.Errorf("bind invited member: %w", err)
	}

	return m.ToDomain(), domain.MerchantID(m.MerchantID), nil
}

// rebindActiveMember resolves a repeat bind for an already-active member.
//
// The IdP subject must match the one recorded at first bind. An email alone is
// not proof of identity here — this runs before the caller has a verified
// principal — so allowing a different sub to resolve an active member would let
// anyone presenting a token for that address inherit the member's merchant
// scope. A mismatch is reported as ErrNotFound, exactly as if no member existed.
func (s *AccountService) rebindActiveMember(ctx context.Context, email, idpSub string) (domain.Member, domain.MerchantID, error) {
	m, err := s.members.GetActiveByEmail(ctx, email)
	if err != nil {
		return domain.Member{}, "", err
	}
	if m.UserID != idpSub {
		return domain.Member{}, "", ErrNotFound
	}
	return m.ToDomain(), domain.MerchantID(m.MerchantID), nil
}

func (s *AccountService) InviteMember(ctx context.Context, merchantID domain.MerchantID, email string, role domain.MemberRole) (domain.Member, error) {
	if email == "" {
		return domain.Member{}, fmt.Errorf("email is required")
	}
	if role == "" {
		role = domain.MemberRoleOwner
	}
	return s.createMemberInvite(ctx, merchantID, email, role)
}

// InviteTeamMember is the account owner's self-serve invite (Team panel, Settings).
// Unlike InviteMember (operator-only provisioning), it is capped at the merchant's
// billing-plan seat limit and always invites as staff — role changes and owner
// reassignment remain operator-assisted.
func (s *AccountService) InviteTeamMember(ctx context.Context, merchantID domain.MerchantID, email string) (domain.Member, error) {
	if email == "" {
		return domain.Member{}, fmt.Errorf("email is required")
	}

	b, err := s.seatLimiter.Get(ctx, merchantID)
	if err != nil {
		return domain.Member{}, fmt.Errorf("get billing for seat check: %w", err)
	}

	existing, _, err := s.members.ListByMerchant(ctx, merchantID, "", 100)
	if err != nil {
		return domain.Member{}, fmt.Errorf("list members for seat check: %w", err)
	}
	if len(existing) >= b.Usage.MemberCap {
		return domain.Member{}, ErrMemberLimitReached
	}

	return s.createMemberInvite(ctx, merchantID, email, domain.MemberRoleStaff)
}

// createMemberInvite creates the Sarvex-side member record and sends the sign-in
// link. Shared by the operator-provisioning invite and the owner self-serve invite;
// callers are responsible for their own authorization and seat-limit checks.
func (s *AccountService) createMemberInvite(ctx context.Context, merchantID domain.MerchantID, email string, role domain.MemberRole) (domain.Member, error) {
	// The merchant account must already exist. Inviting into an account that
	// was never created produced an orphaned member record: BindInvitedMember
	// refuses to bind it at first sign-in ("account: not found"), so the
	// invitee could never complete sign-up and nothing surfaced the dead
	// record. The operator handler already maps ErrNotFound to 404; it simply
	// never had an error to map.
	//
	// The lookup is not extra work — the invite email below already needs the
	// account for its display name.
	account, err := s.accounts.Get(ctx, merchantID)
	if err != nil {
		return domain.Member{}, fmt.Errorf("invite member: %w", err)
	}
	if account == nil {
		return domain.Member{}, ErrNotFound
	}

	// Check if member already exists with this email (could be pending, active, or invited).
	existing, _, err := s.members.ListByMerchant(ctx, merchantID, "", 100)
	if err == nil && lo.SomeBy(existing, func(m *MemberModel) bool {
		return m.Email == email && m.Status == string(domain.MemberStatusActive)
	}) {
		// Attempt to re-invite an active member.
		return domain.Member{}, ErrMemberAlreadyActive
	}

	// Use email as a placeholder user_id until first sign-in binds the IdP sub claim.
	m := &MemberModel{
		UserID:     "pending:" + email,
		MerchantID: string(merchantID),
		Email:      email,
		Roles:      []string{string(role)},
		Status:     string(domain.MemberStatusInvited),
	}
	if err := s.members.UpsertMember(ctx, m); err != nil {
		return domain.Member{}, fmt.Errorf("invite member: %w", err)
	}

	// Send invitation email (non-blocking; mailer is responsible for retries).
	// Use a placeholder link here; in production, this would include a token or URL scheme.
	signInLink := "https://merchant.example.com/signin"
	_ = s.mailer.SendInviteLink(ctx, email, account.Name, signInLink)

	return m.ToDomain(), nil
}
