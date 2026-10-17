// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package billing_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/billing"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// ── fakes ────────────────────────────────────────────────────────────────────

type fakeBillingRepo struct {
	data map[string]*billing.BillingRecordModel
	err  error
}

func newFakeBillingRepo() *fakeBillingRepo {
	return &fakeBillingRepo{data: make(map[string]*billing.BillingRecordModel)}
}

func (f *fakeBillingRepo) Get(_ context.Context, id domain.MerchantID) (*billing.BillingRecordModel, error) {
	if f.err != nil {
		return nil, f.err
	}
	m, ok := f.data[string(id)]
	if !ok {
		return nil, billing.ErrNotFound
	}
	return m, nil
}

func (f *fakeBillingRepo) GetByStripeCustomerID(_ context.Context, customerID string) (*billing.BillingRecordModel, error) {
	if f.err != nil {
		return nil, f.err
	}
	for _, m := range f.data {
		if m.StripeCustomerID == customerID {
			return m, nil
		}
	}
	return nil, billing.ErrNotFound
}

func (f *fakeBillingRepo) Upsert(_ context.Context, m *billing.BillingRecordModel) error {
	if f.err != nil {
		return f.err
	}
	f.data[m.MerchantID] = m
	return nil
}

type fakePrefsRepo struct {
	data map[string]*billing.NotificationPrefsModel
	err  error
}

func newFakePrefsRepo() *fakePrefsRepo {
	return &fakePrefsRepo{data: make(map[string]*billing.NotificationPrefsModel)}
}

func (f *fakePrefsRepo) Get(_ context.Context, id domain.MerchantID) (*billing.NotificationPrefsModel, error) {
	if f.err != nil {
		return nil, f.err
	}
	m, ok := f.data[string(id)]
	if !ok {
		return &billing.NotificationPrefsModel{MerchantID: string(id)}, nil
	}
	return m, nil
}

func (f *fakePrefsRepo) Upsert(_ context.Context, m *billing.NotificationPrefsModel) error {
	if f.err != nil {
		return f.err
	}
	f.data[m.MerchantID] = m
	return nil
}

type fakeStripe struct {
	portalURL      string
	customerID     string
	subscriptionID string
	productID      string
	configID       string
	err            error

	lastPortalCustomerID      string
	lastPortalConfigurationID string
	lastPortalConfig          billing.PortalConfigParams
}

func (f *fakeStripe) CreatePortalSession(customerID, _, configurationID string) (string, error) {
	f.lastPortalCustomerID = customerID
	f.lastPortalConfigurationID = configurationID
	return f.portalURL, f.err
}

func (f *fakeStripe) CreateCustomer(_, _ string) (string, error) {
	return f.customerID, f.err
}

func (f *fakeStripe) CreateTrialSubscription(_, _ string, _ int) (string, error) {
	return f.subscriptionID, f.err
}

func (f *fakeStripe) GetPriceProductID(_ string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.productID, nil
}

func (f *fakeStripe) EnsurePortalConfiguration(existingID string, params billing.PortalConfigParams) (string, error) {
	f.lastPortalConfig = params
	if f.err != nil {
		return "", f.err
	}
	if existingID != "" {
		return existingID, nil
	}
	return f.configID, nil
}

// fakeWebhookEventRepo records which event IDs have been marked processed.
type fakeWebhookEventRepo struct {
	processed map[string]bool
	err       error
}

func newFakeWebhookEventRepo() *fakeWebhookEventRepo {
	return &fakeWebhookEventRepo{processed: make(map[string]bool)}
}

func (f *fakeWebhookEventRepo) IsProcessed(_ context.Context, eventID string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.processed[eventID], nil
}

func (f *fakeWebhookEventRepo) MarkProcessed(_ context.Context, eventID string) error {
	if f.err != nil {
		return f.err
	}
	f.processed[eventID] = true
	return nil
}

func testBillingConfig() billing.Config {
	return billing.Config{
		PlanPriceIDs: map[domain.Plan]string{
			domain.PlanSelfServe: "price_self_serve",
			domain.PlanConcierge: "price_concierge",
			domain.PlanScale:     "price_scale",
		},
		PortalReturnURL: "https://app.example.com/billing",
		WebhookSecret:   "whsec_test",
	}
}

