// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package billing

import (
	"fmt"
	"log/slog"
	"maps"
	"sync"
)

// mockStripeClient fabricates hosted URLs instead of calling Stripe, so
// local/test environments can exercise the trial/portal flow without an
// outbound network call or a real Stripe account. No Stripe customer,
// subscription, or configuration is actually created.
type mockStripeClient struct {
	mu sync.Mutex
	// portalConfigurations records EnsurePortalConfiguration calls, keyed by
	// the fabricated configuration ID. Test helper.
	portalConfigurations map[string]PortalConfigParams
}

func newMockStripeClient() *mockStripeClient {
	return &mockStripeClient{portalConfigurations: make(map[string]PortalConfigParams)}
}

func (c *mockStripeClient) CreatePortalSession(customerID, returnURL, configurationID string) (string, error) {
	slog.Info("stripe portal session (mock mode, not created)", "customer_id", customerID, "configuration_id", configurationID)

	return fmt.Sprintf("%s?mock_portal=1&customer=%s&configuration=%s", returnURL, customerID, configurationID), nil
}

func (c *mockStripeClient) CreateCustomer(email, _ string) (string, error) {
	slog.Info("stripe customer (mock mode, not created)", "email", email)
	return fmt.Sprintf("mock_cus_%s", email), nil
}

func (c *mockStripeClient) CreateTrialSubscription(customerID, priceID string, trialDays int) (string, error) {
	slog.Info("stripe trial subscription (mock mode, not created)",
		"customer_id", customerID, "price_id", priceID, "trial_days", trialDays)
	return fmt.Sprintf("mock_sub_%s_%s", customerID, priceID), nil
}

func (c *mockStripeClient) GetPriceProductID(priceID string) (string, error) {
	return fmt.Sprintf("mock_prod_for_%s", priceID), nil
}

func (c *mockStripeClient) EnsurePortalConfiguration(existingID string, params PortalConfigParams) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := existingID
	if id == "" {
		id = fmt.Sprintf("mock_bpc_%d", len(c.portalConfigurations)+1)
	}
	c.portalConfigurations[id] = params
	slog.Info("stripe portal configuration ensured (mock mode, not created)", "configuration_id", id, "switchable_products", len(params.SwitchableProducts))
	return id, nil
}

// PortalConfigurations returns the configurations ensured so far, keyed by
// ID. Test helper.
func (c *mockStripeClient) PortalConfigurations() map[string]PortalConfigParams {
	c.mu.Lock()
	defer c.mu.Unlock()
	return maps.Clone(c.portalConfigurations)
}

var _ StripeClient = (*mockStripeClient)(nil)
