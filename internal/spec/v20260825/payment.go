// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package v20260825

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// AvailablePaymentInstrument declares an instrument type a PaymentHandler
// supports, with optional constraints. When a PaymentHandler's
// AvailableInstruments is absent, every instrument should be considered
// available.
//
// Constraints is kept as an opaque map[string]any, same as v20260408,
// rather than a typed recursive structure: the wire schema
// (common/types/constraint_expression.json) is a small closed JSON
// Schema Draft 2020-12 grammar (required/properties/anyOf, with property
// values recursively either another Constraint Expression or a
// {enum,const} Value Constraint) — see DISCREPANCIES.md for why this
// package mirrors it as an opaque pass-through, the same way it treats
// Entity.Config, PaymentInstrument.Display, and other
// "structure defined elsewhere" regions of the spec.
type AvailablePaymentInstrument struct {
	// Type is the instrument type identifier (e.g. "card", "gift_card").
	Type string `json:"type"`
	// Constraints depend on instrument type and active capabilities.
	Constraints map[string]any `json:"constraints,omitempty"`
}

// PaymentCredential is the base type for any payment credential. Handlers
// define specific credential types via the "type" discriminator.
//
// Breaking change from v20260408: that version's base spec defined only
// CardCredential (type "card") and the open-ended TokenCredential. This
// spec version deprecates CardCredential in favor of two narrower
// successors — PANCredential (type "pan", verified with a CVC) and
// NetworkTokenCredential (type "network_token", verified with a
// cryptogram) — while keeping CardCredential itself present (deprecated,
// not removed) for backward compatibility. TokenCredential is unchanged.
// Unknown credential types are preserved as raw JSON so pass-through
// never loses data — Sarvex is a pass-through gateway and must not
// assume it knows every handler's credential shape. See
// internal/planes/serving/api/checkout_contract.go's
// OpaquePaymentCredential for why faithfully modeling every raw field
// here (Number, CVC, Cryptogram, ...) is safe: Sarvex's checkout-complete
// HTTP DTO never decodes directly into this type.
type PaymentCredential struct {
	// Type is the credential type discriminator ("card" (deprecated),
	// "pan", "network_token", or a handler-specific token type).
	Type         string
	Card         *CardCredential
	PAN          *PANCredential
	NetworkToken *NetworkTokenCredential
	Token        *TokenCredential
	// Raw holds the original payload when Type does not resolve to a
	// known subtype, guaranteeing lossless round-trip.
	Raw json.RawMessage
}

type paymentCredentialDiscriminator struct {
	Type       string          `json:"type"`
	Number     json.RawMessage `json:"number"`
	Cryptogram json.RawMessage `json:"cryptogram"`
	Token      json.RawMessage `json:"token"`
}

// UnmarshalJSON dispatches on the "type" field: "card" resolves to the
// deprecated CardCredential; "pan" resolves to PANCredential;
// "network_token" resolves to NetworkTokenCredential; otherwise, presence
// of a "token" field (the structural signature of TokenCredential, since
// concrete token types are open strings such as "stripe_token") resolves
// to TokenCredential; anything else is preserved verbatim in Raw.
func (c *PaymentCredential) UnmarshalJSON(data []byte) error {
	var disc paymentCredentialDiscriminator
	if err := json.Unmarshal(data, &disc); err != nil {
		return fmt.Errorf("ucp: payment credential: %w", err)
	}
	switch {
	case disc.Type == "card":
		var card CardCredential
		if err := json.Unmarshal(data, &card); err != nil {
			return fmt.Errorf("ucp: payment credential: card: %w", err)
		}
		*c = PaymentCredential{Type: disc.Type, Card: &card}
	case disc.Type == "pan":
		var pan PANCredential
		if err := json.Unmarshal(data, &pan); err != nil {
			return fmt.Errorf("ucp: payment credential: pan: %w", err)
		}
		*c = PaymentCredential{Type: disc.Type, PAN: &pan}
	case disc.Type == "network_token":
		var nt NetworkTokenCredential
		if err := json.Unmarshal(data, &nt); err != nil {
			return fmt.Errorf("ucp: payment credential: network_token: %w", err)
		}
		*c = PaymentCredential{Type: disc.Type, NetworkToken: &nt}
	case disc.Token != nil:
		var token TokenCredential
		if err := json.Unmarshal(data, &token); err != nil {
			return fmt.Errorf("ucp: payment credential: token: %w", err)
		}
		*c = PaymentCredential{Type: disc.Type, Token: &token}
	default:
		// Compact so that Raw is byte-identical whether it arrived via
		// Unmarshal or was produced by our own Marshal, keeping
		// unmarshal->marshal->unmarshal round trips stable.
		var buf bytes.Buffer
		if err := json.Compact(&buf, data); err != nil {
			return fmt.Errorf("ucp: payment credential: %w", err)
		}
		*c = PaymentCredential{Type: disc.Type, Raw: buf.Bytes()}
	}
	return nil
}

