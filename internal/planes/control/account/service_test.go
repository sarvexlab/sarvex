// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package account_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/planes/control/account"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// fakeAccountRepo is an in-memory AccountRepository for unit tests.
type fakeAccountRepo struct {
	data map[string]*account.AccountModel
}

func newFakeAccountRepo() *fakeAccountRepo {
	return &fakeAccountRepo{data: make(map[string]*account.AccountModel)}
}

// newFakeAccountRepoWith returns a repo with the given merchant accounts
// already provisioned. Inviting a member requires the account to exist —
// otherwise the invitee could never bind at first sign-in — so any test that
// exercises an invite path has to seed the merchant first.
func newFakeAccountRepoWith(merchantIDs ...string) *fakeAccountRepo {
	r := newFakeAccountRepo()
	for _, id := range merchantIDs {
		r.data[id] = &account.AccountModel{ID: id, Name: "Test Merchant"}
	}
	return r
}

func (f *fakeAccountRepo) Get(_ context.Context, id domain.MerchantID) (*account.AccountModel, error) {
	m, ok := f.data[string(id)]
	if !ok {
		return nil, account.ErrNotFound
	}
	return m, nil
}

func (f *fakeAccountRepo) Upsert(_ context.Context, m *account.AccountModel) error {
	f.data[m.ID] = m
	return nil
}

func (f *fakeAccountRepo) List(_ context.Context, afterID string, limit int) ([]*account.AccountModel, bool, error) {
	var out []*account.AccountModel
	for _, m := range f.data {
		if afterID == "" || m.ID > afterID {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	hasMore := len(out) > limit
	if hasMore {
		out = out[:limit]
	}
	return out, hasMore, nil
}

// fakeMemberRepo is an in-memory MemberRepository for unit tests.
type fakeMemberRepo struct {
	data []*account.MemberModel
}

func (f *fakeMemberRepo) UpsertMember(_ context.Context, m *account.MemberModel) error {
	for i, existing := range f.data {
		if existing.UserID == m.UserID && existing.MerchantID == m.MerchantID {
			f.data[i] = m
			return nil
		}
	}
	f.data = append(f.data, m)
	return nil
}

func (f *fakeMemberRepo) ListByMerchant(_ context.Context, id domain.MerchantID, afterUserID string, limit int) ([]*account.MemberModel, bool, error) {
	var filtered []*account.MemberModel
	for _, m := range f.data {
		if m.MerchantID == string(id) && (afterUserID == "" || m.UserID > afterUserID) {
			filtered = append(filtered, m)
		}
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].UserID < filtered[j].UserID })

	hasMore := len(filtered) > limit
	if hasMore {
		filtered = filtered[:limit]
	}
	return filtered, hasMore, nil
}

func (f *fakeMemberRepo) GetByEmail(_ context.Context, merchantID domain.MerchantID, email string) (*account.MemberModel, error) {
	for _, m := range f.data {
		if m.MerchantID == string(merchantID) && m.Email == email {
			return m, nil
		}
	}
	return nil, account.ErrNotFound
}

func (f *fakeMemberRepo) GetInvitedByEmail(_ context.Context, email string) (*account.MemberModel, error) {
	for _, m := range f.data {
		if m.Email == email && m.Status == string(domain.MemberStatusInvited) {
			return m, nil
		}
	}
	return nil, account.ErrNotFound
}

func (f *fakeMemberRepo) GetActiveByEmail(_ context.Context, email string) (*account.MemberModel, error) {
	for _, m := range f.data {
		if m.Email == email && m.Status == string(domain.MemberStatusActive) {
			return m, nil
		}
	}
	return nil, account.ErrNotFound
}

// fakeSeatLimiter is an in-memory SeatLimiter for unit tests.
type fakeSeatLimiter struct {
	cap int
	err error
}

func (f *fakeSeatLimiter) Get(_ context.Context, _ domain.MerchantID) (domain.Billing, error) {
	if f.err != nil {
		return domain.Billing{}, f.err
	}
	return domain.Billing{Usage: domain.BillingUsage{MemberCap: f.cap}}, nil
}

