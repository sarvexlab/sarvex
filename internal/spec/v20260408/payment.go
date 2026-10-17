// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package v20260408

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// AvailablePaymentInstrument declares an instrument type a PaymentHandler
// supports, with optional constraints. When a PaymentHandler's
// AvailableInstruments is absent, every instrument should be considered
// available.
type AvailablePaymentInstrument struct {
	// Type is the instrument type identifier (e.g. "card", "gift_card").
	Type string `json:"type"`
	// Constraints depend on instrument type and active capabilities.
	Constraints map[string]any `json:"constraints,omitempty"`
}

// PaymentIdentity identifies a participant for token binding. AccessToken
// uniquely identifies the participant tokens should be bound to.
type PaymentIdentity struct {
	AccessToken string `json:"access_token"`
}

// Binding binds a token to a specific checkout session and participant,
// preventing reuse across different checkouts or participants.
type Binding struct {
	CheckoutID string `json:"checkout_id"`
	// Identity is the participant this token is bound to. Required when
	// acting on behalf of another participant (e.g. agent tokenizing for
	// merchant). Omit when the authenticated caller is the binding target.
	Identity *PaymentIdentity `json:"identity,omitempty"`
}

// PaymentCredential is the base type for any payment credential. Handlers
// define specific credential types via the "type" discriminator; CardCredential
// and TokenCredential are the two credential shapes defined in the base
// spec. Unknown credential types are preserved as raw JSON so pass-through
// never loses data — Sarvex is a pass-through gateway and must not assume
// it knows every handler's credential shape.
type PaymentCredential struct {
	// Type is the credential type discriminator (e.g. "card", a
	// handler-specific token type).
	Type  string
	Card  *CardCredential
	Token *TokenCredential
	// Raw holds the original payload when Type does not resolve to a
	// known subtype (Card or Token), guaranteeing lossless round-trip.
	Raw json.RawMessage
}

type paymentCredentialDiscriminator struct {
	Type  string          `json:"type"`
	Token json.RawMessage `json:"token"`
}

// UnmarshalJSON dispatches on the "type" field: "card" resolves to
// CardCredential; presence of a "token" field (the structural signature
// of TokenCredential, since concrete token types are open strings such as
// "stripe_token") resolves to TokenCredential; anything else is preserved
// verbatim in Raw.
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
	case c.Token != nil:
		return json.Marshal(c.Token)
	case c.Raw != nil:
		return c.Raw, nil
	default:
		return json.Marshal(map[string]string{"type": c.Type})
	}
}

// CardCredential contains sensitive payment card details including raw
// Primary Account Numbers (PANs). MUST NOT be used for checkout — only
// with payment handlers that tokenize or encrypt credentials. Both
// sender and receiver MUST be PCI DSS compliant; transmission MUST use
// HTTPS/TLS with strong cipher suites.
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

// TokenCredential is the base token credential schema. Concrete payment
// handlers may extend it with additional fields.
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
	m, err := s.PaymentInstrument.fieldMap()
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
type CardPaymentInstrument struct {
	PaymentInstrument
	Display *CardPaymentInstrumentDisplay `json:"display,omitempty"`
}

// UnmarshalJSON populates the embedded PaymentInstrument (including its
// Extra passthrough) and the typed card Display, and fixes Type to "card".
func (c *CardPaymentInstrument) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &c.PaymentInstrument); err != nil {
		return err
	}
	var d struct {
		Display *CardPaymentInstrumentDisplay `json:"display"`
	}
	if err := json.Unmarshal(data, &d); err != nil {
		return fmt.Errorf("ucp: card payment instrument: %w", err)
	}
	c.Display = d.Display
	return nil
}

// MarshalJSON layers the typed card Display on top of the embedded
// PaymentInstrument's fields, fixing Type to "card". Defined explicitly
// so PaymentInstrument.MarshalJSON is not promoted verbatim, which would
// otherwise silently drop the typed Display.
func (c CardPaymentInstrument) MarshalJSON() ([]byte, error) {
	c.PaymentInstrument.Type = "card"
	m, err := c.PaymentInstrument.fieldMap()
	if err != nil {
		return nil, err
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
