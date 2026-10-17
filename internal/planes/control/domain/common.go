// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/contracts/apierror"
)

// MerchantID is the opaque per-tenant identifier resolved from the auth principal.
// It is never sourced from a URL parameter. Partitions billing, identity, team, notification prefs.
type MerchantID string

// StoreID is the per-store identifier for a catalog/serving partition.
// Each store owns one connector. Partitions catalog, serving cache, sessions, profile, pairing, validation.
type StoreID string

// Money represents a monetary value as integer minor units (cents) plus an
// ISO-4217 currency code. Never use floats.
type Money struct {
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
}

// ShippingZone defines shipping rates for a geographic region.
type ShippingZone struct {
	ID    string // Zone identifier
	Name  string // Human-readable zone name
	Rates []Rate // Shipping rates for this zone
}

// Rate defines a single shipping rate method and cost.
type Rate struct {
	Method string // Shipping method (e.g., "ground", "express")
	Cost   Money  // Cost in minor units + currency
}

// Page is the cursor envelope returned on every list response.
type Page struct {
	Cursor  *string `json:"cursor"`
	HasMore bool    `json:"has_more"`
}

// ListOf wraps a typed slice in the standard list envelope.
type ListOf[T any] struct {
	Data []T  `json:"data"`
	Page Page `json:"page"`
}

// ErrorDetail is the body of every non-2xx response (shared with the other
// planes' APIs — see apierror).
type ErrorDetail = apierror.Detail

// ErrorEnvelope is the top-level wrapper for error responses.
type ErrorEnvelope = apierror.Envelope

// JobAccepted is returned when a long-running operation is queued.
type JobAccepted struct {
	JobID  string    `json:"job_id"`
	Status JobStatus `json:"status"`
}

type JobStatus string

const (
	JobQueued  JobStatus = "queued"
	JobRunning JobStatus = "running"
)

// Principal is the resolved identity attached to every authenticated request.
type Principal struct {
	UserID      string     `json:"user_id"`
	Email       string     `json:"email"`
	Role        Role       `json:"role"`
	MerchantID  MerchantID `json:"merchant_id"`
	StoreID     StoreID    `json:"store_id,omitempty"`     // Set by HMAC auth for connector requests
	ConnectorID string     `json:"connector_id,omitempty"` // Set by HMAC auth for connector requests
}

// Session is returned on successful authentication.
type Session struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	Principal Principal `json:"principal"`
}

type Role string

const (
	RoleOwner    Role = "owner"
	RoleStaff    Role = "staff"
	RoleOperator Role = "operator"
)
