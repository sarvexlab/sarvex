// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package billing

import (
	"fmt"
	"net/http"

	"github.com/samber/lo"
	"github.com/stripe/stripe-go/v72"
	"github.com/stripe/stripe-go/v72/client"
)

// StripeClient is the minimal Stripe surface needed by the billing service.
type StripeClient interface {
	// CreatePortalSession creates a Stripe Billing Portal session for an
	// existing customer, scoped to configurationID, and returns the hosted URL.
	CreatePortalSession(customerID, returnURL, configurationID string) (string, error)
	// CreateCustomer creates a Stripe customer and returns its ID.
	CreateCustomer(email, name string) (string, error)
	// CreateTrialSubscription creates a subscription on priceID for an
	// existing customer with a trialDays-long trial and no default payment
	// method, and returns the subscription ID.
	CreateTrialSubscription(customerID, priceID string, trialDays int) (string, error)
	// GetPriceProductID returns the Stripe Product ID a given Price belongs
	// to — needed because the Billing Portal Configuration's switchable-plan
	// list is keyed by Product, not Price.
	GetPriceProductID(priceID string) (string, error)
	// EnsurePortalConfiguration reconciles the Stripe Billing Portal
	// Configuration to match params: updates the configuration at
	// existingID if set, otherwise creates a new one. Returns the
	// configuration's ID either way, so a freshly created one can be
	// persisted into config for subsequent boots.
	EnsurePortalConfiguration(existingID string, params PortalConfigParams) (string, error)
}

// PortalProduct is a Stripe Product+Price pair the Billing Portal
// Configuration should offer as a subscription-update target.
type PortalProduct struct {
	ProductID string
	PriceID   string
}

// PortalConfigParams describes the desired state of the managed Billing
// Portal Configuration. SwitchableProducts deliberately excludes the
// entry-level plan — a plan not listed here is never offered as a switch
// target, which is what makes "no downgrade to the entry tier" true without
// any application-side check.
type PortalConfigParams struct {
	DefaultReturnURL   string
	SwitchableProducts []PortalProduct
}

type stripeClientImpl struct {
	api *client.API
}

// newStripeClientImpl returns a StripeClient backed by the Stripe Go SDK.
// Use NewClientWithHTTPClient to construct a StripeClient with an explicit HTTP client.
func newStripeClientImpl(secretKey string, httpClient *http.Client) StripeClient {
	c := &client.API{}
	var backends *stripe.Backends
	if httpClient != nil {
		backends = stripe.NewBackends(httpClient)
	}
	c.Init(secretKey, backends)
	return &stripeClientImpl{api: c}
}

func (s *stripeClientImpl) CreatePortalSession(customerID, returnURL, configurationID string) (string, error) {
	p := &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(customerID),
		ReturnURL: stripe.String(returnURL),
	}
	if configurationID != "" {
		p.Configuration = stripe.String(configurationID)
	}
	sess, err := s.api.BillingPortalSessions.New(p)
	if err != nil {
		return "", fmt.Errorf("stripe create portal session: %w", err)
	}
	return sess.URL, nil
}

func (s *stripeClientImpl) CreateCustomer(email, name string) (string, error) {
	p := &stripe.CustomerParams{
		Email: stripe.String(email),
		Name:  stripe.String(name),
	}
	cust, err := s.api.Customers.New(p)
	if err != nil {
		return "", fmt.Errorf("stripe create customer: %w", err)
	}
	return cust.ID, nil
}

// CreateTrialSubscription creates a subscription with no payment method on
// file. TrialPeriodDays alone is enough for Stripe to accept this — no
// payment_behavior override needed, since the subscription simply starts in
// the trialing state rather than attempting to collect payment up front.
func (s *stripeClientImpl) CreateTrialSubscription(customerID, priceID string, trialDays int) (string, error) {
	p := &stripe.SubscriptionParams{
		Customer:        stripe.String(customerID),
		TrialPeriodDays: stripe.Int64(int64(trialDays)),
		Items: []*stripe.SubscriptionItemsParams{
			{Price: stripe.String(priceID)},
		},
	}
	sub, err := s.api.Subscriptions.New(p)
	if err != nil {
		return "", fmt.Errorf("stripe create trial subscription: %w", err)
	}
	return sub.ID, nil
}

func (s *stripeClientImpl) GetPriceProductID(priceID string) (string, error) {
	price, err := s.api.Prices.Get(priceID, nil)
	if err != nil {
		return "", fmt.Errorf("stripe get price %s: %w", priceID, err)
	}
	if price.Product == nil {
		return "", fmt.Errorf("stripe price %s has no product", priceID)
	}
	return price.Product.ID, nil
}

func (s *stripeClientImpl) EnsurePortalConfiguration(existingID string, params PortalConfigParams) (string, error) {
	products := lo.Map(params.SwitchableProducts, func(sp PortalProduct, _ int) *stripe.BillingPortalConfigurationFeaturesSubscriptionUpdateProductParams {
		return &stripe.BillingPortalConfigurationFeaturesSubscriptionUpdateProductParams{
			Product: stripe.String(sp.ProductID),
			Prices:  []*string{stripe.String(sp.PriceID)},
		}
	})

	features := &stripe.BillingPortalConfigurationFeaturesParams{
		SubscriptionUpdate: &stripe.BillingPortalConfigurationFeaturesSubscriptionUpdateParams{
			Enabled:               stripe.Bool(true),
			DefaultAllowedUpdates: []*string{stripe.String("price")},
			ProrationBehavior:     stripe.String("create_prorations"),
			Products:              products,
		},
		SubscriptionCancel: &stripe.BillingPortalConfigurationFeaturesSubscriptionCancelParams{
			Enabled: stripe.Bool(true),
			Mode:    stripe.String("at_period_end"),
			CancellationReason: &stripe.BillingPortalConfigurationFeaturesSubscriptionCancelCancellationReasonParams{
				Enabled: stripe.Bool(true),
			},
		},
		PaymentMethodUpdate: &stripe.BillingPortalConfigurationFeaturesPaymentMethodUpdateParams{
			Enabled: stripe.Bool(true),
		},
		InvoiceHistory: &stripe.BillingPortalConfigurationFeaturesInvoiceHistoryParams{
			Enabled: stripe.Bool(true),
		},
	}

	if existingID != "" {
		cfg, err := s.api.BillingPortalConfigurations.Update(existingID, &stripe.BillingPortalConfigurationParams{
			Features: features,
		})
		if err != nil {
			return "", fmt.Errorf("stripe update portal configuration %s: %w", existingID, err)
		}
		return cfg.ID, nil
	}

	cfg, err := s.api.BillingPortalConfigurations.New(&stripe.BillingPortalConfigurationParams{
		Features:         features,
		DefaultReturnURL: stripe.String(params.DefaultReturnURL),
	})
	if err != nil {
		return "", fmt.Errorf("stripe create portal configuration: %w", err)
	}
	return cfg.ID, nil
}
