// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package ap2 implements the UCP AP2 Mandate extension
// (dev.ucp.common.payment.ap2_mandate): cryptographic mandate support for
// non-repudiable checkout authorization per the AP2 protocol, using an
// embedded-signature model under the "ap2" namespace on Checkout.
//
// Breaking change from v20260408: the capability moved from the shopping
// namespace to the common namespace and was renamed from
// "dev.ucp.shopping.ap2_mandate" to "dev.ucp.common.payment.ap2_mandate"
// (source/schemas/common/payment_ap2_mandate.json's "name" field), per
// this release's reorganization of payment-related extensions under
// common/. The extension's own content (merchant_authorization,
// checkout_mandate, and error codes) is otherwise unchanged.
package ap2

import (
	"fmt"
	"regexp"

	v20260825 "github.com/sarvexlab/sarvex/internal/spec/v20260825"
)

// CapabilityName is the reverse-domain name this extension is registered
// under in UCP metadata: "dev.ucp.common.payment.ap2_mandate".
const CapabilityName v20260825.ReverseDomainName = "dev.ucp.common.payment.ap2_mandate"

// MerchantAuthorization is a JWS Detached Content signature (RFC 7515
// Appendix F) over the checkout response body (excluding the "ap2"
// field): "<base64url-header>..<base64url-signature>". The header MUST
// carry "alg" (ES256/ES384/ES512) and "kid" claims; the signature covers
// both the header and the JCS-canonicalized checkout payload.
type MerchantAuthorization string

var merchantAuthorizationPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+\.\.[A-Za-z0-9_-]+$`)

// Validate reports whether m matches the JWS Detached Content shape.
func (m MerchantAuthorization) Validate() error {
	if !merchantAuthorizationPattern.MatchString(string(m)) {
		return fmt.Errorf("ucp/ap2: invalid merchant_authorization format")
	}
	return nil
}

// CheckoutMandate is an SD-JWT+kb credential proving user authorization
// for the checkout. Contains the full checkout including
// ap2.merchant_authorization.
type CheckoutMandate string

var checkoutMandatePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]*\.[A-Za-z0-9_-]+(~[A-Za-z0-9_-]+)*$`)

// Validate reports whether m matches the SD-JWT+kb shape.
func (m CheckoutMandate) Validate() error {
	if !checkoutMandatePattern.MatchString(string(m)) {
		return fmt.Errorf("ucp/ap2: invalid checkout_mandate format")
	}
	return nil
}

// Ap2WithMerchantAuthorization is AP2 extension data including the
// merchant's signature proving checkout terms are authentic.
type Ap2WithMerchantAuthorization struct {
	MerchantAuthorization MerchantAuthorization `json:"merchant_authorization,omitempty"`
}

// Ap2WithCheckoutMandate is AP2 extension data including the SD-JWT+kb
// proving the user authorized this checkout. Required at checkout
// completion.
type Ap2WithCheckoutMandate struct {
	CheckoutMandate CheckoutMandate `json:"checkout_mandate,omitempty"`
}

// AP2 is the full "ap2" namespace object embedded on
// CheckoutWithAP2Mandate: both the merchant authorization and the buyer's
// checkout mandate.
type AP2 struct {
	Ap2WithMerchantAuthorization
	Ap2WithCheckoutMandate
}

// CheckoutWithAP2Mandate is Checkout extended with AP2 mandate support.
// Composes the base Checkout type rather than duplicating its fields.
type CheckoutWithAP2Mandate struct {
	v20260825.Checkout
	AP2 *AP2 `json:"ap2,omitempty"`
}

// ErrorCode enumerates the error codes specific to AP2 mandate
// verification. Unlike the base spec's freeform Error/Warning/Info codes,
// this set is closed in the spec; Known still governs interpretation
// rather than unmarshal rejection, per the module-wide convention.
type ErrorCode string

const (
	ErrorCodeMandateRequired              ErrorCode = "mandate_required"
	ErrorCodeAgentMissingKey              ErrorCode = "agent_missing_key"
	ErrorCodeMandateInvalidSignature      ErrorCode = "mandate_invalid_signature"
	ErrorCodeMandateExpired               ErrorCode = "mandate_expired"
	ErrorCodeMandateScopeMismatch         ErrorCode = "mandate_scope_mismatch"
	ErrorCodeMerchantAuthorizationInvalid ErrorCode = "merchant_authorization_invalid"
	ErrorCodeMerchantAuthorizationMissing ErrorCode = "merchant_authorization_missing"
)

var knownErrorCodes = map[ErrorCode]struct{}{
	ErrorCodeMandateRequired:              {},
	ErrorCodeAgentMissingKey:              {},
	ErrorCodeMandateInvalidSignature:      {},
	ErrorCodeMandateExpired:               {},
	ErrorCodeMandateScopeMismatch:         {},
	ErrorCodeMerchantAuthorizationInvalid: {},
	ErrorCodeMerchantAuthorizationMissing: {},
}

// Known reports whether c is one of the standardized AP2 error codes.
func (c ErrorCode) Known() bool { _, ok := knownErrorCodes[c]; return ok }
