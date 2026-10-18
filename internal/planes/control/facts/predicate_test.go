// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package facts_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/catalogue"
	v1cat "github.com/sarvexlab/sarvex/internal/pkg/ucp/catalogue/v1"
	"github.com/sarvexlab/sarvex/internal/planes/control/account"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/facts"
	"github.com/sarvexlab/sarvex/internal/planes/control/store"
)

// ── Fakes ────────────────────────────────────────────────────────────────────

type fakeAccountRepo struct {
	data map[string]*account.AccountModel
}

func newFakeAccountRepo() *fakeAccountRepo {
	return &fakeAccountRepo{data: make(map[string]*account.AccountModel)}
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

func (f *fakeAccountRepo) List(_ context.Context, _ string, _ int) ([]*account.AccountModel, bool, error) {
	return nil, false, nil
}

type fakeMemberRepo struct{}

func (f *fakeMemberRepo) UpsertMember(context.Context, *account.MemberModel) error { return nil }
func (f *fakeMemberRepo) ListByMerchant(context.Context, domain.MerchantID, string, int) ([]*account.MemberModel, bool, error) {
	return nil, false, nil
}

func (f *fakeMemberRepo) GetByEmail(ctx context.Context, merchantID domain.MerchantID, email string) (*account.MemberModel, error) {
	return nil, nil
}

func (f *fakeMemberRepo) GetInvitedByEmail(ctx context.Context, email string) (*account.MemberModel, error) {
	return nil, nil
}

func (f *fakeMemberRepo) GetActiveByEmail(ctx context.Context, email string) (*account.MemberModel, error) {
	return nil, nil
}

// fakeProfileRepo is an in-memory store.ProfileRepository for unit tests.
type fakeProfileRepo struct {
	bp  *domain.StoreBusinessProfile
	cfg *domain.StoreConfiguration
}

func (f *fakeProfileRepo) GetBusinessProfile(_ context.Context, _ domain.StoreID) (*domain.StoreBusinessProfile, error) {
	if f.bp == nil {
		return nil, store.ErrProfileNotFound
	}
	return f.bp, nil
}

func (f *fakeProfileRepo) SaveBusinessProfile(_ context.Context, p *domain.StoreBusinessProfile) error {
	f.bp = p
	return nil
}

func (f *fakeProfileRepo) GetConfiguration(_ context.Context, _ domain.StoreID) (*domain.StoreConfiguration, error) {
	if f.cfg == nil {
		return nil, store.ErrProfileNotFound
	}
	return f.cfg, nil
}

func (f *fakeProfileRepo) SaveConfiguration(_ context.Context, c *domain.StoreConfiguration) error {
	f.cfg = c
	return nil
}

func (f *fakeProfileRepo) GetCapabilities(context.Context) (catalogue.Catalogue, error) {
	return nil, nil
}

// fakeCatalogue is a minimal facts.CatalogueReader stub so tests can vary the
// required-facts set independently of the real catalogue content.
type fakeCatalogue struct {
	required []v1cat.RequiredBusinessFact
}

func (f *fakeCatalogue) RequiredBusinessFacts() []v1cat.RequiredBusinessFact { return f.required }

func defaultRequired() []v1cat.RequiredBusinessFact {
	return []v1cat.RequiredBusinessFact{
		{ID: "m1", Level: v1cat.BusinessFactLevelMerchant, Field: "legal_entity_name"},
		{ID: "s1", Level: v1cat.BusinessFactLevelStore, Field: "fulfillment_regions"},
		{ID: "s2", Level: v1cat.BusinessFactLevelStore, Field: "return_policy_url"},
	}
}

// ── Tests ────────────────────────────────────────────────────────────────────

func TestComplete_AllPresent(t *testing.T) {
	accounts := account.NewAccountService(newFakeAccountRepo(), &fakeMemberRepo{}, account.NopMailer{}, nil, nil)
	_, err := accounts.Update(context.Background(), "merch-1", domain.AccountUpdate{
		BusinessFacts: &domain.BusinessFacts{
			LegalEntityName: "Acme Pty Ltd",
		},
	})
	require.NoError(t, err)

	profileRepo := &fakeProfileRepo{
		bp: &domain.StoreBusinessProfile{
			StoreID: "store-1", MerchantID: "merch-1", ReturnPolicyURL: "https://acme.example.com/returns",
		},
		cfg: &domain.StoreConfiguration{
			StoreID: "store-1", MerchantID: "merch-1", FulfillmentRegions: []string{"AU"},
		},
	}
	profiles := store.NewProfileService(profileRepo)

	pred := facts.New(accounts, profiles, &fakeCatalogue{required: defaultRequired()})
	result, err := pred.Complete(context.Background(), "merch-1", "store-1")
	require.NoError(t, err)
	assert.True(t, result.Complete)
	assert.Empty(t, result.Missing)
}

func TestComplete_SomeMissing(t *testing.T) {
	accounts := account.NewAccountService(newFakeAccountRepo(), &fakeMemberRepo{}, account.NopMailer{}, nil, nil)
	_, err := accounts.Update(context.Background(), "merch-1", domain.AccountUpdate{
		BusinessFacts: &domain.BusinessFacts{
			LegalEntityName: "Acme Pty Ltd",
		},
	})
	require.NoError(t, err)

	// No store profile persisted at all — return_policy_url and
	// fulfillment_regions (both store-level) are deliberately absent.
	profiles := store.NewProfileService(&fakeProfileRepo{})

	pred := facts.New(accounts, profiles, &fakeCatalogue{required: defaultRequired()})
	result, err := pred.Complete(context.Background(), "merch-1", "store-1")
	require.NoError(t, err)
	assert.False(t, result.Complete)
	assert.ElementsMatch(t, []string{"return_policy_url", "fulfillment_regions"}, result.Missing)
}

func TestComplete_NoAccountRecord(t *testing.T) {
	accounts := account.NewAccountService(newFakeAccountRepo(), &fakeMemberRepo{}, account.NopMailer{}, nil, nil)
	profiles := store.NewProfileService(&fakeProfileRepo{})

	pred := facts.New(accounts, profiles, &fakeCatalogue{required: defaultRequired()})
	result, err := pred.Complete(context.Background(), "merch-missing", "store-missing")
	require.NoError(t, err)
	assert.False(t, result.Complete)
	assert.ElementsMatch(t, []string{"legal_entity_name", "return_policy_url", "fulfillment_regions"}, result.Missing)
}

func TestComplete_CatalogueSetChangeAltersResult(t *testing.T) {
	accounts := account.NewAccountService(newFakeAccountRepo(), &fakeMemberRepo{}, account.NopMailer{}, nil, nil)
	_, err := accounts.Update(context.Background(), "merch-1", domain.AccountUpdate{
		BusinessFacts: &domain.BusinessFacts{LegalEntityName: "Acme Pty Ltd"},
	})
	require.NoError(t, err)
	profiles := store.NewProfileService(&fakeProfileRepo{})

	// Required set demands return_policy_url — incomplete.
	pred := facts.New(accounts, profiles, &fakeCatalogue{required: defaultRequired()})
	result, err := pred.Complete(context.Background(), "merch-1", "store-1")
	require.NoError(t, err)
	assert.False(t, result.Complete)

	// Same persisted facts, but the catalogue no longer requires return_policy_url
	// or the store-level fact — no code change, only catalogue content changed.
	narrowed := &fakeCatalogue{required: []v1cat.RequiredBusinessFact{
		{ID: "m1", Level: v1cat.BusinessFactLevelMerchant, Field: "legal_entity_name"},
	}}
	pred2 := facts.New(accounts, profiles, narrowed)
	result2, err := pred2.Complete(context.Background(), "merch-1", "store-1")
	require.NoError(t, err)
	assert.True(t, result2.Complete)
	assert.Empty(t, result2.Missing)
}

func TestComplete_MerchantVsStoreFactRouting(t *testing.T) {
	accounts := account.NewAccountService(newFakeAccountRepo(), &fakeMemberRepo{}, account.NopMailer{}, nil, nil)
	_, err := accounts.Update(context.Background(), "merch-1", domain.AccountUpdate{
		BusinessFacts: &domain.BusinessFacts{LegalEntityName: "Acme Pty Ltd"},
	})
	require.NoError(t, err)

	// Store profile has a return policy but NO fulfillment_regions — even
	// though the merchant-level fact of the same underlying concept exists,
	// it must not satisfy the store-level requirement.
	profiles := store.NewProfileService(&fakeProfileRepo{
		bp:  &domain.StoreBusinessProfile{StoreID: "store-1", MerchantID: "merch-1", ReturnPolicyURL: "https://acme.example.com/returns"},
		cfg: &domain.StoreConfiguration{StoreID: "store-1", MerchantID: "merch-1", FulfillmentRegions: nil},
	})

	pred := facts.New(accounts, profiles, &fakeCatalogue{required: defaultRequired()})
	result, err := pred.Complete(context.Background(), "merch-1", "store-1")
	require.NoError(t, err)
	assert.False(t, result.Complete)
	assert.Equal(t, []string{"fulfillment_regions"}, result.Missing)
}
