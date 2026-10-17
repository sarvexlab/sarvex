// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

// Complete validates every instrument in the payment block, not only the one
// marked Selected. These tests cover the cases that distinguishes that from
// reading a single handler id: an unadvertised handler on an unmarked
// instrument, and an instrument with no handler id at all. Both were accepted
// when the check read only the selected instrument.

// completeFixture builds a CheckoutService with one store, one connector and
// a session already parked at ReadyForComplete — the state Complete expects.
// Returns the service and the connector, so a test can assert the connector
// was or was not reached.
func completeFixture(t *testing.T, storeID string) (*CheckoutService, *testConnector) {
	t.Helper()

	conn := &testConnector{completeResp: &base.CheckoutResponse{
		Status: base.CheckoutStatusCompleted,
		Order:  &model.OrderConfirmation{ID: "order-1"},
	}}
	sessions := NewMemorySessionStore(time.Minute)
	t.Cleanup(sessions.Close)

	svc := NewCheckoutService(sessions, &noopCache{}, newTestRegistry(storeID, conn), &noopEventBus{}, discardLogger())

	seed := &CheckoutSession{
		SessionID: "sess-1", StoreID: storeID, MerchantID: storeID,
		BackendCheckoutID: "backend-1", State: SessionStateReadyForComplete,
		LineItems: []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := sessions.Create(context.Background(), seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return svc, conn
}

// advertisingStore is a store whose profile advertises googlepay, and nothing
// else — the shape every test here validates against.
func advertisingStore(storeID string) *testStoreProvider {
	return &testStoreProvider{storeID: storeID, handlers: map[string][]wire.PaymentHandler{
		"com.google.pay": {{ID: "googlepay"}},
	}}
}

func instrument(handlerID string, selected bool) model.SelectedPaymentInstrument {
	return model.SelectedPaymentInstrument{
		PaymentInstrument: model.PaymentInstrument{HandlerID: handlerID},
		Selected:          selected,
	}
}

// The case that motivates validating every instrument: the selected
// instrument is legitimate, so a check that reads only the selection passes
// the request — and Complete then forwards the whole block, unadvertised
// handler included, to the connector.
func TestCheckoutService_Complete_RejectsUnadvertisedHandlerOnUnselectedInstrument(t *testing.T) {
	const storeID = "store-1"
	svc, conn := completeFixture(t, storeID)

	payment := &model.Payment{Instruments: []model.SelectedPaymentInstrument{
		instrument("googlepay", true),
		instrument("dev.example.not_a_real_handler", false),
	}}

	_, err := svc.Complete(context.Background(), advertisingStore(storeID), storeID, "sess-1", payment, "")
	if !errors.Is(err, ErrUnknownPaymentHandler) {
		t.Fatalf("expected ErrUnknownPaymentHandler for an unadvertised handler on an unselected "+
			"instrument — the whole block is forwarded to the connector, so an unmarked entry is not "+
			"harmless — got %v", err)
	}
	if atomic.LoadInt32(&conn.completeCalls) != 0 {
		t.Errorf("connector must not be reached once any instrument fails validation")
	}
}

// An instrument with no handler id names no advertised handler, so it fails
// the same rule. This used to pass: the old check read one handler id and
// skipped validation entirely when it came back empty.
func TestCheckoutService_Complete_RejectsEmptyHandlerID(t *testing.T) {
	const storeID = "store-1"
	svc, conn := completeFixture(t, storeID)

	payment := &model.Payment{Instruments: []model.SelectedPaymentInstrument{
		instrument("", true),
	}}

	_, err := svc.Complete(context.Background(), advertisingStore(storeID), storeID, "sess-1", payment, "")
	if !errors.Is(err, ErrUnknownPaymentHandler) {
		t.Fatalf("expected ErrUnknownPaymentHandler for an empty handler_id, got %v", err)
	}
	if atomic.LoadInt32(&conn.completeCalls) != 0 {
		t.Errorf("connector must not be reached for an instrument with no handler_id")
	}
}

// Several instruments, all advertised, still complete. The tightening must
// reject unadvertised handlers, not multi-instrument payments.
func TestCheckoutService_Complete_AllowsEveryInstrumentAdvertised(t *testing.T) {
	const storeID = "store-1"
	svc, conn := completeFixture(t, storeID)

	payment := &model.Payment{Instruments: []model.SelectedPaymentInstrument{
		instrument("googlepay", false),
		instrument("googlepay", true),
	}}

	result, err := svc.Complete(context.Background(), advertisingStore(storeID), storeID, "sess-1", payment, "")
	if err != nil {
		t.Fatalf("Complete: expected advertised handlers on every instrument to be accepted, got %v", err)
	}
	if result.Order == nil || result.Order.ID != "order-1" {
		t.Fatalf("expected the placed order to be reported, got %+v", result)
	}
	if atomic.LoadInt32(&conn.completeCalls) != 1 {
		t.Errorf("expected exactly one connector complete call, got %d", conn.completeCalls)
	}
}

// A nil payment, and one with no instruments, must reach the connector
// exactly as before. Rejecting either would be a new refusal beyond this
// check — the request fails downstream at the connector, which is where that
// belongs.
func TestCheckoutService_Complete_PaymentWithNoInstrumentsStillReachesConnector(t *testing.T) {
	const storeID = "store-1"

	for name, payment := range map[string]*model.Payment{
		"nil payment":    nil,
		"no instruments": {},
	} {
		t.Run(name, func(t *testing.T) {
			svc, conn := completeFixture(t, storeID)

			// The store advertises googlepay, so a validation that fired on
			// an absent handler id would reject here.
			if _, err := svc.Complete(context.Background(), advertisingStore(storeID), storeID, "sess-1", payment, ""); err != nil {
				t.Fatalf("Complete: expected %s to pass validation untouched, got %v", name, err)
			}
			if atomic.LoadInt32(&conn.completeCalls) != 1 {
				t.Errorf("expected the connector to be reached once for %s, got %d calls", name, conn.completeCalls)
			}
		})
	}
}
