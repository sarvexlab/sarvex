// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlab/sarvex/internal/pkg/middlewares"
	"github.com/sarvexlab/sarvex/internal/planes/serving"
)

// This file holds the orders endpoint's wire contract only — the request
// body and the domain-error -> HTTP-status mapping. OrderGet/OrderUpdate
// (handler.go) only bind, call serving.OrderService, and write the result;
// the response body is the canonical model.Order the service already
// builds.

// orderFulfillmentUpdate is the fulfillment portion of an order PUT — only
// Events is ever caller-supplied; Expectations are fixed at Complete and
// are never accepted here.
type orderFulfillmentUpdate struct {
	Events []model.FulfillmentEvent `json:"events,omitempty"`
}

// OrderUpdateRequest is the request body for PUT /orders/{id}. Both fields
// are append-only logs (see serving.OrderService.Update) — nothing about a
// placed order's line items, currency, or fulfillment expectations can
// change via this endpoint.
type OrderUpdateRequest struct {
	Fulfillment *orderFulfillmentUpdate `json:"fulfillment,omitempty"`
	Adjustments []model.Adjustment      `json:"adjustments,omitempty"`
}

// toInput validates the request and narrows it to serving.OrderUpdateInput.
// Validation lives here, not in the service, because it is about the wire
// shape (an adjustment naming a status the spec doesn't define), not the
// order's own state — the same separation checkout_contract.go keeps for
// checkout requests.
func (r OrderUpdateRequest) toInput() (serving.OrderUpdateInput, error) {
	for _, adj := range r.Adjustments {
		// Type is an open string by design (AdjustmentType's doc comment) —
		// only Status is a closed enum, so only it is rejected when unknown.
		if adj.Status != "" && !adj.Status.Known() {
			return serving.OrderUpdateInput{}, fmt.Errorf("adjustment %q: unknown status %q", adj.ID, adj.Status)
		}
	}
	for _, ev := range r.Fulfillment.events() {
		if err := ev.Validate(); err != nil {
			return serving.OrderUpdateInput{}, err
		}
	}

	return serving.OrderUpdateInput{
		FulfillmentEvents: r.Fulfillment.events(),
		Adjustments:       r.Adjustments,
	}, nil
}

// events returns f's events, or nil for a nil *orderFulfillmentUpdate — so
// callers never need a nil check before ranging.
func (f *orderFulfillmentUpdate) events() []model.FulfillmentEvent {
	if f == nil {
		return nil
	}
	return f.Events
}

// mapOrderError translates a serving.OrderService error into an HTTP
// status. Every orders endpoint funnels its error through here so the
// status-code policy lives in exactly one place, mirroring
// mapCheckoutError.
func (h *servingHandler) mapOrderError(c echo.Context, err error) error {
	cid := middlewares.GetCorrelationID(c)

	switch {
	case errors.Is(err, serving.ErrOrderNotFound):
		h.logger.Debug("order error: not found", "correlation_id", cid)
		return echo.NewHTTPError(http.StatusNotFound, "order not found")
	default:
		h.logger.Error("order request failed", "correlation_id", cid, "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "internal error")
	}
}
