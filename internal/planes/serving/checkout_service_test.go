// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/commerce/model"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/eventbus"
	pkgucp "github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/wire"
	"github.com/sarvexlabs/sarvex.cp/internal/platform/woocommerce"
)

// ─── Test doubles ───────────────────────────────────────────────────────────

// flakySessionStore wraps a real SessionStore and fails the next N calls to
// Update before delegating, so tests can exercise what happens when a
// connector call has already succeeded at the merchant but persisting that
// fact locally hiccups.
type flakySessionStore struct {
	SessionStore
	failUpdates int32 // number of Update calls left to fail; -1 = fail forever
}

func (f *flakySessionStore) Update(ctx context.Context, session *CheckoutSession) error {
	if f.failUpdates != 0 {
		if f.failUpdates > 0 {
			f.failUpdates--
		}
		return errors.New("flaky session store: simulated persistence failure")
	}
	return f.SessionStore.Update(ctx, session)
}

// testConnector is a minimal base.Connector stub; only the checkout methods
// tests configure are meaningful, everything else is unused filler to
// satisfy the interface.
type testConnector struct {
	createResp, updateResp, completeResp, getResp *base.CheckoutResponse
	createErr, updateErr, completeErr, getErr     error
	updateCalls, completeCalls                    int32
}

func (c *testConnector) CheckoutCreate(ctx context.Context, storeID string, req *base.CreateCheckoutRequest) (*base.CheckoutResponse, error) {
	return c.createResp, c.createErr
}
func (c *testConnector) CheckoutGet(ctx context.Context, storeID, checkoutID string) (*base.CheckoutResponse, error) {
	return c.getResp, c.getErr
}
func (c *testConnector) CheckoutUpdate(ctx context.Context, storeID, checkoutID string, req *base.UpdateCheckoutRequest) (*base.CheckoutResponse, error) {
	atomic.AddInt32(&c.updateCalls, 1)
	return c.updateResp, c.updateErr
}
func (c *testConnector) CheckoutComplete(ctx context.Context, storeID, checkoutID string, req *base.CompleteCheckoutRequest) (*base.CheckoutResponse, error) {
	atomic.AddInt32(&c.completeCalls, 1)
	return c.completeResp, c.completeErr
}
func (c *testConnector) CheckoutCancel(ctx context.Context, storeID, checkoutID string) (*base.CheckoutResponse, error) {
	return nil, nil
}
func (c *testConnector) CheckoutURL(ctx context.Context, storeID, checkoutID string) (string, error) {
	return "", nil
}
func (c *testConnector) OrderGet(ctx context.Context, storeID, orderID string) (*base.OrderEvent, error) {
	return nil, nil
}
func (c *testConnector) CatalogSearch(ctx context.Context, storeID string, req *base.CatalogSearchRequest) (*base.CatalogSearchResponse, error) {
	return nil, nil
}
func (c *testConnector) CatalogLookup(ctx context.Context, storeID, itemID string) (*base.CatalogLookupResponse, error) {
	return nil, nil
}
func (c *testConnector) UpdateProduct(ctx context.Context, storeID, itemID string, updates map[string]any) error {
	return nil
}
func (c *testConnector) CartCreate(ctx context.Context, storeID string, req *base.CreateCartRequest) (*base.CartResponse, error) {
	return nil, nil
}
func (c *testConnector) CartGet(ctx context.Context, storeID, cartID string) (*base.CartResponse, error) {
	return nil, nil
}
func (c *testConnector) CartUpdate(ctx context.Context, storeID, cartID string, req *base.UpdateCartRequest) (*base.CartResponse, error) {
	return nil, nil
}
func (c *testConnector) FetchProduct(ctx context.Context, storeID, sku string) (*base.CatalogEntity, error) {
	return nil, nil
}
func (c *testConnector) FetchInventory(ctx context.Context, storeID, sku string) (*base.InventoryEntity, error) {
	return nil, nil
}
func (c *testConnector) FetchPricing(ctx context.Context, storeID, sku string) (*base.PricingEntity, error) {
	return nil, nil
}
func (c *testConnector) FetchCatalog(ctx context.Context, storeID string, pageSize int) ([]base.CatalogEntity, error) {
	return nil, nil
}
func (c *testConnector) Ping(ctx context.Context, storeID string) (base.HealthStatus, error) {
	return base.HealthHealthy, nil
}

