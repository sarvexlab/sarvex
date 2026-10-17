// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/samber/lo"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/store"
)

// ── Wire DTOs ──────────────────────────────────────────────────────────────

type moneyDTO struct {
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
}

type shippingRateDTO struct {
	Method string   `json:"method"`
	Cost   moneyDTO `json:"cost"`
}

type shippingZoneDTO struct {
	ID    string            `json:"id"`
	Name  string            `json:"name"`
	Rates []shippingRateDTO `json:"rates"`
}

// storeProfileResponse is the read shape for GET /stores/:store_id/profile.
// domain carries the store's storefront URL (the brand domain serving /.well-known/ucp).
type storeProfileResponse struct {
	StoreID            string                     `json:"store_id"`
	LegalName          string                     `json:"legal_name"`
	SupportEmail       string                     `json:"support_email"`
	Domain             string                     `json:"domain"`
	ReturnPolicyURL    string                     `json:"return_policy_url"`
	FulfillmentRegions []string                   `json:"fulfillment_regions"`
	ShippingZones      map[string]shippingZoneDTO `json:"shipping_zones"`
}

// storeProfileUpdateRequest is the full-replacement write shape for PUT.
type storeProfileUpdateRequest struct {
	LegalName          string                     `json:"legal_name"`
	SupportEmail       string                     `json:"support_email"`
	Domain             string                     `json:"domain"`
	ReturnPolicyURL    string                     `json:"return_policy_url"`
	FulfillmentRegions []string                   `json:"fulfillment_regions"`
	ShippingZones      map[string]shippingZoneDTO `json:"shipping_zones"`
}

// GET /stores/:store_id/profile — read a store's business profile.
func (h *handler) getStoreProfile(c echo.Context) error {
	p := principalFrom(c)
	storeID := domain.StoreID(c.Param("store_id"))
	if storeID == "" {
		return errValidation(c, "store_id is required")
	}
	if h.deps.Profile == nil || h.deps.Store == nil {
		return errInternal(c)
	}

	ctx := c.Request().Context()
	bp, err := h.deps.Profile.GetBusinessProfile(ctx, storeID)
	if errors.Is(err, store.ErrProfileNotFound) {
		// Lazy-provision: stores created before profile-seeding-on-create
		// existed (or any other historical gap) have no profile row yet.
		// Verify the store actually belongs to this merchant first —
		// Store.Get enforces that scoping — before creating one, so a
		// guessed/foreign store_id can't be used to plant a profile
		// claiming the wrong ownership.
		if _, storeErr := h.deps.Store.Get(ctx, p.MerchantID, storeID); storeErr != nil {
			return errNotFound(c)
		}
		seed := &domain.StoreBusinessProfile{StoreID: string(storeID), MerchantID: string(p.MerchantID)}
		if saveErr := h.deps.Profile.SaveBusinessProfile(ctx, seed); saveErr != nil {
			h.log.Error("failed to lazily seed store profile", "error", saveErr, "merchant_id", p.MerchantID, "store_id", storeID)
			return errInternal(c)
		}
		bp = seed
	} else if err != nil {
		h.log.Error("failed to get store profile", "error", err, "merchant_id", p.MerchantID, "store_id", storeID)
		return errInternal(c)
	}

	// Tenant isolation: a store profile is only visible to its owning merchant.
	if bp.MerchantID != string(p.MerchantID) {
		return errNotFound(c)
	}

	cfg, err := h.deps.Profile.GetConfiguration(ctx, storeID)
	if err != nil && !errors.Is(err, store.ErrProfileNotFound) {
		h.log.Error("failed to get store configuration", "error", err, "merchant_id", p.MerchantID, "store_id", storeID)
		return errInternal(c)
	}

	return c.JSON(http.StatusOK, storeProfileToResponse(string(storeID), bp, cfg))
}

// PUT /stores/:store_id/profile — full-replacement update of a store's profile.
func (h *handler) updateStoreProfile(c echo.Context) error {
	p := principalFrom(c)
	storeID := domain.StoreID(c.Param("store_id"))
	if storeID == "" {
		return errValidation(c, "store_id is required")
	}
	if h.deps.Profile == nil {
		return errInternal(c)
	}

	var req storeProfileUpdateRequest
	if err := c.Bind(&req); err != nil {
		return errValidation(c, "invalid request body")
	}
	if msg := validateStoreProfile(&req); msg != "" {
		return errValidation(c, msg)
	}

	ctx := c.Request().Context()

	// Save business identity first: this is the ownership gate. A store profile
	// owned by another merchant collides on _id and returns ErrProfileNotFound.
	bp := &domain.StoreBusinessProfile{
		StoreID:         string(storeID),
		MerchantID:      string(p.MerchantID),
		LegalName:       req.LegalName,
		SupportEmail:    req.SupportEmail,
		StorefrontURL:   req.Domain,
		ReturnPolicyURL: req.ReturnPolicyURL,
	}
	if err := h.deps.Profile.SaveBusinessProfile(ctx, bp); err != nil {
		if errors.Is(err, store.ErrProfileNotFound) {
			return errNotFound(c)
		}
		h.log.Error("failed to save store profile", "error", err, "merchant_id", p.MerchantID, "store_id", storeID)
		return errInternal(c)
	}

	cfg := &domain.StoreConfiguration{
		StoreID:            string(storeID),
		MerchantID:         string(p.MerchantID),
		FulfillmentRegions: req.FulfillmentRegions,
		ShippingZones:      shippingZonesFromDTO(req.ShippingZones),
	}
	if err := h.deps.Profile.SaveConfiguration(ctx, cfg); err != nil {
		if errors.Is(err, store.ErrProfileNotFound) {
			return errNotFound(c)
		}
		h.log.Error("failed to save store configuration", "error", err, "merchant_id", p.MerchantID, "store_id", storeID)
		return errInternal(c)
	}

	// Notify the serving plane so it can rebuild the store's cached profile.
	if h.deps.EventPublisher != nil {
		event := &storeProfileUpdatedEvent{
			storeID:    string(storeID),
			merchantID: string(p.MerchantID),
			updatedBy:  p.Email,
			occurredAt: time.Now().UTC(),
		}
		if err := h.deps.EventPublisher.Publish(ctx, event); err != nil {
			h.log.Error("failed to emit store profile updated event", "error", err, "store_id", storeID)
			// Non-fatal: the profile is persisted.
		}
	}

	h.log.Info("store profile updated", "merchant_id", p.MerchantID, "store_id", storeID)
	return c.JSON(http.StatusOK, storeProfileToResponse(string(storeID), bp, cfg))
}