// fakeTrialProvisioner is an in-memory TrialProvisioner for unit tests.
type fakeTrialProvisioner struct {
	err   error
	calls []domain.MerchantID
}

func (f *fakeTrialProvisioner) ProvisionTrial(_ context.Context, id domain.MerchantID, _ string) error {
	f.calls = append(f.calls, id)
	return f.err
}

func newService(accounts *fakeAccountRepo, members *fakeMemberRepo) account.Service {
	return newServiceWithCap(accounts, members, domain.DefaultMemberCap)
}

func newServiceWithCap(accounts *fakeAccountRepo, members *fakeMemberRepo, cap int) account.Service {
	return account.NewAccountService(accounts, members, nil, &fakeSeatLimiter{cap: cap}, &fakeTrialProvisioner{})
}

// ── Get ──────────────────────────────────────────────────────────────────────

func TestAccountService_Get_NotFound(t *testing.T) {
	svc := newService(newFakeAccountRepo(), &fakeMemberRepo{})
	_, err := svc.Get(context.Background(), "merch-1")
	assert.ErrorIs(t, err, account.ErrNotFound)
}

func TestAccountService_Get_Found(t *testing.T) {
	repo := newFakeAccountRepo()
	repo.data["merch-1"] = &account.AccountModel{
		ID:       "merch-1",
		Name:     "Acme Store",
		Timezone: "Australia/Sydney",
		BusinessFacts: account.BusinessFactsModel{
			LegalEntityName: "Acme Pty Ltd",
			SupportEmail:    "support@acme.com",
		},
		CreatedAt: time.Now().UTC(),
	}

	svc := newService(repo, &fakeMemberRepo{})
	acc, err := svc.Get(context.Background(), "merch-1")
	require.NoError(t, err)
	assert.Equal(t, domain.MerchantID("merch-1"), acc.MerchantID)
	assert.Equal(t, "Acme Store", acc.Name)
	assert.Equal(t, "Australia/Sydney", acc.Timezone)
	assert.Equal(t, "Acme Pty Ltd", acc.BusinessFacts.LegalEntityName)
}

// ── Update ───────────────────────────────────────────────────────────────────

func TestAccountService_Update_CreatesWhenAbsent(t *testing.T) {
	repo := newFakeAccountRepo()
	svc := newService(repo, &fakeMemberRepo{})

	name := "New Store"
	acc, err := svc.Update(context.Background(), "merch-new", domain.AccountUpdate{Name: &name})
	require.NoError(t, err)
	assert.Equal(t, "New Store", acc.Name)
	assert.Equal(t, domain.MerchantID("merch-new"), acc.MerchantID)
	assert.NotZero(t, acc.CreatedAt)
	_, stored := repo.data["merch-new"]
	assert.True(t, stored, "record should be persisted")
}

func TestAccountService_Update_MergesFields(t *testing.T) {
	repo := newFakeAccountRepo()
	repo.data["merch-1"] = &account.AccountModel{
		ID:        "merch-1",
		Name:      "Old Name",
		Timezone:  "UTC",
		CreatedAt: time.Now().UTC(),
	}
	svc := newService(repo, &fakeMemberRepo{})

	tz := "Australia/Melbourne"
	acc, err := svc.Update(context.Background(), "merch-1", domain.AccountUpdate{Timezone: &tz})
	require.NoError(t, err)
	assert.Equal(t, "Old Name", acc.Name, "name should be unchanged")
	assert.Equal(t, "Australia/Melbourne", acc.Timezone)
}

func TestAccountService_Update_BusinessFacts(t *testing.T) {
	repo := newFakeAccountRepo()
	svc := newService(repo, &fakeMemberRepo{})

	facts := &domain.BusinessFacts{
		LegalEntityName:    "Sarvex Pty Ltd",
		SupportEmail:       "hi@sarvex.example.com",
		FulfillmentRegions: []string{"AU", "NZ"},
	}
	acc, err := svc.Update(context.Background(), "merch-1", domain.AccountUpdate{BusinessFacts: facts})
	require.NoError(t, err)
	assert.Equal(t, "Sarvex Pty Ltd", acc.BusinessFacts.LegalEntityName)
	assert.Equal(t, []string{"AU", "NZ"}, acc.BusinessFacts.FulfillmentRegions)
}