// MarshalJSON round-trips the wrapped concrete credential, or the raw
// payload for unknown credential types.
func (c PaymentCredential) MarshalJSON() ([]byte, error) {
	switch {
	case c.Card != nil:
		return json.Marshal(c.Card)
	case c.PAN != nil:
		return json.Marshal(c.PAN)
	case c.NetworkToken != nil:
		return json.Marshal(c.NetworkToken)
	case c.Token != nil:
		return json.Marshal(c.Token)
	case c.Raw != nil:
		return c.Raw, nil
	default:
		return json.Marshal(map[string]string{"type": c.Type})
	}
}

// CardCredential contains sensitive payment card details including raw
// Primary Account Numbers (PANs).
//
// Deprecated: use PANCredential or NetworkTokenCredential instead — the
// credential type itself now carries the fpan/network_token distinction
// that CardCredential.CardNumberType used to encode. CardCredential
// remains present (not removed) in this spec version's schema for
// backward compatibility, so it is still modeled here.
//
// MUST NOT be used for checkout — only with payment handlers that
// tokenize or encrypt credentials. Both sender and receiver MUST be PCI
// DSS compliant; transmission MUST use HTTPS/TLS with strong cipher
// suites.
type CardCredential struct {
	Type           string         `json:"type"`
	CardNumberType CardNumberType `json:"card_number_type"`
	Number         string         `json:"number,omitempty"`
	ExpiryMonth    int            `json:"expiry_month,omitempty"`
	ExpiryYear     int            `json:"expiry_year,omitempty"`
	Name           string         `json:"name,omitempty"`
	CVC            string         `json:"cvc,omitempty"`
	Cryptogram     string         `json:"cryptogram,omitempty"`
	ECIValue       string         `json:"eci_value,omitempty"`
}

// PANCredential is a card credential carrying a funding primary account
// number (FPAN), verified with a CVC. New in this spec version — one of
// CardCredential's two successors. Credential selection follows the
// shape of the value on the wire rather than its provenance: a network
// token surfaced in PAN form and verified with a cvc is carried here,
// while a token verified with a discrete cryptogram uses
// NetworkTokenCredential. MUST NOT be used for checkout — only with
// payment handlers that tokenize or encrypt credentials. Both sender and
// receiver MUST be PCI DSS compliant; transmission MUST use HTTPS/TLS
// with strong cipher suites.
type PANCredential struct {
	Type        string `json:"type"`
	Number      string `json:"number"`
	ExpiryMonth int    `json:"expiry_month,omitempty"`
	ExpiryYear  int    `json:"expiry_year,omitempty"`
	Name        string `json:"name,omitempty"`
	CVC         string `json:"cvc,omitempty"`
}

// NetworkTokenCredential is a card-network token credential verified with
// a transaction cryptogram. New in this spec version — the other of
// CardCredential's two successors. Number carries the network token or
// wallet-provisioned token rather than the underlying FPAN.
type NetworkTokenCredential struct {
	Type        string `json:"type"`
	Number      string `json:"number"`
	ExpiryMonth int    `json:"expiry_month,omitempty"`
	ExpiryYear  int    `json:"expiry_year,omitempty"`
	Name        string `json:"name,omitempty"`
	Cryptogram  string `json:"cryptogram"`
	ECIValue    string `json:"eci_value,omitempty"`
	// TokenRequestorID is the payment network token requestor identifier,
	// when required by the processor or network-token program.
	TokenRequestorID string `json:"token_requestor_id,omitempty"`
}

// TokenCredential is the base token credential schema. Concrete payment
// handlers may extend it with additional fields. Unchanged from
// v20260408.
type TokenCredential struct {
	// Type is the specific type of token produced by the handler (e.g.
	// "stripe_token").
	Type string `json:"type"`
	// Token is omitted from responses (ucp_response: omit in the spec) —
	// it is request-only, sensitive material.
	Token string `json:"token,omitempty"`
}

// PaymentInstrument is the base type for any payment instrument. It links
// the instrument to a specific PaymentHandler.
type PaymentInstrument struct {
	// ID uniquely identifies this instrument instance, assigned by the platform.
	ID string `json:"id"`
	// HandlerID is the PaymentHandler.ID that produced this instrument.
	HandlerID string `json:"handler_id"`
	// Type is the broad instrument category (e.g. "card", "tokenized_card").
	Type           string             `json:"type"`
	BillingAddress *PostalAddress     `json:"billing_address,omitempty"`
	Credential     *PaymentCredential `json:"credential,omitempty"`
	// Display carries handler-defined display properties.
	Display map[string]any `json:"display,omitempty"`

	// Extra carries any additional fields not modeled above; the schema
	// allows additional properties.
	Extra map[string]any `json:"-"`
}