// ── Helpers ────────────────────────────────────────────────────────────────

func validateStoreProfile(req *storeProfileUpdateRequest) string {
	if req.SupportEmail != "" {
		if _, err := mail.ParseAddress(req.SupportEmail); err != nil {
			return "support_email must be a valid email address"
		}
	}
	if req.Domain != "" {
		u, err := url.ParseRequestURI(req.Domain)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return "domain must be a valid http(s) URL"
		}
	}
	if req.ReturnPolicyURL != "" {
		u, err := url.ParseRequestURI(req.ReturnPolicyURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return "return_policy_url must be a valid http(s) URL"
		}
	}
	for id, zone := range req.ShippingZones {
		if id == "" {
			return "shipping zone id must not be empty"
		}
		for _, r := range zone.Rates {
			if r.Cost.Currency == "" {
				return "shipping rate cost currency is required"
			}
			if r.Cost.AmountMinor < 0 {
				return "shipping rate cost must not be negative"
			}
		}
	}
	return ""
}

func shippingZonesFromDTO(in map[string]shippingZoneDTO) map[string]domain.ShippingZone {
	if in == nil {
		return nil
	}
	return lo.MapValues(in, func(z shippingZoneDTO, id string) domain.ShippingZone {
		rates := lo.Map(z.Rates, func(r shippingRateDTO, _ int) domain.Rate {
			return domain.Rate{
				Method: r.Method,
				Cost:   domain.Money{AmountMinor: r.Cost.AmountMinor, Currency: r.Cost.Currency},
			}
		})
		zoneID := z.ID
		if zoneID == "" {
			zoneID = id
		}
		return domain.ShippingZone{ID: zoneID, Name: z.Name, Rates: rates}
	})
}

func storeProfileToResponse(storeID string, bp *domain.StoreBusinessProfile, cfg *domain.StoreConfiguration) storeProfileResponse {
	resp := storeProfileResponse{
		StoreID:            storeID,
		FulfillmentRegions: []string{},
		ShippingZones:      map[string]shippingZoneDTO{},
	}
	if bp != nil {
		resp.LegalName = bp.LegalName
		resp.SupportEmail = bp.SupportEmail
		resp.Domain = bp.StorefrontURL
		resp.ReturnPolicyURL = bp.ReturnPolicyURL
	}
	if cfg != nil {
		if cfg.FulfillmentRegions != nil {
			resp.FulfillmentRegions = cfg.FulfillmentRegions
		}
		resp.ShippingZones = lo.MapValues(cfg.ShippingZones, func(z domain.ShippingZone, _ string) shippingZoneDTO {
			rates := lo.Map(z.Rates, func(r domain.Rate, _ int) shippingRateDTO {
				return shippingRateDTO{
					Method: r.Method,
					Cost:   moneyDTO{AmountMinor: r.Cost.AmountMinor, Currency: r.Cost.Currency},
				}
			})
			return shippingZoneDTO{ID: z.ID, Name: z.Name, Rates: rates}
		})
	}
	return resp
}

// storeProfileUpdatedEvent signals the serving plane to rebuild a store's cached profile.
type storeProfileUpdatedEvent struct {
	storeID    string
	merchantID string
	updatedBy  string
	occurredAt time.Time
	payload    []byte
}

func (e *storeProfileUpdatedEvent) EventType() string  { return ucp.EventStoreProfileUpdated }
func (e *storeProfileUpdatedEvent) StoreID() string    { return e.storeID }
func (e *storeProfileUpdatedEvent) MerchantID() string { return e.merchantID }
func (e *storeProfileUpdatedEvent) OccurredAt() time.Time {
	return e.occurredAt
}

func (e *storeProfileUpdatedEvent) Payload() []byte {
	if e.payload == nil {
		body, _ := json.Marshal(map[string]any{
			"store_id":    e.storeID,
			"merchant_id": e.merchantID,
			"updated_by":  e.updatedBy,
			"occurred_at": e.occurredAt,
		})
		e.payload = body
	}
	return e.payload
}