// ── ListMembers ───────────────────────────────────────────────────────────────

func TestAccountService_ListMembers_Empty(t *testing.T) {
	svc := newService(newFakeAccountRepo(), &fakeMemberRepo{})
	result, err := svc.ListMembers(context.Background(), "merch-1", "", 20)
	require.NoError(t, err)
	assert.Empty(t, result.Data)
	assert.False(t, result.Page.HasMore)
	assert.Nil(t, result.Page.Cursor)
}

func TestAccountService_ListMembers_ReturnsMerchantsMembers(t *testing.T) {
	members := &fakeMemberRepo{data: []*account.MemberModel{
		{UserID: "u-1", MerchantID: "merch-1", Email: "a@example.com", Name: "Alice", Roles: []string{"owner"}, Status: "active", CountryCode: "AU"},
		{UserID: "u-2", MerchantID: "merch-1", Email: "b@example.com", Name: "Bob", Roles: []string{"staff"}, Status: "active", CountryCode: "AU"},
		{UserID: "u-3", MerchantID: "merch-other", Email: "c@example.com", Name: "Carol", Roles: []string{"owner"}, Status: "active", CountryCode: "US"},
	}}
	svc := newService(newFakeAccountRepo(), members)

	result, err := svc.ListMembers(context.Background(), "merch-1", "", 20)
	require.NoError(t, err)
	assert.Len(t, result.Data, 2)
	assert.Equal(t, "u-1", result.Data[0].UserID)
	assert.Equal(t, "u-2", result.Data[1].UserID)
}

func TestAccountService_ListMembers_Pagination(t *testing.T) {
	members := &fakeMemberRepo{data: []*account.MemberModel{
		{UserID: "u-1", MerchantID: "merch-1", Email: "a@example.com", Roles: []string{"owner"}, Status: "active"},
		{UserID: "u-2", MerchantID: "merch-1", Email: "b@example.com", Roles: []string{"staff"}, Status: "active"},
		{UserID: "u-3", MerchantID: "merch-1", Email: "c@example.com", Roles: []string{"staff"}, Status: "active"},
	}}
	svc := newService(newFakeAccountRepo(), members)

	// First page
	page1, err := svc.ListMembers(context.Background(), "merch-1", "", 2)
	require.NoError(t, err)
	assert.Len(t, page1.Data, 2)
	assert.True(t, page1.Page.HasMore)
	require.NotNil(t, page1.Page.Cursor)

	// Second page
	page2, err := svc.ListMembers(context.Background(), "merch-1", *page1.Page.Cursor, 2)
	require.NoError(t, err)
	assert.Len(t, page2.Data, 1)
	assert.False(t, page2.Page.HasMore)
}

func TestAccountService_ListMembers_LimitClamped(t *testing.T) {
	members := &fakeMemberRepo{}
	svc := newService(newFakeAccountRepo(), members)

	// limit=0 should be coerced to 20 (no panic)
	_, err := svc.ListMembers(context.Background(), "merch-1", "", 0)
	require.NoError(t, err)

	// limit>100 should be coerced to 20 (no panic)
	_, err = svc.ListMembers(context.Background(), "merch-1", "", 200)
	require.NoError(t, err)
}

func TestAccountService_ListMembers_MemberFields(t *testing.T) {
	members := &fakeMemberRepo{data: []*account.MemberModel{
		{
			UserID:      "u-1",
			MerchantID:  "merch-1",
			Email:       "alice@example.com",
			Name:        "Alice Smith",
			Roles:       []string{"owner", "staff"},
			Status:      "active",
			CountryCode: "AU",
		},
	}}
	svc := newService(newFakeAccountRepo(), members)

	result, err := svc.ListMembers(context.Background(), "merch-1", "", 20)
	require.NoError(t, err)
	require.Len(t, result.Data, 1)
	m := result.Data[0]
	assert.Equal(t, "u-1", m.UserID)
	assert.Equal(t, "alice@example.com", m.Email)
	assert.Equal(t, "Alice Smith", m.Name)
	assert.Equal(t, []domain.MemberRole{"owner", "staff"}, m.Roles)
	assert.Equal(t, domain.MemberStatusActive, m.Status)
	assert.Equal(t, "AU", m.CountryCode)
}

