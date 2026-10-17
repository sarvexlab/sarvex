// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/paymenthandler"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/store"
)

// paymentHandlerView is one handler in the payment_config response
// (sarvex-v1-control-plane.yaml). It carries configuration STATUS only — never
// the parameters themselves: the contract is that a store's payment
// configuration is write-only.
type paymentHandlerView struct {
	HandlerID    string `json:"handler_id"`
	Name         string `json:"name"`
	Enabled      bool   `json:"enabled"`
	Configured   bool   `json:"configured"`
	ConfigStatus string `json:"config_status"`
}

type paymentConfigView struct {
	StoreID  domain.StoreID       `json:"store_id"`
	Handlers []paymentHandlerView `json:"handlers"`
}

func paymentConfigToView(storeID domain.StoreID, m *store.PaymentConfigModel) paymentConfigView {
	view := paymentConfigView{StoreID: storeID, Handlers: []paymentHandlerView{}}
	if m == nil {
		return view
	}
	for key, h := range m.Handlers {
		spec, ok := paymenthandler.Lookup(key)
		if !ok {
			// A handler Sarvex has no spec for cannot be advertised or
			// completed against, so it is not reported as configured.
			continue
		}
		status := spec.Status(h.Enabled, h.Config)
		view.Handlers = append(view.Handlers, paymentHandlerView{
			HandlerID:    spec.ID,
			Name:         string(spec.Name),
			Enabled:      h.Enabled,
			Configured:   status == paymenthandler.StatusReady,
			ConfigStatus: status,
		})
	}
	sort.Slice(view.Handlers, func(i, j int) bool { return view.Handlers[i].HandlerID < view.Handlers[j].HandlerID })
	return view
}

// GET /stores/:store_id/payment-config — retrieve payment handler configuration status for a store.
func (h *handler) getPaymentConfig(c echo.Context) error {
	principal, ok := c.Get("principal").(domain.Principal)
	if !ok {
		return errUnauthorized(c)
	}

	storeID := domain.StoreID(c.Param("store_id"))
	if storeID == "" {
		return errValidation(c, map[string]string{"store_id": "required"})
	}

	// Get payment config (may not exist, that's okay). Fenced on merchant_id in
	// the repository query, so another tenant's store reads as unconfigured.
	config, err := h.deps.PaymentConfigRepo.Get(c.Request().Context(), principal.MerchantID, storeID)
	if err != nil && !errors.Is(err, store.ErrPaymentConfigNotFound) {
		h.log.Error("failed to get payment config", "error", err, "merchant_id", principal.MerchantID, "store_id", storeID)
		return errInternal(c)
	}
	if errors.Is(err, store.ErrPaymentConfigNotFound) {
		config = nil
	}

	return c.JSON(http.StatusOK, paymentConfigToView(storeID, config))
}

// paymentConfigUpdatedEvent is emitted when payment handler config changes. It
// names the affected handlers but carries no parameters.
type paymentConfigUpdatedEvent struct {
	storeID    string
	merchantID string
	handlers   []paymentHandlerView
	updatedBy  string
	occurredAt time.Time
	payload    []byte
}

func (e *paymentConfigUpdatedEvent) EventType() string {
	return ucp.EventPaymentConfigUpdated
}

func (e *paymentConfigUpdatedEvent) StoreID() string {
	return e.storeID
}

func (e *paymentConfigUpdatedEvent) MerchantID() string {
	return e.merchantID
}

func (e *paymentConfigUpdatedEvent) OccurredAt() time.Time {
	return e.occurredAt
}

func (e *paymentConfigUpdatedEvent) Payload() []byte {
	if e.payload == nil {
		body, _ := json.Marshal(map[string]interface{}{
			"store_id":    e.storeID,
			"merchant_id": e.merchantID,
			"handlers":    e.handlers,
			"updated_by":  e.updatedBy,
			"occurred_at": e.occurredAt,
		})
		e.payload = body
	}
	return e.payload
}

// PUT /stores/:store_id/payment-config — update payment handler configuration for a store.
type UpdatePaymentConfigRequest struct {
	Handlers []UpdatePaymentConfigHandler `json:"handlers"`
}