// SelectedPaymentInstrument is a PaymentInstrument with selection state,
// as returned in Payment.Instruments.
type SelectedPaymentInstrument struct {
	PaymentInstrument
	// Selected reports whether this instrument is selected by the user.
	Selected bool `json:"selected,omitempty"`
}

// UnmarshalJSON populates the embedded PaymentInstrument (including its
// Extra passthrough) and the Selected field.
func (s *SelectedPaymentInstrument) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &s.PaymentInstrument); err != nil {
		return err
	}
	var sel struct {
		Selected bool `json:"selected"`
	}
	if err := json.Unmarshal(data, &sel); err != nil {
		return fmt.Errorf("ucp: selected payment instrument: %w", err)
	}
	s.Selected = sel.Selected
	return nil
}

// MarshalJSON layers Selected on top of the embedded PaymentInstrument's
// fields. Defined explicitly so PaymentInstrument.MarshalJSON is not
// promoted verbatim, which would otherwise silently drop Selected.
func (s SelectedPaymentInstrument) MarshalJSON() ([]byte, error) {
	m, err := s.fieldMap()
	if err != nil {
		return nil, err
	}
	if s.Selected {
		m["selected"] = json.RawMessage("true")
	}
	return json.Marshal(m)
}

// CardPaymentInstrumentDisplay carries the display fields for a
// CardPaymentInstrument.
type CardPaymentInstrumentDisplay struct {
	Brand       string `json:"brand,omitempty"`
	LastDigits  string `json:"last_digits,omitempty"`
	ExpiryMonth int    `json:"expiry_month,omitempty"`
	ExpiryYear  int    `json:"expiry_year,omitempty"`
	Description string `json:"description,omitempty"`
	CardArt     string `json:"card_art,omitempty"`
}

// CardPaymentInstrument is a basic card payment instrument with visible
// card details. Handlers may inherit this shape to add handler-specific
// display details or more complex credential structures.
//
// New in this spec version: Network, for a co-badged card where the
// buyer elected a specific network for this transaction.
type CardPaymentInstrument struct {
	PaymentInstrument
	// Network is the card network elected for this transaction, typically
	// a co-badged selection. When present, the business MAY decline if
	// the card cannot route over it and MUST NOT substitute another.
	Network string                        `json:"network,omitempty"`
	Display *CardPaymentInstrumentDisplay `json:"display,omitempty"`
}

// UnmarshalJSON populates the embedded PaymentInstrument (including its
// Extra passthrough), Network, and the typed card Display, and fixes
// Type to "card".
func (c *CardPaymentInstrument) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &c.PaymentInstrument); err != nil {
		return err
	}
	var d struct {
		Network string                        `json:"network"`
		Display *CardPaymentInstrumentDisplay `json:"display"`
	}
	if err := json.Unmarshal(data, &d); err != nil {
		return fmt.Errorf("ucp: card payment instrument: %w", err)
	}
	c.Network = d.Network
	c.Display = d.Display
	return nil
}

// MarshalJSON layers Network and the typed card Display on top of the
// embedded PaymentInstrument's fields, fixing Type to "card". Defined
// explicitly so PaymentInstrument.MarshalJSON is not promoted verbatim,
// which would otherwise silently drop Network and the typed Display.
func (c CardPaymentInstrument) MarshalJSON() ([]byte, error) {
	c.Type = "card"
	m, err := c.fieldMap()
	if err != nil {
		return nil, err
	}
	if c.Network != "" {
		body, err := json.Marshal(c.Network)
		if err != nil {
			return nil, err
		}
		m["network"] = body
	}
	if c.Display != nil {
		body, err := json.Marshal(c.Display)
		if err != nil {
			return nil, err
		}
		m["display"] = body
	} else {
		delete(m, "display")
	}
	return json.Marshal(m)
}

// PaymentHandler defines how payment instruments are processed. It is
// keyed by reverse-domain name in UCP metadata's payment_handlers registry.
type PaymentHandler struct {
	Entity
	// ID uniquely identifies this handler instance.
	ID string `json:"id"`
	// AvailableInstruments lists instrument types this handler supports,
	// with optional constraints. When absent, every instrument should be
	// considered available.
	AvailableInstruments []AvailablePaymentInstrument `json:"available_instruments,omitempty"`
}

// Payment is the checkout capability's payment configuration, containing
// the payment instruments available for a checkout session. Each
// instrument is associated with a specific handler via HandlerID; handlers
// can extend the base instrument schema with handler-specific fields.
type Payment struct {
	Instruments []SelectedPaymentInstrument `json:"instruments,omitempty"`
}
