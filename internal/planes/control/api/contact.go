// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlab/sarvex/internal/pkg/email"
)

var contactEmailRe = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// contactRequest is the body POST /contact accepts, matching the website's
// server/api/contact.post.ts forwarding payload exactly (name/email/message/source).
type contactRequest struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	Message string `json:"message"`
	Source  string `json:"source"`
}

// postContact receives a contact-form submission from the sarvex.example.com marketing
// website (gated by requireContactToken, not requireAuth — the website has no
// Sarvex account) and emails it to the designated recipient as hello@sarvex.example.com.
func (h *handler) postContact(c echo.Context) error {
	if h.deps.ContactSender == nil {
		return c.JSON(http.StatusNotImplemented, map[string]string{"status": "not_implemented"})
	}

	var req contactRequest
	if err := c.Bind(&req); err != nil {
		return errValidation(c, map[string]string{"body": "malformed request body"})
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.TrimSpace(req.Email)
	req.Message = strings.TrimSpace(req.Message)

	fields := map[string]string{}
	if req.Name == "" || len(req.Name) > 200 {
		fields["name"] = "must be 1-200 characters"
	}
	if req.Email == "" || len(req.Email) > 320 || !contactEmailRe.MatchString(req.Email) {
		fields["email"] = "must be a valid email address"
	}
	if len(req.Message) > 4000 {
		fields["message"] = "must be 4000 characters or fewer"
	}
	if len(fields) > 0 {
		return errValidation(c, map[string]any{"fields": fields})
	}

	body := "From: " + req.Name + " <" + req.Email + ">\n\n" + req.Message
	if req.Source != "" {
		body += "\n\nSource: " + req.Source
	}

	err := h.deps.ContactSender.SendNotification(c.Request().Context(), h.deps.ContactNotifyEmail, email.NotificationData{
		Title: "New contact form submission from " + req.Name,
		Body:  body,
	})
	if err != nil {
		h.log.Error("failed to send contact-form email", "error", err)
		return errInternal(c)
	}

	return c.NoContent(http.StatusAccepted)
}