var _ base.Connector = (*testConnector)(nil)

// testStoreProvider is a minimal pkgucp.StoreProvider for resolving a
// connector out of the registry. handlers is nil by default (no advertised
// payment handlers) — set it to exercise Complete's handler-advertisement
// check against a store that does advertise one.
type testStoreProvider struct {
	storeID  string
	handlers map[string][]wire.PaymentHandler
}

func (p *testStoreProvider) StoreID() string                            { return p.storeID }
func (p *testStoreProvider) Version() string                            { return "2026-04-08" }
func (p *testStoreProvider) Services() map[string][]wire.Service        { return nil }
func (p *testStoreProvider) Capabilities() map[string][]wire.Capability { return nil }
func (p *testStoreProvider) PaymentHandlers() map[string][]wire.PaymentHandler {
	return p.handlers
}
func (p *testStoreProvider) SigningKeys() []wire.JWK { return nil }
func (p *testStoreProvider) StorefrontURL() string   { return "" }
func (p *testStoreProvider) Configuration() pkgucp.Configuration {
	return pkgucp.Configuration{MerchantID: p.storeID, StoreID: p.storeID, Platform: woocommerce.PlatformName}
}

// noopCache implements ServingCache with just enough behavior for the
// checkout service (only Watermark is exercised); a fresh, non-stale
// watermark by default.
type noopCache struct {
	watermark *CacheWatermark
}

func (c *noopCache) GetProduct(ctx context.Context, storeID, sku string) (*CacheProduct, error) {
	return nil, ErrNotFound
}
func (c *noopCache) Search(ctx context.Context, storeID, query string, limit int, cursor string) (*CacheSearchResult, error) {
	return &CacheSearchResult{}, nil
}
func (c *noopCache) GetProfile(ctx context.Context, storeID string) (*CacheProfile, error) {
	return nil, ErrNotFound
}
func (c *noopCache) Watermark(ctx context.Context, storeID string) (*CacheWatermark, error) {
	if c.watermark != nil {
		return c.watermark, nil
	}
	return &CacheWatermark{IsStale: false, Age: time.Minute}, nil
}

// noopEventBus records published events; Publish never fails unless
// publishErr is set. Guarded by mu: the versioned lifecycle contract
// (lifecycle_contract.go) publishes off the request goroutine via
// safego.Go, so a test's own goroutine reading .published/Events() can
// race with an in-flight async publish unless both sides serialize on the
// same lock.
type noopEventBus struct {
	mu         sync.Mutex
	published  []eventbus.DomainEvent
	publishErr error
}

func (b *noopEventBus) Publish(ctx context.Context, event eventbus.DomainEvent) error {
	b.mu.Lock()
	b.published = append(b.published, event)
	b.mu.Unlock()
	return b.publishErr
}

// Events returns a snapshot of everything published so far.
func (b *noopEventBus) Events() []eventbus.DomainEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]eventbus.DomainEvent, len(b.published))
	copy(out, b.published)
	return out
}

// waitForEventType polls Events() until one of type eventType appears or
// timeout elapses — needed because the versioned lifecycle contract's
// events are published asynchronously (R6), so they aren't necessarily
// present in Events() the instant the triggering CheckoutService call
// returns.
func waitForEventType(t *testing.T, bus *noopEventBus, eventType string, timeout time.Duration) eventbus.DomainEvent {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		for _, ev := range bus.Events() {
			if ev.EventType() == eventType {
				return ev
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for event type %q to be published", eventType)
			return nil
		}
		time.Sleep(2 * time.Millisecond)
	}
}
func (b *noopEventBus) Subscribe(ctx context.Context, topic string, handler eventbus.EventHandler) (eventbus.Subscription, error) {
	return nil, nil
}
func (b *noopEventBus) Unsubscribe(ctx context.Context, sub eventbus.Subscription) error { return nil }
func (b *noopEventBus) Close() error                                                     { return nil }

func newTestRegistry(storeID string, conn base.Connector) *base.Registry {
	reg := base.New(base.CredentialsFunc(func(context.Context, string) (map[string]any, error) {
		return nil, errors.New("no credentials in this test")
	}), slog.Default())
	reg.Set(woocommerce.PlatformName, storeID, conn)
	return reg
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, nil))
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// ─── Tests ──────────────────────────────────────────────────────────────────

