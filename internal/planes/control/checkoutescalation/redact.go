// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package checkoutescalation

import "strings"

// sensitiveFieldPatterns is the closed list of field-name substrings that
// must never reach a timeline document, checked case-insensitively against
// every key at every depth of a decoded event payload. This is deliberately
// pattern-based rather than an allowlist of known-safe fields: the checkout
// lifecycle events (internal/planes/serving/checkout_lifecycle_events.go)
// don't carry raw payment credentials or PANs today (the checkout schema
// has no field able to hold one — sarvex-v1-architecture.md A10 invariant 8), but this list
// exists so a future field added to any of those events without updating
// this list still can't leak by accident — it must not be relied on to
// convert an unsafe event contract into a safe one.
var sensitiveFieldPatterns = []string{
	// Payment credential material.
	"credential",
	"card_number",
	"cardnumber",
	"pan",
	"cvv",
	"cvc",
	"security_code",
	"token",
	"payment_method",
	"account_number",
	"routing_number",
	"iban",
	"swift",
	// Buyer PII beyond what the merchant already has via their own
	// commerce backend (R6) — phone/email/full postal address, and auth
	// material.
	"password",
	"secret",
	"ssn",
	"social_security",
	"email",
	"phone",
	"address",
	"date_of_birth",
	"dob",
}

// Redact strips any key matching sensitiveFieldPatterns from payload,
// recursively, at every depth and within arrays — this is the single
// function every timeline write passes through (R6: redaction is enforced
// at write time, not read time). Returns a new map; payload itself is not
// mutated.
func Redact(payload map[string]any) map[string]any {
	return redactMap(payload)
}

func redactMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if isSensitiveKey(k) {
			continue
		}
		out[k] = redactValue(v)
	}
	return out
}

func redactValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return redactMap(t)
	case []any:
		redacted := make([]any, len(t))
		for i, item := range t {
			redacted[i] = redactValue(item)
		}
		return redacted
	default:
		return v
	}
}

func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, pattern := range sensitiveFieldPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}
