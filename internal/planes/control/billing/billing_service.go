// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package billing

import (
	"context"
	"fmt"
	"time"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// Config holds Stripe price IDs and redirect URLs for the billing service.
type Config struct {
	// PlanPriceIDs maps each Plan to its Stripe price ID.
	PlanPriceIDs map[domain.Plan]string
	// PortalReturnURL is where Stripe redirects after the billing portal is closed.
	PortalReturnURL string
	// WebhookSecret verifies the Stripe-Signature header on incoming webhook requests.
	WebhookSecret string
	// TrialDays is the length of the no-payment-method trial granted on the
	// entry-level (self_serve) plan when a merchant account is created.
	TrialDays int
	// PortalConfigurationID is the Stripe Billing Portal Configuration to
	// reconcile on EnsurePortalConfiguration and to scope every portal
	// session to. Empty on first boot in a new environment — EnsurePortalConfiguration
	// creates one and returns its ID for an operator to persist here.
	PortalConfigurationID string
}

// BillingService is the production implementation of Service.
type BillingService struct {
	billing       BillingRepository
	prefs         NotificationPrefsRepository
	webhookEvents WebhookEventRepository
	stripe        StripeClient
	cfg           Config
	// planByPriceID is the inverse of cfg.PlanPriceIDs, built once — lets
	// the webhook handler recover a Plan from a Stripe subscription's price ID.
	planByPriceID map[string]domain.Plan
	// portalConfigurationID is the configuration every portal session is
	// scoped to. Starts at cfg.PortalConfigurationID; EnsurePortalConfiguration
	// updates it in place once the configuration is confirmed to exist.
	portalConfigurationID string
}

// defaultTrialDays applies when Config.TrialDays is unset, so an unconfigured
// environment still grants a trial rather than none.
const defaultTrialDays = 30

// upgradePlans are the plans a merchant can self-serve switch to via the
// Billing Portal Configuration (see EnsurePortalConfiguration) — every
// commercial plan except domain.PlanSelfServe, the entry-level plan every
// merchant starts on. self_serve is deliberately never listed, so a merchant
// can never self-serve switch back down to it once upgraded.
var upgradePlans = []domain.Plan{domain.PlanConcierge, domain.PlanScale}

// NewBillingService constructs a Service backed by MongoDB and Stripe.
func NewBillingService(billing BillingRepository, prefs NotificationPrefsRepository, webhookEvents WebhookEventRepository, stripe StripeClient, cfg Config) Service {
	if cfg.TrialDays <= 0 {
		cfg.TrialDays = defaultTrialDays
	}
	planByPriceID := make(map[string]domain.Plan, len(cfg.PlanPriceIDs))
	for plan, priceID := range cfg.PlanPriceIDs {
		if priceID != "" {
			planByPriceID[priceID] = plan
		}
	}
	return &BillingService{
		billing:               billing,
		prefs:                 prefs,
		webhookEvents:         webhookEvents,
		stripe:                stripe,
		cfg:                   cfg,
		planByPriceID:         planByPriceID,
		portalConfigurationID: cfg.PortalConfigurationID,
	}
}

func (s *BillingService) Get(ctx context.Context, id domain.MerchantID) (domain.Billing, error) {
	rec, err := s.billing.Get(ctx, id)
	if err != nil {
		if err == ErrNotFound {
			// No subscription yet — zero-value Billing, but the member seat cap
			// still applies (the account can have teammates before choosing a plan).
			return domain.Billing{Usage: domain.BillingUsage{MemberCap: domain.DefaultMemberCap}}, nil
		}
		return domain.Billing{}, fmt.Errorf("get billing for %s: %w", id, err)
	}
	return rec.ToDomain(), nil
}

// CreatePortalLink creates a Stripe Billing Portal session scoped to the
// managed Configuration (see EnsurePortalConfiguration) — plan switching
// (to concierge/scale only, never back to the entry plan) and cancellation
// happen inside the portal itself, alongside payment-method and invoice
// management. There is no separate Checkout-based upgrade flow: every
// merchant already has a subscription from account creation (ProvisionTrial),
// so an "upgrade" is a price change on the existing subscription, which is
// exactly what the portal's subscription-update feature does — a new
// Checkout Session would instead create a second, parallel subscription.
func (s *BillingService) CreatePortalLink(ctx context.Context, id domain.MerchantID) (domain.HostedURL, error) {
	rec, err := s.billing.Get(ctx, id)
	if err != nil {
		if err == ErrNotFound {
			return domain.HostedURL{}, ErrNoStripeCustomer
		}
		return domain.HostedURL{}, fmt.Errorf("get billing record for %s: %w", id, err)
	}
	if rec.StripeCustomerID == "" {
		return domain.HostedURL{}, ErrNoStripeCustomer
	}

	url, err := s.stripe.CreatePortalSession(rec.StripeCustomerID, s.cfg.PortalReturnURL, s.portalConfigurationID)
	if err != nil {
		return domain.HostedURL{}, fmt.Errorf("create portal session for %s: %w", id, err)
	}
	return domain.HostedURL{URL: url}, nil
}

// EnsurePortalConfiguration reconciles the Stripe Billing Portal Configuration
// to the desired state (upgradePlans switchable, entryPlan never offered as
// a switch target) and updates s.portalConfigurationID. Called once at
// control-plane startup (api/entry.go); safe to call on every boot — it
// updates the existing configuration in place when one is already
// configured, so the desired state declared here is what's live in Stripe,
// not whatever was last clicked together in the Dashboard.
func (s *BillingService) EnsurePortalConfiguration(ctx context.Context) (string, error) {
	products := make([]PortalProduct, 0, len(upgradePlans))
	for _, plan := range upgradePlans {
		priceID, ok := s.cfg.PlanPriceIDs[plan]
		if !ok || priceID == "" {
			continue
		}
		productID, err := s.stripe.GetPriceProductID(priceID)
		if err != nil {
			return "", fmt.Errorf("resolve product for plan %s: %w", plan, err)
		}
		products = append(products, PortalProduct{ProductID: productID, PriceID: priceID})
	}

	id, err := s.stripe.EnsurePortalConfiguration(s.cfg.PortalConfigurationID, PortalConfigParams{
		DefaultReturnURL:   s.cfg.PortalReturnURL,
		SwitchableProducts: products,
	})
	if err != nil {
		return "", fmt.Errorf("ensure portal configuration: %w", err)
	}
	s.portalConfigurationID = id
	return id, nil
}

func (s *BillingService) GetNotificationPreferences(ctx context.Context, id domain.MerchantID) (domain.NotificationPreferences, error) {
	m, err := s.prefs.Get(ctx, id)
	if err != nil {
		return domain.NotificationPreferences{}, fmt.Errorf("get notification prefs for %s: %w", id, err)
	}
	return m.ToDomain(), nil
}

func (s *BillingService) SetNotificationPreferences(ctx context.Context, id domain.MerchantID, prefs domain.NotificationPreferences) (domain.NotificationPreferences, error) {
	m := &NotificationPrefsModel{
		MerchantID:          string(id),
		EmailEnabled:        prefs.EmailEnabled,
		WeeklyDigest:        prefs.WeeklyDigest,
		AlertEmail:          prefs.AlertEmail,
		AlertRecipients:     prefs.AlertRecipients,
		AlertSeverityFloor:  prefs.AlertSeverityFloor,
		QuietHoursStartHour: prefs.QuietHoursStartHour,
		QuietHoursEndHour:   prefs.QuietHoursEndHour,
		QuietHoursTimezone:  prefs.QuietHoursTimezone,
	}
	if err := s.prefs.Upsert(ctx, m); err != nil {
		return domain.NotificationPreferences{}, fmt.Errorf("set notification prefs for %s: %w", id, err)
	}
	return m.ToDomain(), nil
}

// UpsertBillingRecord lets the Stripe webhook handler (future) sync subscription
// state back into the billing record.
func (s *BillingService) UpsertBillingRecord(ctx context.Context, rec *BillingRecordModel) error {
	rec.UpdatedAt = time.Now().UTC()
	return s.billing.Upsert(ctx, rec)
}

// ProvisionTrial creates a Stripe customer and a trialing subscription on the
// entry-level (self_serve) plan, and persists the billing record — so a
// merchant account never exists without an active subscription. Unlike
// CreateCheckoutLink, this calls the Stripe API directly (no hosted redirect):
// there is no user present to complete a checkout at account-creation time,
// and no payment method is collected or required during the trial.
func (s *BillingService) ProvisionTrial(ctx context.Context, id domain.MerchantID, email string) error {
	priceID, ok := s.cfg.PlanPriceIDs[domain.PlanSelfServe]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownPlan, domain.PlanSelfServe)
	}

	customerID, err := s.stripe.CreateCustomer(email, string(id))
	if err != nil {
		return fmt.Errorf("create stripe customer for %s: %w", id, err)
	}

	subscriptionID, err := s.stripe.CreateTrialSubscription(customerID, priceID, s.cfg.TrialDays)
	if err != nil {
		return fmt.Errorf("create trial subscription for %s: %w", id, err)
	}

	rec := &BillingRecordModel{
		MerchantID:           string(id),
		StripeCustomerID:     customerID,
		StripeSubscriptionID: subscriptionID,
		Plan:                 string(domain.PlanSelfServe),
		Status:               string(domain.BillingActive),
	}
	if err := s.UpsertBillingRecord(ctx, rec); err != nil {
		return fmt.Errorf("upsert trial billing record for %s: %w", id, err)
	}
	return nil
}

var _ Service = (*BillingService)(nil)