func newService(billingRepo *fakeBillingRepo, prefsRepo *fakePrefsRepo, stripe *fakeStripe) billing.Service {
	return billing.NewBillingService(billingRepo, prefsRepo, newFakeWebhookEventRepo(), stripe, testBillingConfig())
}

// ── Get ──────────────────────────────────────────────────────────────────────

func TestBillingService_Get_NoRecord_ReturnsEmptyBilling(t *testing.T) {
	svc := newService(newFakeBillingRepo(), newFakePrefsRepo(), &fakeStripe{})
	b, err := svc.Get(context.Background(), "merch-1")
	require.NoError(t, err)
	// The team-member seat cap still applies even with no subscription yet —
	// only Usage.MemberCap is non-zero, everything else stays zero-value.
	assert.Equal(t, domain.Billing{Usage: domain.BillingUsage{MemberCap: domain.DefaultMemberCap}}, b)
}

func TestBillingService_Get_RecordExists_ReturnsDomain(t *testing.T) {
	repo := newFakeBillingRepo()
	repo.data["merch-1"] = &billing.BillingRecordModel{
		MerchantID: "merch-1",
		Plan:       string(domain.PlanSelfServe),
		Status:     string(domain.BillingActive),
		SKUBand:    "1000",
		SKUCap:     1000,
		SKUCount:   42,
	}
	svc := newService(repo, newFakePrefsRepo(), &fakeStripe{})
	b, err := svc.Get(context.Background(), "merch-1")
	require.NoError(t, err)
	assert.Equal(t, domain.PlanSelfServe, b.Plan)
	assert.Equal(t, domain.BillingActive, b.Status)
	assert.Equal(t, "1000", b.SKUBand)
	assert.Equal(t, 1000, b.Usage.SKUCap)
	assert.Equal(t, 42, b.Usage.SKUCount)
	assert.Equal(t, domain.DefaultMemberCap, b.Usage.MemberCap, "unset member_cap on an existing record defaults, same as sku_cap would")
}

func TestBillingService_Get_RecordExists_MemberCapSet_ReturnsIt(t *testing.T) {
	repo := newFakeBillingRepo()
	repo.data["merch-1"] = &billing.BillingRecordModel{
		MerchantID: "merch-1",
		Plan:       string(domain.PlanConcierge),
		MemberCap:  10,
	}
	svc := newService(repo, newFakePrefsRepo(), &fakeStripe{})
	b, err := svc.Get(context.Background(), "merch-1")
	require.NoError(t, err)
	assert.Equal(t, 10, b.Usage.MemberCap)
}

func TestBillingService_Get_RepoError_ReturnsError(t *testing.T) {
	repo := newFakeBillingRepo()
	repo.err = errors.New("db error")
	svc := newService(repo, newFakePrefsRepo(), &fakeStripe{})
	_, err := svc.Get(context.Background(), "merch-1")
	assert.Error(t, err)
}

// ── CreatePortalLink ──────────────────────────────────────────────────────────

func TestBillingService_CreatePortalLink_NoRecord_ReturnsErrNoStripeCustomer(t *testing.T) {
	svc := newService(newFakeBillingRepo(), newFakePrefsRepo(), &fakeStripe{})
	_, err := svc.CreatePortalLink(context.Background(), "merch-1")
	assert.ErrorIs(t, err, billing.ErrNoStripeCustomer)
}

func TestBillingService_CreatePortalLink_NoCustomerID_ReturnsErrNoStripeCustomer(t *testing.T) {
	repo := newFakeBillingRepo()
	repo.data["merch-1"] = &billing.BillingRecordModel{MerchantID: "merch-1"} // no customer ID
	svc := newService(repo, newFakePrefsRepo(), &fakeStripe{})
	_, err := svc.CreatePortalLink(context.Background(), "merch-1")
	assert.ErrorIs(t, err, billing.ErrNoStripeCustomer)
}