// UpdatePaymentConfigHandler is one handler in the payment_config_update body.
// Parameters are the handler's public parameters; anything the handler does not
// declare public is rejected rather than stored.
type UpdatePaymentConfigHandler struct {
	HandlerID  string         `json:"handler_id"`
	Enabled    bool           `json:"enabled"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

// stringParams converts a request's parameters to strings; a non-string value
// is a validation error rather than a silent coercion.
func stringParams(in map[string]any) (map[string]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("parameter %q must be a string", k)
		}
		out[k] = s
	}
	return out, nil
}

func (h *handler) updatePaymentConfig(c echo.Context) error {
	principal, ok := c.Get("principal").(domain.Principal)
	if !ok {
		return errUnauthorized(c)
	}

	storeID := domain.StoreID(c.Param("store_id"))
	if storeID == "" {
		return errValidation(c, map[string]string{"store_id": "required"})
	}

	var req UpdatePaymentConfigRequest
	if err := c.Bind(&req); err != nil {
		return errValidation(c, map[string]string{"body": "invalid request body"})
	}

	ctx := c.Request().Context()

	// The store must exist and belong to the caller before anything is written
	// under its id. Store.Get is fenced on merchant_id (ADR-2); without this a
	// merchant could plant a payment config under another merchant's store id,
	// after which that merchant's own writes would collide on the primary key.
	if h.deps.Store == nil {
		return errInternal(c)
	}
	st, err := h.deps.Store.Get(ctx, principal.MerchantID, storeID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return errNotFound(c)
		}
		h.log.Error("payment config store lookup failed", "error", err, "merchant_id", principal.MerchantID, "store_id", storeID)
		return errInternal(c)
	}

	handlers := make(map[string]store.PaymentHandlerConfigModel, len(req.Handlers))
	enabled := make([]domain.EnabledHandler, 0, len(req.Handlers))
	for _, rh := range req.Handlers {
		spec, known := paymenthandler.Lookup(rh.HandlerID)
		if !known {
			return errValidation(c, map[string]string{"handlers": fmt.Sprintf("invalid payment handler: %s", rh.HandlerID)})
		}
		if _, dup := handlers[string(spec.Name)]; dup {
			return errValidation(c, map[string]string{"handlers": fmt.Sprintf("payment handler %s listed more than once", spec.ID)})
		}
		if rh.Enabled && !spec.AppliesTo(string(st.Platform)) {
			return errValidation(c, map[string]string{"handlers": fmt.Sprintf("payment handler %s cannot be used on a %s store", spec.ID, st.Platform)})
		}
		params, err := stringParams(rh.Parameters)
		if err != nil {
			return errValidation(c, map[string]string{"handlers": err.Error()})
		}
		handlers[string(spec.Name)] = store.PaymentHandlerConfigModel{Enabled: rh.Enabled, Config: params}
		enabled = append(enabled, domain.EnabledHandler{HandlerID: string(spec.Name), Enabled: rh.Enabled, Config: params})
	}

	// Validate handlers and their parameters against the catalogue and specs.
	config := &domain.PaymentHandlerConfig{
		StoreID:   storeID,
		Handlers:  enabled,
		UpdatedAt: time.Now().UTC(),
		UpdatedBy: principal.Email,
	}
	if h.deps.PaymentValidator != nil {
		if err := h.deps.PaymentValidator.ValidateConfig(config); err != nil {
			h.log.Warn("payment config validation failed", "error", err, "merchant_id", principal.MerchantID, "store_id", storeID)
			return errValidation(c, map[string]string{"handlers": err.Error()})
		}
	}

	// Upsert configuration
	model := &store.PaymentConfigModel{
		ID:         string(storeID),
		MerchantID: string(principal.MerchantID),
		Handlers:   handlers,
		UpdatedBy:  principal.Email,
	}

	if err := h.deps.PaymentConfigRepo.Upsert(ctx, model); err != nil {
		h.log.Error("failed to update payment config", "error", err, "merchant_id", principal.MerchantID, "store_id", storeID)
		return errInternal(c)
	}

	view := paymentConfigToView(storeID, model)

	// Emit payment config updated event so the serving plane rebuilds the
	// store's cached profile.
	if h.deps.EventPublisher != nil {
		event := &paymentConfigUpdatedEvent{
			storeID:    string(storeID),
			merchantID: string(principal.MerchantID),
			handlers:   view.Handlers,
			updatedBy:  principal.Email,
			occurredAt: time.Now().UTC(),
		}
		if err := h.deps.EventPublisher.Publish(ctx, event); err != nil {
			h.log.Error("failed to emit payment config updated event", "error", err, "store_id", storeID)
			// Non-fatal: configuration is persisted, just log the event emission failure
		}
	}

	h.log.Info("payment config updated", "merchant_id", principal.MerchantID, "store_id", storeID, "handlers", len(req.Handlers))

	return c.JSON(http.StatusOK, view)
}
