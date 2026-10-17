// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

// cartConnector scripts the cart methods over the shared checkout test stub.
type cartConnector struct {
	*testConnector
	createResp, updateResp *base.CartResponse
	createErr, updateErr   error
	created                int
	lastCreate             *base.CreateCartRequest
	lastUpdate             *base.UpdateCartRequest
	lastUpdateID           string
}

func (c *cartConnector) CartCreate(_ context.Context, _ string, req *base.CreateCartRequest) (*base.CartResponse, error) {
	c.created++
	c.lastCreate = req
	return c.createResp, c.createErr
}

func (c *cartConnector) CartUpdate(_ context.Context, _ string, id string, req *base.UpdateCartRequest) (*base.CartResponse, error) {
	c.lastUpdateID, c.lastUpdate = id, req
	return c.updateResp, c.updateErr
}

func newCartTest(conn *cartConnector) (*CartService, CartStore) {
	store := NewMemoryCartStore(time.Hour)
	return NewCartService(store, newTestRegistry("store-1", conn), discardLogger()), store
}

var oneItem = []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 2}}

func TestCartCreate_ReturnsOpaqueIDNotTheMerchants(t *testing.T) {
	conn := &cartConnector{testConnector: &testConnector{}, createResp: &base.CartResponse{ID: "4242", Currency: "USD"}}
	svc, store := newCartTest(conn)

	resp, err := svc.Create(context.Background(), &testStoreProvider{storeID: "store-1"}, "store-1", CreateCartInput{LineItems: oneItem})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if resp.ID == "" || resp.ID == "4242" {
		t.Fatalf("agent must get an opaque Sarvex id, not the backend order id: %q", resp.ID)
	}
	if resp.Currency != "USD" {
		t.Errorf("currency dropped: %q", resp.Currency)
	}
	rec, err := store.Get(context.Background(), "store-1", resp.ID)
	if err != nil || rec.BackendCartID != "4242" {
		t.Fatalf("record = %+v, %v; want backend id 4242 bound to the opaque id", rec, err)
	}
}

func TestCartCreate_RequiresLineItems(t *testing.T) {
	svc, _ := newCartTest(&cartConnector{testConnector: &testConnector{}})
	_, err := svc.Create(context.Background(), &testStoreProvider{storeID: "store-1"}, "store-1", CreateCartInput{})
	if !errors.Is(err, ErrLineItemsRequired) {
		t.Fatalf("err = %v, want ErrLineItemsRequired", err)
	}
}

func TestCartCreate_IdempotentReplayDoesNotOpenASecondCart(t *testing.T) {
	conn := &cartConnector{testConnector: &testConnector{}, createResp: &base.CartResponse{ID: "4242"}}
	svc, _ := newCartTest(conn)
	in := CreateCartInput{LineItems: oneItem, IdempotencyKey: "k1", BodyHash: "h1"}

	first, err := svc.Create(context.Background(), &testStoreProvider{storeID: "store-1"}, "store-1", in)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Create(context.Background(), &testStoreProvider{storeID: "store-1"}, "store-1", in)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if conn.created != 1 || second.ID != first.ID {
		t.Fatalf("replay must return the original cart without a second merchant call (calls=%d ids %q/%q)", conn.created, first.ID, second.ID)
	}

	in.BodyHash = "different"
	if _, err := svc.Create(context.Background(), &testStoreProvider{storeID: "store-1"}, "store-1", in); !errors.Is(err, ErrIdempotencyKeyConflict) {
		t.Fatalf("same key, different body: err = %v, want ErrIdempotencyKeyConflict", err)
	}
}

func TestCartCreate_ConnectorRejectionPassesThrough(t *testing.T) {
	rej := &base.CheckoutRejectedError{Code: "out_of_stock", Detail: "no stock"}
	svc, _ := newCartTest(&cartConnector{testConnector: &testConnector{}, createErr: rej})
	_, err := svc.Create(context.Background(), &testStoreProvider{storeID: "store-1"}, "store-1", CreateCartInput{LineItems: oneItem})
	var got *base.CheckoutRejectedError
	if !errors.As(err, &got) || got.Code != "out_of_stock" {
		t.Fatalf("err = %v, want the business rejection intact", err)
	}
}