// ── InviteMember (operator-provisioning invite) ─────────────────────────────────

func TestAccountService_InviteMember_RequiresEmail(t *testing.T) {
	svc := newService(newFakeAccountRepo(), &fakeMemberRepo{})
	_, err := svc.InviteMember(context.Background(), "merch-1", "", domain.MemberRoleStaff)
	assert.Error(t, err)
}

func TestAccountService_InviteMember_DefaultsRoleToOwner(t *testing.T) {
	members := &fakeMemberRepo{}
	svc := newService(newFakeAccountRepoWith("merch-1"), members)

	m, err := svc.InviteMember(context.Background(), "merch-1", "owner@example.com", "")
	require.NoError(t, err)
	require.Len(t, m.Roles, 1)
	assert.Equal(t, domain.MemberRoleOwner, m.Roles[0])
	assert.Equal(t, domain.MemberStatusInvited, m.Status)
}

func TestAccountService_InviteMember_RejectsAlreadyActive(t *testing.T) {
	members := &fakeMemberRepo{data: []*account.MemberModel{
		{UserID: "u-1", MerchantID: "merch-1", Email: "active@example.com", Roles: []string{"staff"}, Status: "active"},
	}}
	svc := newService(newFakeAccountRepoWith("merch-1"), members)

	_, err := svc.InviteMember(context.Background(), "merch-1", "active@example.com", domain.MemberRoleStaff)
	assert.ErrorIs(t, err, account.ErrMemberAlreadyActive)
}

func TestAccountService_InviteMember_ReinvitesPendingEmail(t *testing.T) {
	// Re-inviting an email that's still "invited" (not yet active) is not an error —
	// only an active member blocks re-invitation.
	members := &fakeMemberRepo{data: []*account.MemberModel{
		{UserID: "pending:invited@example.com", MerchantID: "merch-1", Email: "invited@example.com", Roles: []string{"staff"}, Status: "invited"},
	}}
	svc := newService(newFakeAccountRepoWith("merch-1"), members)

	m, err := svc.InviteMember(context.Background(), "merch-1", "invited@example.com", domain.MemberRoleStaff)
	require.NoError(t, err)
	assert.Equal(t, domain.MemberStatusInvited, m.Status)
}

func TestAccountService_InviteMember_NotSubjectToSeatCap(t *testing.T) {
	// Operator-driven provisioning (e.g. the first admin invite at onboarding)
	// must not be blocked by the self-serve seat cap.
	members := &fakeMemberRepo{}
	svc := newServiceWithCap(newFakeAccountRepoWith("merch-1"), members, 0)

	_, err := svc.InviteMember(context.Background(), "merch-1", "admin@example.com", domain.MemberRoleOwner)
	assert.NoError(t, err)
}

// ── InviteTeamMember (owner self-serve invite) ──────────────────────────────────

func TestAccountService_InviteTeamMember_RequiresEmail(t *testing.T) {
	svc := newService(newFakeAccountRepo(), &fakeMemberRepo{})
	_, err := svc.InviteTeamMember(context.Background(), "merch-1", "")
	assert.Error(t, err)
}

func TestAccountService_InviteTeamMember_InvitesAsStaff(t *testing.T) {
	members := &fakeMemberRepo{}
	svc := newServiceWithCap(newFakeAccountRepoWith("merch-1"), members, domain.DefaultMemberCap)

	m, err := svc.InviteTeamMember(context.Background(), "merch-1", "new@example.com")
	require.NoError(t, err)
	require.Len(t, m.Roles, 1)
	assert.Equal(t, domain.MemberRoleStaff, m.Roles[0])
	assert.Equal(t, domain.MemberStatusInvited, m.Status)
}

