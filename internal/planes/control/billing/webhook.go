// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/stripe/stripe-go/v72"
	"github.com/stripe/stripe-go/v72/webhook"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// mapStripeStatus translates a Stripe subscription status into V1's
// BillingStatus. V1 has no "incomplete" bucket, so incomplete/unpaid/past_due
// all land on BillingPastDue — a merchant whose payment hasn't gone through
// is not yet active, but should not be treated as canceled either. This is
// a deliberate simplification for V1, not a 1:1 mapping.
func mapStripeStatus(s stripe.SubscriptionStatus) domain.BillingStatus {
	switch s {
	case stripe.SubscriptionStatusActive, stripe.SubscriptionStatusTrialing:
		return domain.BillingActive
	case stripe.SubscriptionStatusPastDue, stripe.SubscriptionStatusUnpaid, stripe.SubscriptionStatusIncomplete:
		return domain.BillingPastDue
	case stripe.SubscriptionStatusCanceled, stripe.SubscriptionStatusIncompleteExpired:
		return domain.BillingCanceled
	default:
		return domain.BillingPastDue
	}
}

// HandleStripeWebhook verifies sigHeader against the configured webhook
// secret, dedupes by Stripe event ID (Stripe redelivers at least once), and
// syncs the resulting event into the billing record. Unhandled event types
// are acknowledged as a no-op. The event is only marked processed after it
// has been successfully applied — a failure partway through leaves it
// unmarked, so Stripe's redelivery can retry rather than being silently
// dropped.
func (s *BillingService) HandleStripeWebhook(ctx context.Context, payload []byte, sigHeader string) error {
	event, err := webhook.ConstructEvent(payload, sigHeader, s.cfg.WebhookSecret)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidWebhookSignature, err)
	}

	processed, err := s.webhookEvents.IsProcessed(ctx, event.ID)
	if err != nil {
		return fmt.Errorf("check webhook event %s processed: %w", event.ID, err)
	}
	if processed {
		return nil
	}

	switch event.Type {
	case "customer.subscription.updated":
		err = s.handleSubscriptionEvent(ctx, event, false)
	case "customer.subscription.deleted":
		err = s.handleSubscriptionEvent(ctx, event, true)
	default:
		return nil // unhandled type — nothing was applied, no need to dedupe it
	}
	if err != nil {
		return err
	}

	if err := s.webhookEvents.MarkProcessed(ctx, event.ID); err != nil {
		return fmt.Errorf("mark webhook event %s processed: %w", event.ID, err)
	}
	return nil
}

func (s *BillingService) handleSubscriptionEvent(ctx context.Context, event stripe.Event, deleted bool) error {
	var sub stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
		return fmt.Errorf("unmarshal subscription: %w", err)
	}
	if sub.Customer == nil {
		return fmt.Errorf("subscription %s missing customer", sub.ID)
	}

	rec, err := s.billing.GetByStripeCustomerID(ctx, sub.Customer.ID)
	if err != nil {
		if err == ErrNotFound {
			// Event for a customer we have no record of — e.g. a different
			// Stripe account/env, or a race with ProvisionTrial's own upsert.
			// Nothing to update; Stripe does not benefit from a retry.
			slog.Warn("stripe subscription event for unknown customer", "customer_id", sub.Customer.ID, "event_id", event.ID)
			return nil
		}
		return fmt.Errorf("get billing record for stripe customer %s: %w", sub.Customer.ID, err)
	}

	status := mapStripeStatus(sub.Status)
	if deleted {
		status = domain.BillingCanceled
	}
	rec.Status = string(status)
	rec.StripeSubscriptionID = sub.ID
	if sub.Items != nil && len(sub.Items.Data) > 0 && sub.Items.Data[0].Price != nil {
		if plan, ok := s.planByPriceID[sub.Items.Data[0].Price.ID]; ok {
			rec.Plan = string(plan)
		}
	}

	if err := s.UpsertBillingRecord(ctx, rec); err != nil {
		return fmt.Errorf("upsert billing record for stripe customer %s: %w", sub.Customer.ID, err)
	}
	return nil
}
