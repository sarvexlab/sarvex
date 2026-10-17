// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"errors"
	"io"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlab/sarvex/internal/planes/control/billing"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

func (h *handler) getBilling(c echo.Context) error {
	p := principalFrom(c)
	b, err := h.deps.Billing.Get(c.Request().Context(), p.MerchantID)
	if err != nil {
		h.log.Error("getBilling failed", "error", err, "merchant_id", p.MerchantID)
		return errInternal(c)
	}
	return c.JSON(http.StatusOK, b)
}

func (h *handler) billingPortalLink(c echo.Context) error {
	p := principalFrom(c)
	link, err := h.deps.Billing.CreatePortalLink(c.Request().Context(), p.MerchantID)
	if err != nil {
		if errors.Is(err, billing.ErrNoStripeCustomer) {
			return errNotFound(c)
		}
		h.log.Error("billingPortalLink failed", "error", err, "merchant_id", p.MerchantID)
		return errInternal(c)
	}
	return c.JSON(http.StatusOK, link)
}

// stripeWebhook receives Stripe webhook events. Unlike every other route, it
// carries no auth/hmac middleware: verification happens inside
// Billing.HandleStripeWebhook via the Stripe SDK against a static
// per-process secret, since there's no per-caller credential to resolve
// up front (unlike connector HMAC, which is per-connector).
func (h *handler) stripeWebhook(c echo.Context) error {
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return errValidation(c, "failed to read request body")
	}
	sig := c.Request().Header.Get("Stripe-Signature")

	err = h.deps.Billing.HandleStripeWebhook(c.Request().Context(), body, sig)
	if err != nil {
		if errors.Is(err, billing.ErrInvalidWebhookSignature) {
			return errUnauthorized(c)
		}
		h.log.Error("stripeWebhook failed", "error", err)
		return errInternal(c)
	}
	return c.NoContent(http.StatusOK)
}

func (h *handler) getNotificationPreferences(c echo.Context) error {
	p := principalFrom(c)
	prefs, err := h.deps.Billing.GetNotificationPreferences(c.Request().Context(), p.MerchantID)
	if err != nil {
		h.log.Error("getNotificationPreferences failed", "error", err, "merchant_id", p.MerchantID)
		return errInternal(c)
	}
	return c.JSON(http.StatusOK, prefs)
}

func (h *handler) setNotificationPreferences(c echo.Context) error {
	p := principalFrom(c)
	var prefs domain.NotificationPreferences
	if err := c.Bind(&prefs); err != nil {
		return errValidation(c, err.Error())
	}
	saved, err := h.deps.Billing.SetNotificationPreferences(c.Request().Context(), p.MerchantID, prefs)
	if err != nil {
		h.log.Error("setNotificationPreferences failed", "error", err, "merchant_id", p.MerchantID)
		return errInternal(c)
	}
	return c.JSON(http.StatusOK, saved)
}
