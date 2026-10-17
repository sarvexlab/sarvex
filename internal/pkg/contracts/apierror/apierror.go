// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package apierror is the error envelope every Sarvex HTTP API outside the
// UCP surface answers with (sarvex-v1-architecture.md):
//
//	{ "error": { "code", "message", "details" } }
//
// It is shared so each plane can speak it without importing another plane.
package apierror

// Detail is the body of an error response.
type Detail struct {
	Code          string `json:"code"`
	Message       string `json:"message"`
	CorrelationID string `json:"correlation_id,omitempty"` // Request correlation ID for tracing
	Details       any    `json:"details,omitempty"`
}

// Envelope wraps a Detail as the top-level "error" object.
type Envelope struct {
	Error Detail `json:"error"`
}