func TestAccountService_InviteTeamMember_RejectsAlreadyActive(t *testing.T) {
	members := &fakeMemberRepo{data: []*account.MemberModel{
		{UserID: "u-1", MerchantID: "merch-1", Email: "active@example.com", Roles: []string{"staff"}, Status: "active"},
	}}
	svc := newServiceWithCap(newFakeAccountRepoWith("merch-1"), members, domain.DefaultMemberCap)

	_, err := svc.InviteTeamMember(context.Background(), "merch-1", "active@example.com")
	assert.ErrorIs(t, err, account.ErrMemberAlreadyActive)
}

func TestAccountService_InviteTeamMember_UnderCapSucceeds(t *testing.T) {
	members := &fakeMemberRepo{data: []*account.MemberModel{
		{UserID: "u-1", MerchantID: "merch-1", Email: "owner@example.com", Roles: []string{"owner"}, Status: "active"},
	}}
	svc := newServiceWithCap(newFakeAccountRepoWith("merch-1"), members, 2)

	_, err := svc.InviteTeamMember(context.Background(), "merch-1", "new@example.com")
	assert.NoError(t, err)
}

func TestAccountService_InviteTeamMember_AtCapIsRejected(t *testing.T) {
	members := &fakeMemberRepo{data: []*account.MemberModel{
		{UserID: "u-1", MerchantID: "merch-1", Email: "owner@example.com", Roles: []string{"owner"}, Status: "active"},
		{UserID: "u-2", MerchantID: "merch-1", Email: "staff@example.com", Roles: []string{"staff"}, Status: "active"},
	}}
	svc := newServiceWithCap(newFakeAccountRepo(), members, 2)

	_, err := svc.InviteTeamMember(context.Background(), "merch-1", "new@example.com")
	assert.ErrorIs(t, err, account.ErrMemberLimitReached)
}

func TestAccountService_InviteTeamMember_PendingInvitesCountTowardCap(t *testing.T) {
	// A pending (not yet accepted) invite still occupies a seat, so it can't be
	// used to exceed the cap by inviting many people who never sign in.
	members := &fakeMemberRepo{data: []*account.MemberModel{
		{UserID: "u-1", MerchantID: "merch-1", Email: "owner@example.com", Roles: []string{"owner"}, Status: "active"},
		{UserID: "pending:staff@example.com", MerchantID: "merch-1", Email: "staff@example.com", Roles: []string{"staff"}, Status: "invited"},
	}}
	svc := newServiceWithCap(newFakeAccountRepo(), members, 2)

	_, err := svc.InviteTeamMember(context.Background(), "merch-1", "new@example.com")
	assert.ErrorIs(t, err, account.ErrMemberLimitReached)
}

func TestAccountService_InviteTeamMember_NoBillingRecordDefaultsCap(t *testing.T) {
	// A merchant with no subscription yet still gets the default seat cap, not zero.
	members := &fakeMemberRepo{}
	svc := account.NewAccountService(newFakeAccountRepoWith("merch-1"), members, nil, &fakeSeatLimiter{cap: domain.DefaultMemberCap}, &fakeTrialProvisioner{})

	_, err := svc.InviteTeamMember(context.Background(), "merch-1", "new@example.com")
	assert.NoError(t, err)
}

func TestAccountService_InviteTeamMember_SeatLimiterErrorPropagates(t *testing.T) {
	members := &fakeMemberRepo{}
	svc := account.NewAccountService(newFakeAccountRepo(), members, nil, &fakeSeatLimiter{err: assert.AnError}, &fakeTrialProvisioner{})

	_, err := svc.InviteTeamMember(context.Background(), "merch-1", "new@example.com")
	assert.Error(t, err)
}

// ── Create ───────────────────────────────────────────────────────────────────

