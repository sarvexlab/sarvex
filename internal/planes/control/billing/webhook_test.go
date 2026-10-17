// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package billing_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stripe/stripe-go/v72/webhook"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/billing"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

const testWebhookSecret = "whsec_test"

func sign(t *testing.T, payload []byte) string {
	t.Helper()
	now := time.Now()
	sig := webhook.ComputeSignature(now, payload, testWebhookSecret)
	return fmt.Sprintf("t=%d,v1=%x", now.Unix(), sig)
}

func newWebhookService(billingRepo *fakeBillingRepo, webhookRepo *fakeWebhookEventRepo, stripe *fakeStripe) billing.Service {
	cfg := testBillingConfig()
	return billing.NewBillingService(billingRepo, newFakePrefsRepo(), webhookRepo, stripe, cfg)
}

func subscriptionEventPayload(eventID, eventType, customerID, subscriptionID, status, priceID string) []byte {
	return []byte(fmt.Sprintf(`{
		"id": %q,
		"type": %q,
		"data": {
			"object": {
				"id": %q,
				"object": "subscription",
				"customer": %q,
				"status": %q,
				"items": {"data": [{"id": "si_1", "price": {"id": %q}}]}
			}
		}
	}`, eventID, eventType, subscriptionID, customerID, status, priceID))
}

func TestHandleStripeWebhook_InvalidSignature(t *testing.T) {
	svc := newWebhookService(newFakeBillingRepo(), newFakeWebhookEventRepo(), &fakeStripe{})
	payload := subscriptionEventPayload("evt_1", "customer.subscription.updated", "cus_1", "sub_1", "active", "price_self_serve")

	err := svc.HandleStripeWebhook(context.Background(), payload, "t=1,v1=bad")
	assert.ErrorIs(t, err, billing.ErrInvalidWebhookSignature)
}

func TestHandleStripeWebhook_DuplicateEvent_IsNoOp(t *testing.T) {
	repo := newFakeBillingRepo()
	repo.data["merch-1"] = &billing.BillingRecordModel{
		MerchantID:       "merch-1",
		StripeCustomerID: "cus_1",
		Status:           string(domain.BillingActive),
	}
	svc := newWebhookService(repo, newFakeWebhookEventRepo(), &fakeStripe{})
	payload := subscriptionEventPayload("evt_dup", "customer.subscription.updated", "cus_1", "sub_1", "past_due", "price_self_serve")
	sig := sign(t, payload)

	require.NoError(t, svc.HandleStripeWebhook(context.Background(), payload, sig))
	require.Equal(t, string(domain.BillingPastDue), repo.data["merch-1"].Status, "first delivery should apply the event")
	repo.data["merch-1"].Status = string(domain.BillingActive)

	require.NoError(t, svc.HandleStripeWebhook(context.Background(), payload, sig))
	assert.Equal(t, string(domain.BillingActive), repo.data["merch-1"].Status, "second delivery of the same event must not reprocess")
}

func TestHandleStripeWebhook_SubscriptionUpdated_PastDue(t *testing.T) {
	repo := newFakeBillingRepo()
	repo.data["merch-1"] = &billing.BillingRecordModel{
		MerchantID:       "merch-1",
		StripeCustomerID: "cus_123",
		Plan:             string(domain.PlanSelfServe),
		Status:           string(domain.BillingActive),
	}
	svc := newWebhookService(repo, newFakeWebhookEventRepo(), &fakeStripe{})
	payload := subscriptionEventPayload("evt_2", "customer.subscription.updated", "cus_123", "sub_123", "past_due", "price_concierge")

	err := svc.HandleStripeWebhook(context.Background(), payload, sign(t, payload))
	require.NoError(t, err)

	rec := repo.data["merch-1"]
	assert.Equal(t, string(domain.BillingPastDue), rec.Status)
	assert.Equal(t, string(domain.PlanConcierge), rec.Plan, "plan should follow the subscription's price id")
}

func TestHandleStripeWebhook_SubscriptionDeleted_Canceled(t *testing.T) {
	repo := newFakeBillingRepo()
	repo.data["merch-1"] = &billing.BillingRecordModel{
		MerchantID:       "merch-1",
		StripeCustomerID: "cus_123",
		Status:           string(domain.BillingActive),
	}
	svc := newWebhookService(repo, newFakeWebhookEventRepo(), &fakeStripe{})
	payload := subscriptionEventPayload("evt_3", "customer.subscription.deleted", "cus_123", "sub_123", "canceled", "price_self_serve")

	err := svc.HandleStripeWebhook(context.Background(), payload, sign(t, payload))
	require.NoError(t, err)
	assert.Equal(t, string(domain.BillingCanceled), repo.data["merch-1"].Status)
}

func TestHandleStripeWebhook_SubscriptionEvent_UnknownCustomer_IsNoOp(t *testing.T) {
	repo := newFakeBillingRepo()
	svc := newWebhookService(repo, newFakeWebhookEventRepo(), &fakeStripe{})
	payload := subscriptionEventPayload("evt_4", "customer.subscription.updated", "cus_unknown", "sub_1", "active", "price_self_serve")

	err := svc.HandleStripeWebhook(context.Background(), payload, sign(t, payload))
	assert.NoError(t, err)
	assert.Empty(t, repo.data)
}

func TestHandleStripeWebhook_ProcessingFailure_LeavesEventUnmarked(t *testing.T) {
	repo := newFakeBillingRepo()
	repo.data["merch-1"] = &billing.BillingRecordModel{
		MerchantID:       "merch-1",
		StripeCustomerID: "cus_1",
		Status:           string(domain.BillingActive),
	}
	webhookRepo := newFakeWebhookEventRepo()
	svc := newWebhookService(repo, webhookRepo, &fakeStripe{})
	payload := subscriptionEventPayload("evt_retry", "customer.subscription.updated", "cus_1", "sub_1", "past_due", "price_self_serve")
	sig := sign(t, payload)

	repo.err = fmt.Errorf("transient db error")
	err := svc.HandleStripeWebhook(context.Background(), payload, sig)
	require.Error(t, err, "upsert failure should surface as an error")

	processed, checkErr := webhookRepo.IsProcessed(context.Background(), "evt_retry")
	require.NoError(t, checkErr)
	assert.False(t, processed, "a failed attempt must not be marked processed, so Stripe's redelivery can retry it")

	// Simulate the redelivery succeeding once the transient error clears.
	repo.err = nil
	require.NoError(t, svc.HandleStripeWebhook(context.Background(), payload, sig))
	assert.Equal(t, string(domain.BillingPastDue), repo.data["merch-1"].Status)
}

func TestHandleStripeWebhook_UnhandledEventType_IsNoOp(t *testing.T) {
	repo := newFakeBillingRepo()
	svc := newWebhookService(repo, newFakeWebhookEventRepo(), &fakeStripe{})
	payload := []byte(`{"id": "evt_5", "type": "invoice.paid", "data": {"object": {}}}`)

	err := svc.HandleStripeWebhook(context.Background(), payload, sign(t, payload))
	assert.NoError(t, err)
	assert.Empty(t, repo.data)
}
