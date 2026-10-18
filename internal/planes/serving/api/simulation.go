// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/negotiation"
	"github.com/sarvexlab/sarvex/internal/planes/serving"
)

// simulationSecretEnvVar names the shared secret that gates the testing
// affordances below. It is read per request rather than cached so a process
// that never sets it (production) can never enable them.
const simulationSecretEnvVar = "SIMULATION_SECRET"

// SimulateShipping is a conformance/testing affordance, not a UCP endpoint:
// the official UCP conformance suite POSTs here to drive a shipment for a
// completed order. It is gated by the SIMULATION_SECRET env var — unset
// (production) disables it entirely (404); set (only `sarvex conform serve`
// does) requires the Simulation-Secret header to match, answering 403
// otherwise. Its route is registered outside the UCP API surface (handler.go)
// and appears in no published spec.
func (h *servingHandler) SimulateShipping(c echo.Context) error {
	secret := os.Getenv(simulationSecretEnvVar)
	if secret == "" {
		// No secret configured: the affordance does not exist here.
		return echo.NewHTTPError(http.StatusNotFound, "not found")
	}
	if c.Request().Header.Get("Simulation-Secret") != secret {
		return echo.NewHTTPError(http.StatusForbidden, "invalid or missing simulation secret")
	}

	instance, ok := negotiation.GetStoreProfile(c)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "instance not loaded")
	}
	orderID := c.Param("orderId")
	ctx := c.Request().Context()

	// Confirm the order exists before acknowledging a shipment against it — a
	// simulate call for an unknown order is a client error, not a silent 200.
	order, err := h.order.Get(ctx, instance.StoreID(), orderID)
	if err != nil {
		return h.mapOrderError(c, err)
	}

	// Record a "shipped" fulfillment event against every line item, driving
	// the same append-only event log a real PUT /orders/{id} would (and, via
	// OrderService.Update, the order-event webhook update delivery this
	// affordance exists to exercise).
	lineItems := make([]model.ExpectationLineItem, len(order.LineItems))
	for i, li := range order.LineItems {
		lineItems[i] = model.ExpectationLineItem{ID: li.ID, Quantity: li.Quantity.Total}
	}
	event := model.FulfillmentEvent{
		ID:             uuid.New().String(),
		OccurredAt:     time.Now().UTC().Format(time.RFC3339),
		Type:           model.FulfillmentEventTypeShipped,
		LineItems:      lineItems,
		TrackingNumber: "SIM-TRACK-" + orderID,
		TrackingURL:    fmt.Sprintf("https://example.com/track/%s", orderID),
		Carrier:        "Simulated Carrier",
	}
	if _, err := h.order.Update(ctx, instance.StoreID(), orderID, serving.OrderUpdateInput{
		FulfillmentEvents: []model.FulfillmentEvent{event},
	}); err != nil {
		return h.mapOrderError(c, err)
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "shipped", "order_id": orderID})
}
