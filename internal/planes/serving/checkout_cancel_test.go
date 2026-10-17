// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/commerce/model"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/wire"
)

// Regression tests for architecture G1 (checkout cancel) and Complete's
// idempotency-key handling — both previously unimplemented.

func TestCheckoutService_Cancel_Success(t *testing.T) {
	const storeID = "store-1"
	sessions := NewMemorySessionStore(time.Minute)
	defer sessions.Close()
	svc := NewCheckoutService(sessions, &noopCache{}, newTestRegistry(storeID, &testConnector{}), &noopEventBus{}, discardLogger())

	seed := &CheckoutSession{
		SessionID: "sess-1", StoreID: storeID, MerchantID: storeID,
		BackendCheckoutID: "backend-1", State: SessionStateReadyForComplete,
		LastResponse: &base.CheckoutResponse{ID: "sess-1", Status: base.CheckoutStatusReadyForComplete, Currency: "USD"},
		CreatedAt:    time.Now(), UpdatedAt: time.Now(),
	}
	if err := sessions.Create(context.Background(), seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	resp, err := svc.Cancel(context.Background(), storeID, "sess-1")
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if resp.Status != base.CheckoutStatusCanceled {
		t.Errorf("expected status canceled, got %q", resp.Status)
	}

	stored, err := sessions.Get(context.Background(), storeID, "sess-1")
	if err != nil {
		t.Fatalf("Get after cancel: %v", err)
	}
	if stored.State != SessionStateCancelled {
		t.Errorf("expected session state cancelled, got %q", stored.State)
	}
}

func TestCheckoutService_Cancel_RepeatedIsIdempotent(t *testing.T) {
	const storeID = "store-1"
	sessions := NewMemorySessionStore(time.Minute)
	defer sessions.Close()
	svc := NewCheckoutService(sessions, &noopCache{}, newTestRegistry(storeID, &testConnector{}), &noopEventBus{}, discardLogger())

	seed := &CheckoutSession{
		SessionID: "sess-1", StoreID: storeID, MerchantID: storeID,
		BackendCheckoutID: "backend-1", State: SessionStateReadyForComplete,
		LastResponse: &base.CheckoutResponse{ID: "sess-1", Status: base.CheckoutStatusReadyForComplete},
		CreatedAt:    time.Now(), UpdatedAt: time.Now(),
	}
	if err := sessions.Create(context.Background(), seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := svc.Cancel(context.Background(), storeID, "sess-1"); err != nil {
		t.Fatalf("first Cancel: %v", err)
	}
	resp, err := svc.Cancel(context.Background(), storeID, "sess-1")
	if err != nil {
		t.Fatalf("repeated Cancel should be idempotent, got error: %v", err)
	}
	if resp.Status != base.CheckoutStatusCanceled {
		t.Errorf("expected status canceled on replay, got %q", resp.Status)
	}
}

func TestCheckoutService_Cancel_RejectsCompletedSession(t *testing.T) {
	const storeID = "store-1"
	sessions := NewMemorySessionStore(time.Minute)
	defer sessions.Close()
	svc := NewCheckoutService(sessions, &noopCache{}, newTestRegistry(storeID, &testConnector{}), &noopEventBus{}, discardLogger())

	seed := &CheckoutSession{
		SessionID: "sess-1", StoreID: storeID, MerchantID: storeID,
		BackendCheckoutID: "backend-1", State: SessionStateCommittedPendingHandoff,
		LastResponse: &base.CheckoutResponse{ID: "sess-1", Status: base.CheckoutStatusCompleted},
		CreatedAt:    time.Now(), UpdatedAt: time.Now(),
	}
	if err := sessions.Create(context.Background(), seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := svc.Cancel(context.Background(), storeID, "sess-1"); !errors.Is(err, ErrCheckoutNotModifiable) {
		t.Fatalf("expected ErrCheckoutNotModifiable, got %v", err)
	}
}

func TestCheckoutService_Update_RejectsCancelledSession(t *testing.T) {
	const storeID = "store-1"
	sessions := NewMemorySessionStore(time.Minute)
	defer sessions.Close()
	svc := NewCheckoutService(sessions, &noopCache{}, newTestRegistry(storeID, &testConnector{}), &noopEventBus{}, discardLogger())

	seed := &CheckoutSession{
		SessionID: "sess-1", StoreID: storeID, MerchantID: storeID,
		BackendCheckoutID: "backend-1", State: SessionStateCancelled,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := sessions.Create(context.Background(), seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err := svc.Update(context.Background(), &testStoreProvider{storeID: storeID}, storeID, "sess-1", UpdateCheckoutInput{})
	if !errors.Is(err, ErrCheckoutNotModifiable) {
		t.Fatalf("expected ErrCheckoutNotModifiable, got %v", err)
	}
}

func TestCheckoutService_Complete_RejectsCancelledSession(t *testing.T) {
	const storeID = "store-1"
	sessions := NewMemorySessionStore(time.Minute)
	defer sessions.Close()
	svc := NewCheckoutService(sessions, &noopCache{}, newTestRegistry(storeID, &testConnector{}), &noopEventBus{}, discardLogger())

	seed := &CheckoutSession{
		SessionID: "sess-1", StoreID: storeID, MerchantID: storeID,
		BackendCheckoutID: "backend-1", State: SessionStateCancelled,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := sessions.Create(context.Background(), seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err := svc.Complete(context.Background(), &testStoreProvider{storeID: storeID}, storeID, "sess-1", &model.Payment{}, "")
	if !errors.Is(err, ErrCheckoutNotModifiable) {
		t.Fatalf("expected ErrCheckoutNotModifiable, got %v", err)
	}
}

// TestCheckoutService_Complete_IdempotentReplay verifies that completing
// with the same idempotency key and the same payment payload replays the
// original result without calling the connector again, while the same key
// with a different payload is a conflict — mirroring Create's rule.
func TestCheckoutService_Complete_IdempotentReplay(t *testing.T) {
	const storeID = "store-1"
	conn := &testConnector{
		completeResp: &base.CheckoutResponse{
			Status: base.CheckoutStatusCompleted,
			Order:  &model.OrderConfirmation{ID: "order-1", PermalinkURL: "https://shop.example/order/1"},
		},
	}
	sessions := NewMemorySessionStore(time.Minute)
	defer sessions.Close()
	svc := NewCheckoutService(sessions, &noopCache{}, newTestRegistry(storeID, conn), &noopEventBus{}, discardLogger())

	seed := &CheckoutSession{
		SessionID: "sess-1", StoreID: storeID, MerchantID: storeID,
		BackendCheckoutID: "backend-1", State: SessionStateReadyForComplete,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := sessions.Create(context.Background(), seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	payment := &model.Payment{Instruments: []model.SelectedPaymentInstrument{{
		PaymentInstrument: model.PaymentInstrument{HandlerID: "google_pay"},
		Selected:          true,
	}}}
	instance := &testStoreProvider{storeID: storeID, handlers: map[string][]wire.PaymentHandler{
		"google_pay": {{ID: "google_pay"}},
		"shop_pay":   {{ID: "shop_pay"}},
	}}

	result1, err := svc.Complete(context.Background(), instance, storeID, "sess-1", payment, "idem-1")
	if err != nil {
		t.Fatalf("first Complete: %v", err)
	}
	if result1.Order == nil || result1.Order.ID != "order-1" {
		t.Fatalf("expected order-1, got %+v", result1)
	}

	// Replay: same key, same payload — no second connector call, same order.
	result2, err := svc.Complete(context.Background(), instance, storeID, "sess-1", payment, "idem-1")
	if err != nil {
		t.Fatalf("replay Complete: %v", err)
	}
	if result2.Order == nil || result2.Order.ID != "order-1" {
		t.Fatalf("expected replayed order-1, got %+v", result2)
	}
	if conn.completeCalls != 1 {
		t.Errorf("expected exactly 1 connector call (replay must not call again), got %d", conn.completeCalls)
	}

	// Same key, different payload — conflict.
	otherPayment := &model.Payment{Instruments: []model.SelectedPaymentInstrument{{
		PaymentInstrument: model.PaymentInstrument{HandlerID: "shop_pay"},
		Selected:          true,
	}}}
	_, err = svc.Complete(context.Background(), instance, storeID, "sess-1", otherPayment, "idem-1")
	if !errors.Is(err, ErrIdempotencyKeyConflict) {
		t.Fatalf("expected ErrIdempotencyKeyConflict, got %v", err)
	}
}