func TestCheckoutService_Create_ReturnsSessionIDNotBackendID(t *testing.T) {
	const storeID = "store-1"
	conn := &testConnector{createResp: &base.CheckoutResponse{ID: "backend-999", Status: base.CheckoutStatusIncomplete}}
	sessions := NewMemorySessionStore(time.Minute)
	defer sessions.Close()
	svc := NewCheckoutService(sessions, &noopCache{}, newTestRegistry(storeID, conn), &noopEventBus{}, discardLogger())

	resp, err := svc.Create(context.Background(), &testStoreProvider{storeID: storeID}, storeID, CreateCheckoutInput{
		LineItems: []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if resp.ID == "" || resp.ID == "backend-999" {
		t.Fatalf("expected response id to be the session id, not the backend checkout id, got %q", resp.ID)
	}

	session, err := sessions.Get(context.Background(), storeID, resp.ID)
	if err != nil {
		t.Fatalf("expected response id %q to resolve to a session: %v", resp.ID, err)
	}
	if session.BackendCheckoutID != "backend-999" {
		t.Errorf("expected backend_checkout_id backend-999, got %q", session.BackendCheckoutID)
	}
}

// TestCheckoutService_Create_RetriesPersistenceBeforeGivingUp verifies that
// a transient session-store failure right after the connector confirms a
// new checkout doesn't immediately orphan it — the write is retried.
func TestCheckoutService_Create_RetriesPersistenceBeforeGivingUp(t *testing.T) {
	const storeID = "store-1"
	conn := &testConnector{createResp: &base.CheckoutResponse{ID: "backend-1", Status: base.CheckoutStatusIncomplete}}
	real := NewMemorySessionStore(time.Minute)
	defer real.Close()
	flaky := &flakySessionStore{SessionStore: real, failUpdates: 2} // fails twice, succeeds on the 3rd (final) attempt
	svc := NewCheckoutService(flaky, &noopCache{}, newTestRegistry(storeID, conn), &noopEventBus{}, discardLogger())

	resp, err := svc.Create(context.Background(), &testStoreProvider{storeID: storeID}, storeID, CreateCheckoutInput{
		LineItems: []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
	})
	if err != nil {
		t.Fatalf("Create: expected the retried persistence to eventually succeed, got error: %v", err)
	}
	if session, err := real.Get(context.Background(), storeID, resp.ID); err != nil || session.BackendCheckoutID != "backend-1" {
		t.Errorf("expected the backend checkout id to be persisted after retry, session=%+v err=%v", session, err)
	}
}

// TestCheckoutService_Update_DoesNotMutateSessionOnConnectorFailure verifies
// that a failed connector update never leaves the session store's live
// object (which the in-memory store hands back by reference) holding
// buyer/fulfillment/line_items the merchant never actually confirmed —
// a concurrent CheckoutGet must still see the pre-update state.
func TestCheckoutService_Update_DoesNotMutateSessionOnConnectorFailure(t *testing.T) {
	const storeID = "store-1"
	conn := &testConnector{updateErr: errors.New("simulated connector failure")}
	sessions := NewMemorySessionStore(time.Minute)
	defer sessions.Close()
	svc := NewCheckoutService(sessions, &noopCache{}, newTestRegistry(storeID, conn), &noopEventBus{}, discardLogger())

	original := []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}}
	seed := &CheckoutSession{
		SessionID: "sess-1", StoreID: storeID, MerchantID: storeID,
		BackendCheckoutID: "backend-1", State: SessionStateIncomplete,
		LineItems: original, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := sessions.Create(context.Background(), seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err := svc.Update(context.Background(), &testStoreProvider{storeID: storeID}, storeID, "sess-1", UpdateCheckoutInput{
		LineItems: []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-2"}, Quantity: 5}},
		Payment:   &model.Payment{},
	})
	if err == nil {
		t.Fatal("expected Update to fail when the connector rejects it")
	}

	// A concurrent (or subsequent) Get must still see the original,
	// merchant-confirmed line items — not the rejected update.
	current, err := sessions.Get(context.Background(), storeID, "sess-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(current.LineItems) != 1 || current.LineItems[0].Item.ID != "SKU-1" {
		t.Errorf("expected session to still show the original line items after a rejected update, got %+v", current.LineItems)
	}
}

// TestCheckoutService_Complete_ReportsSuccessEvenIfSessionPersistenceFails
// is the central atomicity guarantee this service exists to provide: once
// the connector confirms an order was placed at the merchant, that fact
// must never be hidden from the agent by a subsequent, unrelated failure
// to update Sarvex's own session bookkeeping — and the order-intent event
// (the durable record read back via GET /stores/{id}/orders/{id}) must
// still be emitted.
func TestCheckoutService_Complete_ReportsSuccessEvenIfSessionPersistenceFails(t *testing.T) {
	const storeID = "store-1"
	conn := &testConnector{
		completeResp: &base.CheckoutResponse{
			Status: base.CheckoutStatusCompleted,
			Totals: model.Totals{{Type: model.TotalTypeTotal, Amount: 4200}},
			Order:  &model.OrderConfirmation{ID: "order-777", PermalinkURL: "https://shop.example/order/777"},
		},
	}
	real := NewMemorySessionStore(time.Minute)
	defer real.Close()
	flaky := &flakySessionStore{SessionStore: real, failUpdates: -1} // every Update fails, forever
	events := &noopEventBus{}
	svc := NewCheckoutService(flaky, &noopCache{}, newTestRegistry(storeID, conn), events, discardLogger())

	seed := &CheckoutSession{
		SessionID: "sess-1", StoreID: storeID, MerchantID: storeID,
		BackendCheckoutID: "backend-1", State: SessionStateReadyForComplete,
		LineItems: []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := real.Create(context.Background(), seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	result, err := svc.Complete(context.Background(), &testStoreProvider{storeID: storeID}, storeID, "sess-1", &model.Payment{}, "")
	if err != nil {
		t.Fatalf("Complete: expected success to be reported even though session persistence fails, got error: %v", err)
	}
	if result.Order == nil || result.Order.ID != "order-777" {
		t.Fatalf("expected the placed order to be reported, got %+v", result)
	}

	// The order-intent event — the thing that actually makes this order
	// queryable via the control plane — must have been emitted regardless
	// of the session-store failure. Complete also emits a best-effort
	// payment_submitted lifecycle event (checkout_lifecycle_events.go)
	// right before the connector call, so two events are expected here,
	// not one; find the order-intent among them rather than assuming
	// position/count of exactly one.
	var intent *OrderIntentEvent
	published := events.Events()
	for _, ev := range published {
		if oi, ok := ev.(*OrderIntentEvent); ok {
			intent = oi
		}
	}
	if intent == nil {
		t.Fatalf("expected an *OrderIntentEvent among published events, got %+v", published)
	}
	if intent.OrderID != "order-777" {
		t.Errorf("expected order-intent order id order-777, got %q", intent.OrderID)
	}
}

// Complete must reject a handler_id the store's own profile never
// advertised — an agent has no legitimate way to end up with one, since
// FetchProfile's payment_handlers is the only source it could have read a
// handler id from. Regression test for the conformance suite's
// checkout.rejects_unknown_payment_handler check, which found that any
// handler_id — including a fabricated one — completed successfully because
// nothing validated it against what the store advertises.
func TestCheckoutService_Complete_RejectsUnadvertisedPaymentHandler(t *testing.T) {
	const storeID = "store-1"
	conn := &testConnector{
		completeResp: &base.CheckoutResponse{Status: base.CheckoutStatusCompleted},
	}
	sessions := NewMemorySessionStore(time.Minute)
	defer sessions.Close()
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

	payment := &model.Payment{Instruments: []model.SelectedPaymentInstrument{{
		PaymentInstrument: model.PaymentInstrument{HandlerID: "dev.example.not_a_real_handler"},
		Selected:          true,
	}}}

	// This store advertises no payment handlers at all (testStoreProvider's
	// default), so any handler_id — real-looking or not — must be rejected.
	_, err := svc.Complete(context.Background(), &testStoreProvider{storeID: storeID}, storeID, "sess-1", payment, "")
	if !errors.Is(err, ErrUnknownPaymentHandler) {
		t.Fatalf("expected ErrUnknownPaymentHandler, got %v", err)
	}
	// updateCalls, which this used to assert on, is a counter Complete never
	// increments — the assertion could not fail. completeCalls is the one
	// that matters: "rejected, but the card was already charged" is the
	// failure mode worth a test.
	if atomic.LoadInt32(&conn.completeCalls) != 0 {
		t.Errorf("connector must not be reached before the handler is validated")
	}
}

// The same check must let through a handler_id the store's profile does
// advertise — Complete's validation must not become a way to reject every
// checkout.
func TestCheckoutService_Complete_AllowsAdvertisedPaymentHandler(t *testing.T) {
	const storeID = "store-1"
	conn := &testConnector{
		completeResp: &base.CheckoutResponse{
			Status: base.CheckoutStatusCompleted,
			Order:  &model.OrderConfirmation{ID: "order-1"},
		},
	}
	sessions := NewMemorySessionStore(time.Minute)
	defer sessions.Close()
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

	payment := &model.Payment{Instruments: []model.SelectedPaymentInstrument{{
		PaymentInstrument: model.PaymentInstrument{HandlerID: "googlepay"},
		Selected:          true,
	}}}
	instance := &testStoreProvider{storeID: storeID, handlers: map[string][]wire.PaymentHandler{
		"com.google.pay": {{ID: "googlepay"}},
	}}

	result, err := svc.Complete(context.Background(), instance, storeID, "sess-1", payment, "")
	if err != nil {
		t.Fatalf("Complete: expected an advertised handler to be accepted, got: %v", err)
	}
	if result.Order == nil || result.Order.ID != "order-1" {
		t.Fatalf("expected the placed order to be reported, got %+v", result)
	}
}

// The freshness guard must fire on a snapshot that is genuinely behind its
// source, and only on that.
//
// It used to read `freshness.IsStale || freshness.Age > 5*time.Minute`, where
// staleness itself was time.Since(newest product's UpdatedAt) — so a store
// whose catalog had simply been steady for five minutes failed every
// checkout. That is the normal state of a healthy catalog, and the guard's
// own subject (is what we serve behind what the store has?) was never being
// asked.
func TestCheckoutComplete_FreshnessGuard(t *testing.T) {
	tests := []struct {
		name      string
		watermark *CacheWatermark
		wantBlock bool
	}{
		{
			// The case that was broken: nothing pending, so the snapshot is
			// level with the store no matter how long ago it was built.
			name:      "quiet store with an old snapshot is fresh",
			watermark: &CacheWatermark{BuiltAt: time.Now().Add(-8 * time.Hour), IsStale: false},
			wantBlock: false,
		},
		{
			name: "a change signalled and rebuilt within the window is fresh",
			watermark: &CacheWatermark{
				PendingSince: time.Now().Add(-30 * time.Second),
				Age:          30 * time.Second,
				IsStale:      false,
			},
			wantBlock: false,
		},
		{
			name: "a change left un-rebuilt past the window is stale",
			watermark: &CacheWatermark{
				PendingSince: time.Now().Add(-20 * time.Minute),
				Age:          20 * time.Minute,
				IsStale:      true,
			},
			wantBlock: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			const storeID = "store-1"
			conn := &testConnector{
				createResp:   &base.CheckoutResponse{ID: "backend-1", Status: base.CheckoutStatusIncomplete},
				completeResp: &base.CheckoutResponse{ID: "backend-1", Status: base.CheckoutStatusCompleted},
			}
			sessions := NewMemorySessionStore(time.Minute)
			defer sessions.Close()
			svc := NewCheckoutService(sessions, &noopCache{watermark: tc.watermark}, newTestRegistry(storeID, conn), &noopEventBus{}, discardLogger())

			created, err := svc.Create(context.Background(), &testStoreProvider{storeID: storeID}, storeID, CreateCheckoutInput{
				LineItems: []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
			})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}

			_, err = svc.Complete(context.Background(), &testStoreProvider{storeID: storeID}, storeID, created.ID, &model.Payment{}, "")

			blocked := errors.Is(err, ErrFreshnessGuardFailed)
			if blocked != tc.wantBlock {
				t.Errorf("blocked by freshness guard = %v, want %v (err: %v)", blocked, tc.wantBlock, err)
			}
		})
	}
}
