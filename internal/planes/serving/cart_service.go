// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"

	"github.com/google/uuid"

	pkgucp "github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

// CartService implements the dev.ucp.shopping.cart capability: a lightweight
// pre-purchase basket with merchant-estimated pricing (create / get / update).
//
// Like checkout, create and update call the merchant through the sync-plane
// connector so the estimate reflects real prices, stock and discounts; Get is
// served from the last response Sarvex recorded and makes no live call, so a
// read never depends on the merchant being reachable.
type CartService struct {
	carts    CartStore
	registry *base.Registry
	logger   *slog.Logger
}

// NewCartService constructs a CartService.
func NewCartService(carts CartStore, registry *base.Registry, logger *slog.Logger) *CartService {
	if logger == nil {
		logger = slog.Default()
	}
	return &CartService{carts: carts, registry: registry, logger: logger}
}

// resolveConnector is the cart path's route to a store connector, guarded the
// same way checkout's is (see CheckoutService.resolveConnector): the live
// calls pass through, the sync plane's store-API pull methods fail loudly.
func (s *CartService) resolveConnector(instance pkgucp.StoreProvider) (base.Connector, error) {
	connector, err := s.registry.Get(instance)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConnectorUnavailable, err)
	}
	return blockPullConnector{Connector: connector, storeID: instance.Configuration().StoreID}, nil
}

// CreateCartInput is the caller-supplied portion of a new cart.
type CreateCartInput struct {
	LineItems      []base.LineItemCreateRequest
	Buyer          *model.Buyer
	Context        *model.Context
	Discounts      *model.DiscountsObject
	IdempotencyKey string
	BodyHash       string
}

// UpdateCartInput replaces the cart's state (PUT). A nil Buyer/Discounts leaves
// that part unchanged; a non-nil Discounts (even with no codes) replaces the
// prior codes.
type UpdateCartInput struct {
	LineItems []base.LineItemCreateRequest
	Buyer     *model.Buyer
	Discounts *model.DiscountsObject
}

// Create opens a cart at the merchant and returns it under an opaque Sarvex id.
func (s *CartService) Create(ctx context.Context, instance pkgucp.StoreProvider, storeID string, in CreateCartInput) (*base.CartResponse, error) {
	if len(in.LineItems) == 0 {
		return nil, ErrLineItemsRequired
	}
	cfg := instance.Configuration()

	now := time.Now().UTC()
	rec := &CartRecord{
		CartID:         uuid.NewString(),
		StoreID:        storeID,
		MerchantID:     cfg.MerchantID,
		LineItems:      in.LineItems,
		Buyer:          in.Buyer,
		Discounts:      in.Discounts,
		IdempotencyKey: in.IdempotencyKey,
		BodyHash:       in.BodyHash,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	// Reserve the record (and idempotency key) before the connector call, so a
	// retry racing the original cannot open a second cart at the merchant.
	stored, replayed, err := s.carts.Create(ctx, rec)
	if err != nil {
		return nil, err
	}
	if replayed {
		if stored.LastResponse == nil {
			return nil, ErrCartNotReady
		}
		return stored.LastResponse, nil
	}

	connector, err := s.resolveConnector(instance)
	if err != nil {
		return nil, err
	}
	connResp, err := connector.CartCreate(ctx, storeID, &base.CreateCartRequest{
		LineItems: in.LineItems,
		Buyer:     in.Buyer,
		Context:   in.Context,
		Discounts: in.Discounts,
	})
	if err != nil {
		return nil, err
	}
	return s.record(ctx, rec, connResp, "create")
}

// Get returns the cart as last confirmed by the merchant.
func (s *CartService) Get(ctx context.Context, storeID, cartID string) (*base.CartResponse, error) {
	rec, err := s.carts.Get(ctx, storeID, cartID)
	if err != nil {
		return nil, err
	}
	if rec.LastResponse == nil {
		return nil, ErrCartNotReady
	}
	return rec.LastResponse, nil
}

// Update replaces the cart's line items (and buyer/discounts when supplied) and
// returns the merchant's re-priced estimate.
func (s *CartService) Update(ctx context.Context, instance pkgucp.StoreProvider, storeID, cartID string, in UpdateCartInput) (*base.CartResponse, error) {
	if len(in.LineItems) == 0 {
		return nil, ErrLineItemsRequired
	}
	rec, err := s.carts.Get(ctx, storeID, cartID)
	if err != nil {
		return nil, err
	}
	if rec.BackendCartID == "" {
		return nil, ErrCartNotReady
	}

	updated := *rec
	updated.LineItems = in.LineItems
	if in.Buyer != nil {
		updated.Buyer = in.Buyer
	}
	if in.Discounts != nil {
		updated.Discounts = in.Discounts
	}

	connector, err := s.resolveConnector(instance)
	if err != nil {
		return nil, err
	}
	req := &base.UpdateCartRequest{Buyer: updated.Buyer, Discounts: updated.Discounts}
	req.LineItems = make([]base.LineItemUpdateRequest, len(updated.LineItems))
	for i, li := range updated.LineItems {
		req.LineItems[i] = base.LineItemUpdateRequest{Item: li.Item, Quantity: li.Quantity}
	}
	connResp, err := connector.CartUpdate(ctx, storeID, rec.BackendCartID, req)
	if err != nil {
		return nil, err
	}
	return s.record(ctx, &updated, connResp, "update")
}

// Seed returns the cart's request state — what a checkout created from it
// (cart_id) starts from.
func (s *CartService) Seed(ctx context.Context, storeID, cartID string) (*CartRecord, error) {
	return s.carts.Get(ctx, storeID, cartID)
}

// record stamps the merchant's response with the agent-facing id and persists
// it. The merchant call has already succeeded, so a bookkeeping failure is
// retried and then surfaced rather than silently dropped: without the record
// the agent would hold a cart id Sarvex cannot resolve.
func (s *CartService) record(ctx context.Context, rec *CartRecord, connResp *base.CartResponse, op string) (*base.CartResponse, error) {
	// A connector may answer with messages and no cart (nothing was created);
	// pass that through without binding an empty backend id.
	if connResp == nil || connResp.ID == "" {
		return connResp, nil
	}

	backendID := connResp.ID
	resp := *connResp
	resp.ID = rec.CartID

	rec.BackendCartID = backendID
	rec.LastResponse = &resp
	rec.UpdatedAt = time.Now().UTC()

	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		if err = s.carts.Update(ctx, rec); err == nil {
			return &resp, nil
		}
		s.logger.Warn("cart persistence failed, retrying", "op", op, "attempt", attempt, "cart_id", rec.CartID, "error", err)
		select {
		case <-time.After(time.Duration(attempt) * 50 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, fmt.Errorf("persist cart after merchant confirmed %q: %w", op, err)
}