func TestCartGet_ServesRecordedResponseAndIsStoreFenced(t *testing.T) {
	conn := &cartConnector{testConnector: &testConnector{}, createResp: &base.CartResponse{ID: "4242"}}
	svc, _ := newCartTest(conn)
	created, err := svc.Create(context.Background(), &testStoreProvider{storeID: "store-1"}, "store-1", CreateCartInput{LineItems: oneItem})
	if err != nil {
		t.Fatal(err)
	}

	got, err := svc.Get(context.Background(), "store-1", created.ID)
	if err != nil || got.ID != created.ID {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	if _, err := svc.Get(context.Background(), "store-2", created.ID); !errors.Is(err, ErrCartNotFound) {
		t.Fatalf("another store's cart id must be not found, got %v", err)
	}
	if _, err := svc.Get(context.Background(), "store-1", "4242"); !errors.Is(err, ErrCartNotFound) {
		t.Fatalf("the merchant's own cart id must not resolve, got %v", err)
	}
}

func TestCartUpdate_ReplacesLineItemsAtTheBackendCart(t *testing.T) {
	conn := &cartConnector{
		testConnector: &testConnector{},
		createResp:    &base.CartResponse{ID: "4242"},
		updateResp:    &base.CartResponse{ID: "4242", Currency: "USD"},
	}
	svc, _ := newCartTest(conn)
	inst := &testStoreProvider{storeID: "store-1"}
	created, _ := svc.Create(context.Background(), inst, "store-1", CreateCartInput{LineItems: oneItem})

	codes := &model.DiscountsObject{Codes: []string{"SPRING20"}}
	next := []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-2"}, Quantity: 5}}
	resp, err := svc.Update(context.Background(), inst, "store-1", created.ID, UpdateCartInput{
		LineItems: next, Discounts: codes, Buyer: &model.Buyer{Email: "b@example.com"},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if conn.lastUpdateID != "4242" {
		t.Errorf("connector addressed %q, want the backend cart 4242", conn.lastUpdateID)
	}
	if len(conn.lastUpdate.LineItems) != 1 || conn.lastUpdate.LineItems[0].Item.ID != "SKU-2" || conn.lastUpdate.LineItems[0].Quantity != 5 {
		t.Errorf("line items not forwarded: %+v", conn.lastUpdate.LineItems)
	}
	if conn.lastUpdate.Discounts != codes || conn.lastUpdate.Buyer == nil {
		t.Errorf("discounts/buyer not forwarded: %+v", conn.lastUpdate)
	}
	if resp.ID != created.ID {
		t.Errorf("response id = %q, want the opaque id %q", resp.ID, created.ID)
	}

	seed, err := svc.Seed(context.Background(), "store-1", created.ID)
	if err != nil || len(seed.LineItems) != 1 || seed.LineItems[0].Item.ID != "SKU-2" {
		t.Errorf("Seed after update = %+v, %v; want the replaced line items", seed, err)
	}
}

func TestCartUpdate_UnknownCartAndEmptyItems(t *testing.T) {
	svc, _ := newCartTest(&cartConnector{testConnector: &testConnector{}})
	inst := &testStoreProvider{storeID: "store-1"}
	if _, err := svc.Update(context.Background(), inst, "store-1", "nope", UpdateCartInput{LineItems: oneItem}); !errors.Is(err, ErrCartNotFound) {
		t.Errorf("unknown cart: err = %v", err)
	}
	if _, err := svc.Update(context.Background(), inst, "store-1", "nope", UpdateCartInput{}); !errors.Is(err, ErrLineItemsRequired) {
		t.Errorf("empty line items: err = %v", err)
	}
}