func TestBillingService_CreatePortalLink_OK(t *testing.T) {
	repo := newFakeBillingRepo()
	repo.data["merch-1"] = &billing.BillingRecordModel{
		MerchantID:       "merch-1",
		StripeCustomerID: "cus_abc",
	}
	stripe := &fakeStripe{portalURL: "https://billing.stripe.com/portal/abc"}
	svc := newService(repo, newFakePrefsRepo(), stripe)

	link, err := svc.CreatePortalLink(context.Background(), "merch-1")
	require.NoError(t, err)
	assert.Equal(t, "https://billing.stripe.com/portal/abc", link.URL)
	assert.Equal(t, "cus_abc", stripe.lastPortalCustomerID)
}

func TestBillingService_CreatePortalLink_UsesEnsuredConfigurationID(t *testing.T) {
	repo := newFakeBillingRepo()
	repo.data["merch-1"] = &billing.BillingRecordModel{
		MerchantID:       "merch-1",
		StripeCustomerID: "cus_abc",
	}
	stripe := &fakeStripe{configID: "bpc_new", productID: "prod_x"}
	svc := newService(repo, newFakePrefsRepo(), stripe)

	_, err := svc.EnsurePortalConfiguration(context.Background())
	require.NoError(t, err)

	_, err = svc.CreatePortalLink(context.Background(), "merch-1")
	require.NoError(t, err)
	assert.Equal(t, "bpc_new", stripe.lastPortalConfigurationID)
}

func TestBillingService_CreatePortalLink_StripeError_ReturnsError(t *testing.T) {
	repo := newFakeBillingRepo()
	repo.data["merch-1"] = &billing.BillingRecordModel{
		MerchantID:       "merch-1",
		StripeCustomerID: "cus_abc",
	}
	stripe := &fakeStripe{err: errors.New("stripe error")}
	svc := newService(repo, newFakePrefsRepo(), stripe)
	_, err := svc.CreatePortalLink(context.Background(), "merch-1")
	assert.Error(t, err)
}

// ── GetNotificationPreferences ────────────────────────────────────────────────

func TestBillingService_GetNotificationPreferences_NoRecord_ReturnsDefaults(t *testing.T) {
	svc := newService(newFakeBillingRepo(), newFakePrefsRepo(), &fakeStripe{})
	prefs, err := svc.GetNotificationPreferences(context.Background(), "merch-1")
	require.NoError(t, err)
	assert.False(t, prefs.EmailEnabled)
	assert.False(t, prefs.WeeklyDigest)
	assert.Nil(t, prefs.AlertEmail)
}

func TestBillingService_GetNotificationPreferences_RecordExists(t *testing.T) {
	prefsRepo := newFakePrefsRepo()
	email := "alerts@acme.com"
	prefsRepo.data["merch-1"] = &billing.NotificationPrefsModel{
		MerchantID:   "merch-1",
		EmailEnabled: true,
		WeeklyDigest: true,
		AlertEmail:   &email,
	}
	svc := newService(newFakeBillingRepo(), prefsRepo, &fakeStripe{})

	prefs, err := svc.GetNotificationPreferences(context.Background(), "merch-1")
	require.NoError(t, err)
	assert.True(t, prefs.EmailEnabled)
	assert.True(t, prefs.WeeklyDigest)
	require.NotNil(t, prefs.AlertEmail)
	assert.Equal(t, "alerts@acme.com", *prefs.AlertEmail)
}

// ── SetNotificationPreferences ────────────────────────────────────────────────

func TestBillingService_SetNotificationPreferences_PersistsAndReturns(t *testing.T) {
	prefsRepo := newFakePrefsRepo()
	svc := newService(newFakeBillingRepo(), prefsRepo, &fakeStripe{})

	email := "alerts@acme.com"
	input := domain.NotificationPreferences{
		EmailEnabled: true,
		WeeklyDigest: false,
		AlertEmail:   &email,
	}
	saved, err := svc.SetNotificationPreferences(context.Background(), "merch-1", input)
	require.NoError(t, err)
	assert.True(t, saved.EmailEnabled)
	assert.False(t, saved.WeeklyDigest)
	require.NotNil(t, saved.AlertEmail)
	assert.Equal(t, "alerts@acme.com", *saved.AlertEmail)

	_, stored := prefsRepo.data["merch-1"]
	assert.True(t, stored, "record should be persisted")
}

