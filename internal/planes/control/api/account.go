// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/samber/lo"

	"github.com/sarvexlab/sarvex/internal/planes/control/account"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

type accountResponse struct {
	MerchantID    domain.MerchantID     `json:"merchant_id"`
	Name          string                `json:"name"`
	Timezone      string                `json:"timezone"`
	CountryCode   string                `json:"country_code"`
	BusinessFacts domain.BusinessFacts  `json:"business_facts"`
	CreatedAt     time.Time             `json:"created_at"`
	LocaleOptions localeOptionsResponse `json:"locale_options"`
}

type localeOptionsResponse struct {
	Countries []countryLocaleResponse `json:"countries"`
}

type countryLocaleResponse struct {
	Code      string   `json:"code"`
	Name      string   `json:"name"`
	TimeZones []string `json:"time_zones"`
}

func (h *handler) localeOptions(ctx context.Context) localeOptionsResponse {
	if h.deps.Locales == nil {
		return localeOptionsResponse{Countries: []countryLocaleResponse{}}
	}
	countries, err := h.deps.Locales.List(ctx)
	if err != nil {
		h.log.Error("failed to list locale options", "error", err)
		return localeOptionsResponse{Countries: []countryLocaleResponse{}}
	}
	out := lo.Map(countries, func(c account.CountryLocale, _ int) countryLocaleResponse {
		return countryLocaleResponse{Code: c.Code, Name: c.Name, TimeZones: c.TimeZones}
	})
	return localeOptionsResponse{Countries: out}
}

func (h *handler) accountToResponse(ctx context.Context, a domain.Account) accountResponse {
	return accountResponse{
		MerchantID:    a.MerchantID,
		Name:          a.Name,
		Timezone:      a.Timezone,
		CountryCode:   a.CountryCode,
		BusinessFacts: a.BusinessFacts,
		CreatedAt:     a.CreatedAt,
		LocaleOptions: h.localeOptions(ctx),
	}
}

func (h *handler) getAccount(c echo.Context) error {
	p := principalFrom(c)
	ctx := c.Request().Context()
	acc, err := h.deps.Account.Get(ctx, p.MerchantID)
	if err != nil {
		if errors.Is(err, account.ErrNotFound) {
			return errNotFound(c)
		}
		h.log.Error("getAccount failed", "error", err, "merchant_id", p.MerchantID)
		return errInternal(c)
	}
	return c.JSON(http.StatusOK, h.accountToResponse(ctx, acc))
}

func (h *handler) updateAccount(c echo.Context) error {
	p := principalFrom(c)
	var body domain.AccountUpdate
	if err := c.Bind(&body); err != nil {
		return errValidation(c, err.Error())
	}
	ctx := c.Request().Context()
	acc, err := h.deps.Account.Update(ctx, p.MerchantID, body)
	if err != nil {
		h.log.Error("updateAccount failed", "error", err, "merchant_id", p.MerchantID)
		return errInternal(c)
	}
	return c.JSON(http.StatusOK, h.accountToResponse(ctx, acc))
}

func (h *handler) listMembers(c echo.Context) error {
	p := principalFrom(c)
	cursor := c.QueryParam("cursor")
	limit := 20
	if l := c.QueryParam("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	result, err := h.deps.Account.ListMembers(c.Request().Context(), p.MerchantID, cursor, limit)
	if err != nil {
		h.log.Error("listMembers failed", "error", err, "merchant_id", p.MerchantID)
		return errInternal(c)
	}
	return c.JSON(http.StatusOK, result)
}

// inviteTeamMember is the account owner's self-serve invite (Settings → Team).
// Restricted to the owner role by the requireOwner middleware; always invites as
// staff and is capped at the merchant's billing-plan seat limit.
func (h *handler) inviteTeamMember(c echo.Context) error {
	p := principalFrom(c)
	var body struct {
		Email string `json:"email"`
	}
	if err := c.Bind(&body); err != nil {
		return errValidation(c, "invalid request body")
	}
	if body.Email == "" {
		return errValidation(c, "email is required")
	}
	member, err := h.deps.Account.InviteTeamMember(c.Request().Context(), p.MerchantID, body.Email)
	if err != nil {
		if errors.Is(err, account.ErrMemberAlreadyActive) {
			return c.JSON(http.StatusConflict, domain.ErrorEnvelope{
				Error: domain.ErrorDetail{
					Code:    "member_already_active",
					Message: "member is already active for this merchant",
				},
			})
		}
		if errors.Is(err, account.ErrMemberLimitReached) {
			return c.JSON(http.StatusConflict, domain.ErrorEnvelope{
				Error: domain.ErrorDetail{
					Code:    "member_limit_reached",
					Message: "this account has reached its team member limit",
				},
			})
		}
		h.log.Error("inviteTeamMember failed", "error", err, "merchant_id", p.MerchantID)
		return errInternal(c)
	}
	return c.JSON(http.StatusCreated, member)
}
