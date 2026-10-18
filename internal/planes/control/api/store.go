// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"golang.org/x/sync/errgroup"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/platform"
)

type storeResponse struct {
	StoreID      domain.StoreID      `json:"store_id"`
	Name         string              `json:"name"`
	Domain       string              `json:"domain"`
	ConnectorID  string              `json:"connector_id"`
	Platform     domain.Platform     `json:"platform"`
	Status       domain.StoreStatus  `json:"status"`
	JourneyState domain.JourneyState `json:"journey_state,omitempty"`
	// UCPState is the store's own activation state, projected onto the store
	// record so the console's store switcher, health surface and presence
	// panel can read it without a second request. The full view is
	// GET /stores/{store_id}/activation.
	UCPState       domain.UCPState `json:"ucp_state,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	LastSyncAt     *time.Time      `json:"last_sync_at,omitempty"`
	ReadinessScore *float32        `json:"readiness_score,omitempty"`
	OpenTaskCount  int             `json:"open_task_count"`
	Blocked        bool            `json:"blocked"`
}

// storeToResponse builds the wire response for a store. domainURL is the
// store's profile-declared storefront URL (sarvex-v1-control-plane.yaml
// store.domain) — the store record itself carries no domain field; it's
// looked up from the profile via storefrontDomain, since that's the one
// place a merchant actually sets it (PUT /stores/:store_id/profile).
// openTaskCount/blocked come from storeTaskSummary — the header store
// switcher's per-store status badge. journeyState comes from
// storeJourneyState — the operator board still keys off it. ucpState comes
// from storeUCPState and is what the merchant-facing surfaces read: the
// Presence tab, the Health surface and the store switcher all ask "is this
// store transactable", which is an activation question, not a journey one.
func storeToResponse(s *domain.Store, domainURL string, openTaskCount int, blocked bool, journeyState domain.JourneyState, ucpState domain.UCPState) storeResponse {
	return storeResponse{
		StoreID:        s.StoreID,
		Name:           s.DisplayName,
		Domain:         domainURL,
		ConnectorID:    s.ConnectorID,
		Platform:       s.Platform,
		Status:         s.Status,
		JourneyState:   journeyState,
		UCPState:       ucpState,
		CreatedAt:      s.CreatedAt,
		UpdatedAt:      s.UpdatedAt,
		LastSyncAt:     s.LastSyncAt,
		ReadinessScore: s.ReadinessScore,
		OpenTaskCount:  openTaskCount,
		Blocked:        blocked,
	}
}

// storeTaskSummary computes the header store switcher's per-store badge:
// how many open tasks are on the merchant to act on, and whether any open
// task blocks checkout. Capped at 100 open tasks per store (the same
// pragmatic cap journey/operator summaries already use elsewhere) —
// best-effort, never fails the store list/get request over this field.
func (h *handler) storeTaskSummary(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (openTaskCount int, blocked bool) {
	if h.deps.Task == nil {
		return 0, false
	}
	openState := domain.TaskStateOpen
	result, err := h.deps.Task.List(ctx, merchantID, &storeID, "", 100, nil, &openState)
	if err != nil {
		h.log.Error("failed to summarize store tasks", "error", err, "merchant_id", merchantID, "store_id", storeID)
		return 0, false
	}
	for _, t := range result.Data {
		if t.Owner == domain.TaskOwnerMerchant {
			openTaskCount++
		}
		if t.BlocksCheckout {
			blocked = true
		}
	}
	return openTaskCount, blocked
}

// storefrontDomain looks up a store's profile-declared storefront URL for
// the domain field above. Best-effort: a store with no profile yet, or a
// Profile dependency that isn't wired, just gets "" — never fails the
// store list/get request over this one field.
func (h *handler) storefrontDomain(ctx context.Context, storeID domain.StoreID) string {
	if h.deps.Profile == nil {
		return ""
	}
	bp, err := h.deps.Profile.GetBusinessProfile(ctx, storeID)
	if err != nil {
		return ""
	}
	return bp.StorefrontURL
}

// storeJourneyState looks up a store's current journey state for the
// journey_state field above. Best-effort, same idiom as storeTaskSummary/
// storefrontDomain: a store with no journey record yet, or a Journey
// dependency that isn't wired, just gets "" rather than failing the whole
// store list/get request over this one field.
func (h *handler) storeJourneyState(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) domain.JourneyState {
	if h.deps.Journey == nil {
		return ""
	}
	j, err := h.deps.Journey.Get(ctx, merchantID, storeID)
	if err != nil {
		return ""
	}
	return j.State
}

// storeUCPState looks up a store's activation state for the ucp_state field
// above. Best-effort, same idiom as storeJourneyState: a store with no
// activation record yet, or an Activation dependency that isn't wired, gets
// "" rather than failing the whole store list/get request over one field.
func (h *handler) storeUCPState(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) domain.UCPState {
	if h.deps.Activation == nil {
		return ""
	}
	a, err := h.deps.Activation.Get(ctx, merchantID, storeID)
	if err != nil {
		return ""
	}
	return a.UCPState
}

// storeEnrichment runs the three best-effort per-store lookups above
// concurrently instead of sequentially: each writes only its own return
// values, so there's no shared state between the goroutines. A store list
// with N stores previously cost N * (list-item lookups), all serial — this
// is what pushed GET /stores' p95 over loadtest/control/console_read.js's
// http_req_duration{name:stores} threshold under its 100-VU stage. Every
// call site below (list, get, update) now pays max(3 lookups) instead of
// their sum per store.
func (h *handler) storeEnrichment(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domainURL string, openTaskCount int, blocked bool, journeyState domain.JourneyState, ucpState domain.UCPState) {
	var g errgroup.Group
	g.Go(func() error {
		openTaskCount, blocked = h.storeTaskSummary(ctx, merchantID, storeID)
		return nil
	})
	g.Go(func() error {
		domainURL = h.storefrontDomain(ctx, storeID)
		return nil
	})
	g.Go(func() error {
		journeyState = h.storeJourneyState(ctx, merchantID, storeID)
		return nil
	})
	g.Go(func() error {
		ucpState = h.storeUCPState(ctx, merchantID, storeID)
		return nil
	})
	_ = g.Wait()
	return
}

// GET /stores — list stores for the authenticated merchant.
func (h *handler) listStores(c echo.Context) error {
	p := principalFrom(c)

	cursor := c.QueryParam("cursor")
	limitStr := c.QueryParam("limit")
	limit := 20
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	result, err := h.deps.Store.List(c.Request().Context(), p.MerchantID, cursor, limit)
	if err != nil {
		h.log.Error("failed to list stores", "error", err, "merchant_id", p.MerchantID)
		return errInternal(c)
	}

	// Convert stores to response DTOs excluding merchant_id. Each store's
	// enrichment lookups run concurrently with every other store's (in
	// addition to storeEnrichment already parallelizing the three lookups
	// within a store) — V1 onboards one store per merchant, but the model
	// supports more, and this keeps a multi-store merchant's list latency
	// flat rather than linear in store count.
	ctx := c.Request().Context()
	storeResponses := make([]storeResponse, len(result.Data))
	var g errgroup.Group
	for i, s := range result.Data {
		g.Go(func() error {
			domainURL, openTaskCount, blocked, journeyState, ucpState := h.storeEnrichment(ctx, p.MerchantID, s.StoreID)
			storeResponses[i] = storeToResponse(&s, domainURL, openTaskCount, blocked, journeyState, ucpState)
			return nil
		})
	}
	_ = g.Wait()
	response := map[string]any{
		"data": storeResponses,
		"page": result.Page,
	}
	return c.JSON(http.StatusOK, response)
}

// POST /stores — create a new store.
type CreateStoreRequest struct {
	DisplayName string          `json:"display_name"`
	Platform    domain.Platform `json:"platform"`
}

func (h *handler) createStore(c echo.Context) error {
	p := principalFrom(c)

	var req CreateStoreRequest
	if err := c.Bind(&req); err != nil {
		return errValidation(c, "invalid request body")
	}

	if req.DisplayName == "" {
		return errValidation(c, "display_name is required")
	}

	if req.Platform == "" {
		return errValidation(c, "platform is required")
	}

	if !platform.Supported(req.Platform) {
		return errValidation(c, unsupportedPlatformMessage())
	}

	store, err := h.deps.Store.Create(c.Request().Context(), p.MerchantID, req.DisplayName, req.Platform)
	if err != nil {
		h.log.Error("failed to create store", "error", err, "merchant_id", p.MerchantID)
		return errInternal(c)
	}

	// Initialize journey for the new store (pre-pairing state).
	if err := h.deps.Journey.Initialize(c.Request().Context(), p.MerchantID, store.StoreID); err != nil {
		h.log.Error("failed to initialize journey", "error", err, "merchant_id", p.MerchantID, "store_id", store.StoreID)
		// Don't fail store creation if journey initialization fails; store is created successfully
	}

	// Activation starts at draft. It is a separate lifecycle from the
	// journey, so it is seeded here in its own right rather than derived
	// from the journey record — and like the journey, a failure to seed it
	// never fails store creation.
	if h.deps.Activation != nil {
		if err := h.deps.Activation.Initialize(c.Request().Context(), p.MerchantID, store.StoreID); err != nil {
			h.log.Error("failed to initialize activation", "error", err, "merchant_id", p.MerchantID, "store_id", store.StoreID)
		}
	}

	// Seed an empty business profile so GET /stores/:store_id/profile is
	// available immediately instead of 404ing until the merchant's first PUT.
	if h.deps.Profile != nil {
		if err := h.deps.Profile.SaveBusinessProfile(c.Request().Context(), &domain.StoreBusinessProfile{
			StoreID:    string(store.StoreID),
			MerchantID: string(p.MerchantID),
		}); err != nil {
			h.log.Error("failed to seed store profile", "error", err, "merchant_id", p.MerchantID, "store_id", store.StoreID)
			// Don't fail store creation if profile seeding fails; store is created successfully
		}
	}

	// Freshly created; the profile just seeded above has no domain set yet,
	// and there's no task on a brand-new store either. Both lifecycle states
	// are exactly what Initialize just set above — journey "connected",
	// activation "draft" — so there's no need to re-read either back.
	return c.JSON(http.StatusCreated, storeToResponse(&store, "", 0, false, domain.JourneyConnected, domain.UCPStateDraft))
}

// GET /stores/:store_id — retrieve a store.
func (h *handler) getStore(c echo.Context) error {
	p := principalFrom(c)

	storeID := domain.StoreID(c.Param("store_id"))
	if storeID == "" {
		return errValidation(c, "store_id is required")
	}

	ctx := c.Request().Context()
	store, err := h.deps.Store.Get(ctx, p.MerchantID, storeID)
	if err != nil {
		h.log.Error("failed to get store", "error", err, "merchant_id", p.MerchantID, "store_id", storeID)
		return errNotFound(c)
	}

	domainURL, openTaskCount, blocked, journeyState, ucpState := h.storeEnrichment(ctx, p.MerchantID, store.StoreID)
	return c.JSON(http.StatusOK, storeToResponse(&store, domainURL, openTaskCount, blocked, journeyState, ucpState))
}

// PATCH /stores/:store_id — update a store.
type UpdateStoreRequest struct {
	DisplayName string `json:"display_name"`
}

func (h *handler) updateStore(c echo.Context) error {
	p := principalFrom(c)

	storeID := domain.StoreID(c.Param("store_id"))
	if storeID == "" {
		return errValidation(c, "store_id is required")
	}

	var req UpdateStoreRequest
	if err := c.Bind(&req); err != nil {
		return errValidation(c, "invalid request body")
	}

	if req.DisplayName == "" {
		return errValidation(c, "display_name is required")
	}

	ctx := c.Request().Context()
	store, err := h.deps.Store.Update(ctx, p.MerchantID, storeID, req.DisplayName)
	if err != nil {
		h.log.Error("failed to update store", "error", err, "merchant_id", p.MerchantID, "store_id", storeID)
		return errNotFound(c)
	}

	domainURL, openTaskCount, blocked, journeyState, ucpState := h.storeEnrichment(ctx, p.MerchantID, store.StoreID)
	return c.JSON(http.StatusOK, storeToResponse(&store, domainURL, openTaskCount, blocked, journeyState, ucpState))
}

// DELETE /stores/:store_id — delete a store.
func (h *handler) deleteStore(c echo.Context) error {
	p := principalFrom(c)

	storeID := domain.StoreID(c.Param("store_id"))
	if storeID == "" {
		return errValidation(c, "store_id is required")
	}

	if err := h.deps.Store.Delete(c.Request().Context(), p.MerchantID, storeID); err != nil {
		h.log.Error("failed to delete store", "error", err, "merchant_id", p.MerchantID, "store_id", storeID)
		return errNotFound(c)
	}

	return c.NoContent(http.StatusNoContent)
}

// unsupportedPlatformMessage names the platforms a store can be created on or
// paired with — the platform registry's, never a list kept here — e.g.
// "platform must be woocommerce or magento".
func unsupportedPlatformMessage() string {
	names := platform.Names()
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = string(n)
	}
	if len(quoted) == 1 {
		return "platform must be " + quoted[0]
	}
	return "platform must be " + strings.Join(quoted[:len(quoted)-1], ", ") + " or " + quoted[len(quoted)-1]
}
