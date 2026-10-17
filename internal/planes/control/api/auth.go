// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/samber/lo"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

type principalResponse struct {
	UserID     string            `json:"user_id"`
	Email      string            `json:"email"`
	Role       domain.Role       `json:"role"`
	MerchantID domain.MerchantID `json:"merchant_id"`
	StoreIDs   []domain.StoreID  `json:"store_ids"`
}

// getMe resolves the current principal from the verified bearer token: the
// identity claims as-is, plus the merchant's stores (merchant → store, per
// the tenancy model — V1 onboards one store per merchant but the response
// shape doesn't assume that). Capped at 100 stores, same as every other
// merchant-scoped store listing (store.go's listStores).
func (h *handler) getMe(c echo.Context) error {
	p := principalFrom(c)
	result, err := h.deps.Store.List(c.Request().Context(), p.MerchantID, "", 100)
	if err != nil {
		h.log.Error("getMe: list stores failed", "error", err, "merchant_id", p.MerchantID)
		return errInternal(c)
	}
	storeIDs := lo.Map(result.Data, func(s domain.Store, _ int) domain.StoreID { return s.StoreID })
	return c.JSON(http.StatusOK, principalResponse{
		UserID:     p.UserID,
		Email:      p.Email,
		Role:       p.Role,
		MerchantID: p.MerchantID,
		StoreIDs:   storeIDs,
	})
}