func TestBillingService_SetNotificationPreferences_RepoError(t *testing.T) {
	prefsRepo := newFakePrefsRepo()
	prefsRepo.err = errors.New("db error")
	svc := newService(newFakeBillingRepo(), prefsRepo, &fakeStripe{})
	_, err := svc.SetNotificationPreferences(context.Background(), "merch-1", domain.NotificationPreferences{})
	assert.Error(t, err)
}

// ── ProvisionTrial ───────────────────────────────────────────────────────────

func TestBillingService_ProvisionTrial_CreatesCustomerSubscriptionAndRecord(t *testing.T) {
	repo := newFakeBillingRepo()
	stripe := &fakeStripe{customerID: "cus_new", subscriptionID: "sub_new"}
	svc := newService(repo, newFakePrefsRepo(), stripe)

	err := svc.ProvisionTrial(context.Background(), "merch-1", "owner@example.com")
	require.NoError(t, err)

	rec, ok := repo.data["merch-1"]
	require.True(t, ok, "billing record should be persisted")
	assert.Equal(t, "cus_new", rec.StripeCustomerID)
	assert.Equal(t, "sub_new", rec.StripeSubscriptionID)
	assert.Equal(t, string(domain.PlanSelfServe), rec.Plan)
	assert.Equal(t, string(domain.BillingActive), rec.Status)
}

func TestBillingService_ProvisionTrial_CreateCustomerError_ReturnsError(t *testing.T) {
	svc := newService(newFakeBillingRepo(), newFakePrefsRepo(), &fakeStripe{err: errors.New("stripe down")})
	err := svc.ProvisionTrial(context.Background(), "merch-1", "owner@example.com")
	assert.Error(t, err)
}

func TestBillingService_ProvisionTrial_UnknownEntryPlanPrice_ReturnsError(t *testing.T) {
	repo := newFakeBillingRepo()
	stripe := &fakeStripe{}
	svc := billing.NewBillingService(repo, newFakePrefsRepo(), newFakeWebhookEventRepo(), stripe, billing.Config{
		PlanPriceIDs: map[domain.Plan]string{}, // no self_serve price configured
	})
	err := svc.ProvisionTrial(context.Background(), "merch-1", "owner@example.com")
	assert.ErrorIs(t, err, billing.ErrUnknownPlan)
}

// ── EnsurePortalConfiguration ────────────────────────────────────────────────

func TestBillingService_EnsurePortalConfiguration_OffersOnlyConciergeAndScale(t *testing.T) {
	stripe := &fakeStripe{configID: "bpc_new", productID: "prod_generic"}
	svc := newService(newFakeBillingRepo(), newFakePrefsRepo(), stripe)

	id, err := svc.EnsurePortalConfiguration(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "bpc_new", id)

	products := stripe.lastPortalConfig.SwitchableProducts
	require.Len(t, products, 2, "self_serve must never be offered as a switch target")
	priceIDs := []string{products[0].PriceID, products[1].PriceID}
	assert.ElementsMatch(t, []string{"price_concierge", "price_scale"}, priceIDs)
}

func TestBillingService_EnsurePortalConfiguration_ReconcilesExistingID(t *testing.T) {
	stripe := &fakeStripe{productID: "prod_generic"}
	cfg := testBillingConfig()
	cfg.PortalConfigurationID = "bpc_existing"
	svc := billing.NewBillingService(newFakeBillingRepo(), newFakePrefsRepo(), newFakeWebhookEventRepo(), stripe, cfg)

	id, err := svc.EnsurePortalConfiguration(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "bpc_existing", id, "an existing configuration id is updated in place, not replaced")
}

func TestBillingService_EnsurePortalConfiguration_GetPriceProductIDError_ReturnsError(t *testing.T) {
	stripe := &fakeStripe{err: errors.New("stripe unavailable")}
	svc := newService(newFakeBillingRepo(), newFakePrefsRepo(), stripe)

	_, err := svc.EnsurePortalConfiguration(context.Background())
	assert.Error(t, err)
}
