// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"golang.org/x/sync/errgroup"

	"github.com/sarvexlab/sarvex/internal/planes/control/account"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/store"
)

func (h *handler) operatorBoard(c echo.Context) error {
	// Parse query parameters.
	state := c.QueryParam("state")
	attention := c.QueryParam("attention") == "true"
	cursor := c.QueryParam("cursor")
	limit := 20
	if l := c.QueryParam("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	// Convert state parameter.
	var stateFilter *domain.JourneyState
	if state != "" {
		stateFilter = (*domain.JourneyState)(&state)
	}

	attentionPtr := &attention

	result, err := h.deps.Operator.Board(c.Request().Context(), stateFilter, attentionPtr, cursor, limit)
	if err != nil {
		h.log.Error("operatorBoard failed", "error", err)
		return errInternal(c)
	}
	return c.JSON(http.StatusOK, result)
}

func (h *handler) captureManualStep(c echo.Context) error {
	// Require X-Operate-As header.
	targetMerchantStr := c.Request().Header.Get("X-Operate-As")
	if targetMerchantStr == "" {
		return c.JSON(http.StatusBadRequest, domain.ErrorEnvelope{
			Error: domain.ErrorDetail{
				Code:    "missing_target",
				Message: "X-Operate-As header required",
			},
		})
	}
	targetMerchant := domain.MerchantID(targetMerchantStr)

	// Get operator ID from principal.
	principal := principalFrom(c)

	// Parse request body.
	var body domain.ManualStep
	if err := c.Bind(&body); err != nil {
		return errValidation(c, map[string]string{"body": "invalid JSON"})
	}

	// Validate description.
	if body.Description == "" {
		return errValidation(c, map[string]string{"description": "required"})
	}

	result, err := h.deps.Operator.CaptureManualStep(c.Request().Context(), principal.UserID, targetMerchant, body)
	if err != nil {
		h.log.Error("captureManualStep failed", "error", err, "target", targetMerchant)
		return errInternal(c)
	}
	return c.JSON(http.StatusCreated, result)
}

func (h *handler) auditLog(c echo.Context) error {
	// Parse query parameters.
	targetMerchantStr := c.QueryParam("target_merchant_id")
	var targetMerchant *domain.MerchantID
	if targetMerchantStr != "" {
		m := domain.MerchantID(targetMerchantStr)
		targetMerchant = &m
	}

	cursor := c.QueryParam("cursor")
	limit := 20
	if l := c.QueryParam("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	result, err := h.deps.Operator.AuditLog(c.Request().Context(), targetMerchant, cursor, limit)
	if err != nil {
		h.log.Error("auditLog failed", "error", err)
		return errInternal(c)
	}
	return c.JSON(http.StatusOK, result)
}

func (h *handler) actOnBehalf(c echo.Context) error {
	// Require X-Operate-As header.
	targetMerchantStr := c.Request().Header.Get("X-Operate-As")
	if targetMerchantStr == "" {
		return c.JSON(http.StatusBadRequest, domain.ErrorEnvelope{
			Error: domain.ErrorDetail{
				Code:    "missing_target",
				Message: "X-Operate-As header required",
			},
		})
	}
	targetMerchant := domain.MerchantID(targetMerchantStr)

	// Get operator ID from principal.
	principal := principalFrom(c)

	// Parse request body.
	var body struct {
		Action string                 `json:"action"`
		Params map[string]interface{} `json:"params"`
	}
	if err := c.Bind(&body); err != nil {
		return errValidation(c, map[string]string{"body": "invalid JSON"})
	}

	if body.Action == "" {
		return errValidation(c, map[string]string{"action": "required"})
	}

	err := h.deps.Operator.ActOnBehalf(c.Request().Context(), principal.UserID, targetMerchant, body.Action, body.Params)
	if err != nil {
		h.log.Error("actOnBehalf failed", "error", err, "action", body.Action, "target", targetMerchant)
		return errInternal(c)
	}

	return c.JSON(http.StatusOK, map[string]string{
		"status":  "success",
		"action":  body.Action,
		"target":  string(targetMerchant),
		"message": "operator action executed",
	})
}

func (h *handler) listManualSteps(c echo.Context) error {
	// Parse query parameters.
	disposition := c.QueryParam("disposition")
	cursor := c.QueryParam("cursor")
	limit := 20
	if l := c.QueryParam("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	var dispositionFilter *domain.ManualStepDisposition
	if disposition != "" {
		d := domain.ManualStepDisposition(disposition)
		dispositionFilter = &d
	}

	result, err := h.deps.Operator.ListManualSteps(c.Request().Context(), dispositionFilter, cursor, limit)
	if err != nil {
		h.log.Error("listManualSteps failed", "error", err)
		return errInternal(c)
	}
	return c.JSON(http.StatusOK, result)
}

func (h *handler) retagManualStep(c echo.Context) error {
	stepID := c.Param("step_id")
	if stepID == "" {
		return errValidation(c, map[string]string{"step_id": "required"})
	}

	principal := principalFrom(c)

	var body struct {
		Disposition domain.ManualStepDisposition `json:"disposition"`
	}
	if err := c.Bind(&body); err != nil {
		return errValidation(c, map[string]string{"body": "invalid JSON"})
	}

	if body.Disposition == "" {
		return errValidation(c, map[string]string{"disposition": "required"})
	}

	err := h.deps.Operator.RetagManualStep(c.Request().Context(), principal.UserID, stepID, body.Disposition)
	if err != nil {
		h.log.Error("retagManualStep failed", "error", err, "step_id", stepID)
		return errInternal(c)
	}

	return c.JSON(http.StatusOK, map[string]string{
		"status":      "success",
		"step_id":     stepID,
		"disposition": string(body.Disposition),
	})
}

func (h *handler) bulkRetagManualSteps(c echo.Context) error {
	principal := principalFrom(c)

	var body struct {
		StepIDs     []string                     `json:"step_ids"`
		Disposition domain.ManualStepDisposition `json:"disposition"`
	}
	if err := c.Bind(&body); err != nil {
		return errValidation(c, map[string]string{"body": "invalid JSON"})
	}

	if len(body.StepIDs) == 0 {
		return errValidation(c, map[string]string{"step_ids": "required (non-empty array)"})
	}

	if body.Disposition == "" {
		return errValidation(c, map[string]string{"disposition": "required"})
	}

	err := h.deps.Operator.BulkRetagManualSteps(c.Request().Context(), principal.UserID, body.StepIDs, body.Disposition)
	if err != nil {
		h.log.Error("bulkRetagManualSteps failed", "error", err, "count", len(body.StepIDs))
		return errInternal(c)
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"status":         "success",
		"steps_retagged": len(body.StepIDs),
		"disposition":    string(body.Disposition),
	})
}

// ── Operator merchant provisioning ─────────────────────────────────────────────
// Architecture §3 Domain 1: the bootstrap sequence that cannot use X-Operate-As
// because the merchant does not yet exist when these endpoints are called.

func (h *handler) listMerchants(c echo.Context) error {
	cursor := c.QueryParam("cursor")
	limit := 20
	if l := c.QueryParam("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}
	result, err := h.deps.Account.List(c.Request().Context(), cursor, limit)
	if err != nil {
		h.log.Error("listMerchants failed", "error", err)
		return errInternal(c)
	}
	return c.JSON(http.StatusOK, result)
}

func (h *handler) createMerchant(c echo.Context) error {
	var body struct {
		Name          string                `json:"name"`
		OwnerEmail    string                `json:"owner_email,omitempty"`
		BusinessFacts *domain.BusinessFacts `json:"business_facts,omitempty"`
	}
	if err := c.Bind(&body); err != nil {
		return errValidation(c, "invalid request body")
	}
	if body.Name == "" {
		return errValidation(c, "name is required")
	}
	if body.OwnerEmail == "" {
		return errValidation(c, "owner_email is required")
	}
	acc, err := h.deps.Account.Create(c.Request().Context(), body.Name, body.BusinessFacts, body.OwnerEmail)
	if err != nil {
		h.log.Error("createMerchant failed", "error", err)
		return errInternal(c)
	}
	if _, err := h.deps.Account.InviteMember(c.Request().Context(), acc.MerchantID, body.OwnerEmail, domain.MemberRoleOwner); err != nil {
		h.log.Error("createMerchant: invite owner failed", "error", err, "merchant_id", acc.MerchantID)
		return errInternal(c)
	}
	return c.JSON(http.StatusCreated, h.accountToResponse(c.Request().Context(), acc))
}

func (h *handler) operatorInviteMember(c echo.Context) error {
	merchantID := domain.MerchantID(c.Param("merchant_id"))
	if merchantID == "" {
		return errValidation(c, "merchant_id is required")
	}
	var body struct {
		Email string            `json:"email"`
		Role  domain.MemberRole `json:"role"`
	}
	if err := c.Bind(&body); err != nil {
		return errValidation(c, "invalid request body")
	}
	if body.Email == "" {
		return errValidation(c, "email is required")
	}
	if body.Role == "" {
		body.Role = domain.MemberRoleOwner
	}
	member, err := h.deps.Account.InviteMember(c.Request().Context(), merchantID, body.Email, body.Role)
	if err != nil {
		if errors.Is(err, account.ErrNotFound) {
			return errNotFound(c)
		}
		if errors.Is(err, account.ErrMemberAlreadyActive) {
			return c.JSON(http.StatusConflict, domain.ErrorEnvelope{
				Error: domain.ErrorDetail{
					Code:    "member_already_active",
					Message: "member is already active for this merchant",
				},
			})
		}
		h.log.Error("operatorInviteMember failed", "error", err, "merchant_id", merchantID)
		return errInternal(c)
	}
	return c.JSON(http.StatusCreated, member)
}

func (h *handler) operatorListMembers(c echo.Context) error {
	merchantID := domain.MerchantID(c.Param("merchant_id"))
	if merchantID == "" {
		return errValidation(c, "merchant_id is required")
	}
	result, err := h.deps.Account.ListMembers(c.Request().Context(), merchantID, "", 50)
	if err != nil {
		h.log.Error("operatorListMembers failed", "error", err, "merchant_id", merchantID)
		return errInternal(c)
	}
	return c.JSON(http.StatusOK, result)
}

func (h *handler) operatorListStores(c echo.Context) error {
	merchantID := domain.MerchantID(c.Param("merchant_id"))
	if merchantID == "" {
		return errValidation(c, "merchant_id is required")
	}
	ctx := c.Request().Context()
	result, err := h.deps.Store.List(ctx, merchantID, "", 100)
	if err != nil {
		h.log.Error("operatorListStores failed", "error", err, "merchant_id", merchantID)
		return errInternal(c)
	}
	responses := make([]storeResponse, len(result.Data))
	var g errgroup.Group
	for i, s := range result.Data {
		g.Go(func() error {
			domainURL, openTaskCount, blocked, journeyState, ucpState := h.storeEnrichment(ctx, merchantID, s.StoreID)
			responses[i] = storeToResponse(&s, domainURL, openTaskCount, blocked, journeyState, ucpState)
			return nil
		})
	}
	_ = g.Wait()
	return c.JSON(http.StatusOK, map[string]any{
		"data": responses,
		"page": result.Page,
	})
}

func (h *handler) operatorCreateStore(c echo.Context) error {
	merchantID := domain.MerchantID(c.Param("merchant_id"))
	if merchantID == "" {
		return errValidation(c, "merchant_id is required")
	}
	var body struct {
		Name     string          `json:"name"`
		Platform domain.Platform `json:"platform"`
	}
	if err := c.Bind(&body); err != nil {
		return errValidation(c, "invalid request body")
	}
	if body.Name == "" {
		return errValidation(c, "name is required")
	}
	if body.Platform == "" {
		return errValidation(c, "platform is required")
	}
	s, err := h.deps.Store.Create(c.Request().Context(), merchantID, body.Name, body.Platform)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return errNotFound(c)
		}
		h.log.Error("operatorCreateStore failed", "error", err, "merchant_id", merchantID)
		return errInternal(c)
	}

	// Initialize journey for the new store (pre-pairing state).
	if err := h.deps.Journey.Initialize(c.Request().Context(), merchantID, s.StoreID); err != nil {
		h.log.Error("failed to initialize journey", "error", err, "merchant_id", merchantID, "store_id", s.StoreID)
		// Don't fail store creation if journey initialization fails; store is created successfully
	}

	// Activation starts at draft. It is a separate lifecycle from the
	// journey, so it is seeded here in its own right rather than derived
	// from the journey record — and like the journey, a failure to seed it
	// never fails store creation.
	if h.deps.Activation != nil {
		if err := h.deps.Activation.Initialize(c.Request().Context(), merchantID, s.StoreID); err != nil {
			h.log.Error("failed to initialize activation", "error", err, "merchant_id", merchantID, "store_id", s.StoreID)
		}
	}

	// Seed an empty business profile so GET /stores/:store_id/profile is
	// available immediately instead of 404ing until the merchant's first PUT.
	if h.deps.Profile != nil {
		if err := h.deps.Profile.SaveBusinessProfile(c.Request().Context(), &domain.StoreBusinessProfile{
			StoreID:    string(s.StoreID),
			MerchantID: string(merchantID),
		}); err != nil {
			h.log.Error("failed to seed store profile", "error", err, "merchant_id", merchantID, "store_id", s.StoreID)
			// Don't fail store creation if profile seeding fails; store is created successfully
		}
	}

	// Freshly created; the profile just seeded above has no domain set yet,
	// and there's no task on a brand-new store either. Both lifecycle states
	// are exactly what Initialize just set above — journey "connected",
	// activation "draft" — so there's no need to re-read either back.
	return c.JSON(http.StatusCreated, storeToResponse(&s, "", 0, false, domain.JourneyConnected, domain.UCPStateDraft))
}

func (h *handler) timelineMetrics(c echo.Context) error {
	// Parse optional query parameters.
	merchantIDStr := c.QueryParam("merchant_id")
	var merchantID *domain.MerchantID
	if merchantIDStr != "" {
		m := domain.MerchantID(merchantIDStr)
		merchantID = &m
	}

	startDate := c.QueryParam("start_date")
	endDate := c.QueryParam("end_date")

	// If no date range specified, use reasonable defaults (last 90 days).
	if startDate == "" || endDate == "" {
		// For now, empty strings will be handled by service (queries all available data).
		// In production, would compute 90-day window here.
	}

	result, err := h.deps.Operator.TimelineMetrics(c.Request().Context(), merchantID, startDate, endDate)
	if err != nil {
		h.log.Error("timelineMetrics failed", "error", err)
		return errInternal(c)
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"datapoints": result,
		"count":      len(result),
	})
}