func TestAccountService_Create_ProvisionsTrial(t *testing.T) {
	provisioner := &fakeTrialProvisioner{}
	accounts := newFakeAccountRepo()
	svc := account.NewAccountService(accounts, &fakeMemberRepo{}, nil, &fakeSeatLimiter{}, provisioner)

	acc, err := svc.Create(context.Background(), "Acme Store", nil, "owner@example.com")
	require.NoError(t, err)
	require.Len(t, provisioner.calls, 1)
	assert.Equal(t, acc.MerchantID, provisioner.calls[0])
}

func TestAccountService_Create_TrialProvisioningErrorFailsCreate(t *testing.T) {
	provisioner := &fakeTrialProvisioner{err: assert.AnError}
	svc := account.NewAccountService(newFakeAccountRepo(), &fakeMemberRepo{}, nil, &fakeSeatLimiter{}, provisioner)

	_, err := svc.Create(context.Background(), "Acme Store", nil, "owner@example.com")
	assert.Error(t, err)
}

// ── BindInvitedMember ────────────────────────────────────────────────────────

func TestAccountService_BindInvitedMember_ActivatesOnFirstSignIn(t *testing.T) {
	members := &fakeMemberRepo{data: []*account.MemberModel{
		{UserID: "pending:new@example.com", MerchantID: "merch-1", Email: "new@example.com", Roles: []string{"staff"}, Status: "invited"},
	}}
	svc := newService(newFakeAccountRepoWith("merch-1"), members)

	m, merchantID, err := svc.BindInvitedMember(context.Background(), "new@example.com", "idp-sub-1")
	require.NoError(t, err)
	assert.Equal(t, domain.MemberStatusActive, m.Status)
	assert.Equal(t, "idp-sub-1", m.UserID)
	assert.Equal(t, domain.MerchantID("merch-1"), merchantID)
}

// TestAccountService_BindInvitedMember_IsIdempotent covers the retry that the
// sign-in path actually makes. The first bind flips the member to active, so a
// second call finds no *invited* member — it must still resolve to the same
// member rather than failing, or a bound user is locked out on their next
// request.
func TestAccountService_BindInvitedMember_IsIdempotent(t *testing.T) {
	members := &fakeMemberRepo{data: []*account.MemberModel{
		{UserID: "pending:new@example.com", MerchantID: "merch-1", Email: "new@example.com", Roles: []string{"staff"}, Status: "invited"},
	}}
	svc := newService(newFakeAccountRepoWith("merch-1"), members)

	_, _, err := svc.BindInvitedMember(context.Background(), "new@example.com", "idp-sub-1")
	require.NoError(t, err)

	m, merchantID, err := svc.BindInvitedMember(context.Background(), "new@example.com", "idp-sub-1")
	require.NoError(t, err)
	assert.Equal(t, domain.MemberStatusActive, m.Status)
	assert.Equal(t, "idp-sub-1", m.UserID)
	assert.Equal(t, domain.MerchantID("merch-1"), merchantID)
}

// TestAccountService_BindInvitedMember_RejectsDifferentSubject is the security
// half of the idempotent rebind: an email is not proof of identity on this
// path, so a second subject presenting the same address must not inherit the
// bound member's merchant scope.
func TestAccountService_BindInvitedMember_RejectsDifferentSubject(t *testing.T) {
	members := &fakeMemberRepo{data: []*account.MemberModel{
		{UserID: "idp-sub-1", MerchantID: "merch-1", Email: "new@example.com", Roles: []string{"staff"}, Status: "active"},
	}}
	svc := newService(newFakeAccountRepoWith("merch-1"), members)

	_, _, err := svc.BindInvitedMember(context.Background(), "new@example.com", "attacker-sub")
	assert.ErrorIs(t, err, account.ErrNotFound)
}

func TestAccountService_BindInvitedMember_UnknownEmail(t *testing.T) {
	svc := newService(newFakeAccountRepoWith("merch-1"), &fakeMemberRepo{})

	_, _, err := svc.BindInvitedMember(context.Background(), "nobody@example.com", "idp-sub-1")
	assert.ErrorIs(t, err, account.ErrNotFound)
}
