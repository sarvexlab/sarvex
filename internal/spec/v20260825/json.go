// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package v20260825

import (
	"encoding/json"
	"fmt"
)

// This file holds custom (Un)MarshalJSON implementations for types whose
// wire shape mixes fixed, named fields with an open "additionalProperties"
// tail (Buyer, Context, LocationSummary, SearchFilters, Signals,
// PaymentInstrument, Policy, MultiDestinationEntry,
// FulfillmentDestinationFilter, SigningKey). Discriminated unions
// (Message, PaymentCredential, FulfillmentDestination) and the
// Capability "extends" oneOf implement their own (Un)MarshalJSON
// alongside their type definitions, since they are single-purpose and
// easier to follow in context.

// marshalWithExtra marshals named (typically a private alias struct
// mirroring a type's fixed fields) and overlays extra on top, so that
// unknown/additional properties round-trip losslessly. Named fields take
// precedence on key collision.
func marshalWithExtra(named any, extra map[string]any) ([]byte, error) {
	body, err := json.Marshal(named)
	if err != nil {
		return nil, err
	}
	if len(extra) == 0 {
		return body, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	for k, v := range extra {
		if _, exists := m[k]; exists {
			continue
		}
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		m[k] = b
	}
	return json.Marshal(m)
}

// unmarshalWithExtra unmarshals data into named (a pointer to an alias
// struct mirroring a type's fixed fields), then returns every top-level
// key from data that is not in knownKeys as an "extra" map, preserving
// unknown fields for lossless round-tripping.
func unmarshalWithExtra(data []byte, named any, knownKeys []string) (map[string]any, error) {
	if err := json.Unmarshal(data, named); err != nil {
		return nil, err
	}
	var all map[string]any
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, err
	}
	for _, k := range knownKeys {
		delete(all, k)
	}
	if len(all) == 0 {
		return nil, nil
	}
	return all, nil
}

type buyerAlias struct {
	FirstName   string `json:"first_name,omitempty"`
	LastName    string `json:"last_name,omitempty"`
	Email       string `json:"email,omitempty"`
	PhoneNumber string `json:"phone_number,omitempty"`
}

var buyerKnownKeys = []string{"first_name", "last_name", "email", "phone_number"}

// UnmarshalJSON preserves any additional buyer properties in Extra.
func (b *Buyer) UnmarshalJSON(data []byte) error {
	var alias buyerAlias
	extra, err := unmarshalWithExtra(data, &alias, buyerKnownKeys)
	if err != nil {
		return fmt.Errorf("ucp: buyer: %w", err)
	}
	*b = Buyer{
		FirstName:   alias.FirstName,
		LastName:    alias.LastName,
		Email:       alias.Email,
		PhoneNumber: alias.PhoneNumber,
		Extra:       extra,
	}
	return nil
}

// MarshalJSON re-adds any additional buyer properties from Extra.
func (b Buyer) MarshalJSON() ([]byte, error) {
	m, err := b.FieldMap()
	if err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

// FieldMap renders Buyer's fields (including Extra) as a mutable map of
// raw JSON. Exported so that types outside this package which compose
// Buyer (e.g. the buyerconsent extension's BuyerWithConsent) can layer
// additional fields on top without falling into the Go embedding trap
// where Buyer's own MarshalJSON would otherwise be promoted verbatim,
// silently dropping the outer type's additional fields.
func (b Buyer) FieldMap() (map[string]json.RawMessage, error) {
	alias := buyerAlias{FirstName: b.FirstName, LastName: b.LastName, Email: b.Email, PhoneNumber: b.PhoneNumber}
	body, err := marshalWithExtra(alias, b.Extra)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	return m, nil
}

type contextAlias struct {
	AddressCountry string                     `json:"address_country,omitempty"`
	AddressRegion  string                     `json:"address_region,omitempty"`
	PostalCode     string                     `json:"postal_code,omitempty"`
	Location       string                     `json:"location,omitempty"`
	Intent         string                     `json:"intent,omitempty"`
	Language       string                     `json:"language,omitempty"`
	Currency       string                     `json:"currency,omitempty"`
	Eligibility    []ReverseDomainName        `json:"eligibility,omitempty"`
	Payment        []ContextPaymentPreference `json:"payment,omitempty"`
}

var contextKnownKeys = []string{
	"address_country", "address_region", "postal_code", "location",
	"intent", "language", "currency", "eligibility", "payment",
}

// UnmarshalJSON preserves any additional context properties in Extra.
func (c *Context) UnmarshalJSON(data []byte) error {
	var alias contextAlias
	extra, err := unmarshalWithExtra(data, &alias, contextKnownKeys)
	if err != nil {
		return fmt.Errorf("ucp: context: %w", err)
	}
	*c = Context{
		AddressCountry: alias.AddressCountry,
		AddressRegion:  alias.AddressRegion,
		PostalCode:     alias.PostalCode,
		Location:       alias.Location,
		Intent:         alias.Intent,
		Language:       alias.Language,
		Currency:       alias.Currency,
		Eligibility:    alias.Eligibility,
		Payment:        alias.Payment,
		Extra:          extra,
	}
	return nil
}

// MarshalJSON re-adds any additional context properties from Extra.
func (c Context) MarshalJSON() ([]byte, error) {
	alias := contextAlias{
		AddressCountry: c.AddressCountry,
		AddressRegion:  c.AddressRegion,
		PostalCode:     c.PostalCode,
		Location:       c.Location,
		Intent:         c.Intent,
		Language:       c.Language,
		Currency:       c.Currency,
		Eligibility:    c.Eligibility,
		Payment:        c.Payment,
	}
	return marshalWithExtra(alias, c.Extra)
}

type locationSummaryAlias struct {
	ID      string         `json:"id"`
	Name    string         `json:"name,omitempty"`
	Address *PostalAddress `json:"address,omitempty"`
}

var locationSummaryKnownKeys = []string{"id", "name", "address"}

// UnmarshalJSON preserves any additional location properties in Extra.
func (l *LocationSummary) UnmarshalJSON(data []byte) error {
	var alias locationSummaryAlias
	extra, err := unmarshalWithExtra(data, &alias, locationSummaryKnownKeys)
	if err != nil {
		return fmt.Errorf("ucp: location summary: %w", err)
	}
	*l = LocationSummary{ID: alias.ID, Name: alias.Name, Address: alias.Address, Extra: extra}
	return nil
}

// MarshalJSON re-adds any additional location properties from Extra.
func (l LocationSummary) MarshalJSON() ([]byte, error) {
	m, err := l.FieldMap()
	if err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

// FieldMap renders LocationSummary's fields (including Extra) as a
// mutable map of raw JSON. Exported so that FulfillmentDestination can
// inject the "business_location" type discriminator on top without
// falling into the Go embedding trap of a promoted MarshalJSON silently
// dropping it.
func (l LocationSummary) FieldMap() (map[string]json.RawMessage, error) {
	alias := locationSummaryAlias{ID: l.ID, Name: l.Name, Address: l.Address}
	body, err := marshalWithExtra(alias, l.Extra)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	return m, nil
}

type searchFiltersAlias struct {
	Categories []string     `json:"categories,omitempty"`
	Price      *PriceFilter `json:"price,omitempty"`
}

var searchFiltersKnownKeys = []string{"categories", "price"}

// UnmarshalJSON preserves any additional filter properties in Extra.
func (f *SearchFilters) UnmarshalJSON(data []byte) error {
	var alias searchFiltersAlias
	extra, err := unmarshalWithExtra(data, &alias, searchFiltersKnownKeys)
	if err != nil {
		return fmt.Errorf("ucp: search filters: %w", err)
	}
	*f = SearchFilters{Categories: alias.Categories, Price: alias.Price, Extra: extra}
	return nil
}

// MarshalJSON re-adds any additional filter properties from Extra.
func (f SearchFilters) MarshalJSON() ([]byte, error) {
	alias := searchFiltersAlias{Categories: f.Categories, Price: f.Price}
	return marshalWithExtra(alias, f.Extra)
}

type signalsAlias struct {
	BuyerIP   string `json:"dev.ucp.buyer_ip,omitempty"`
	UserAgent string `json:"dev.ucp.user_agent,omitempty"`
}

var signalsKnownKeys = []string{"dev.ucp.buyer_ip", "dev.ucp.user_agent"}

// UnmarshalJSON preserves any additional reverse-domain-namespaced signal
// keys in Extra.
func (s *Signals) UnmarshalJSON(data []byte) error {
	var alias signalsAlias
	extra, err := unmarshalWithExtra(data, &alias, signalsKnownKeys)
	if err != nil {
		return fmt.Errorf("ucp: signals: %w", err)
	}
	*s = Signals{BuyerIP: alias.BuyerIP, UserAgent: alias.UserAgent, Extra: extra}
	return nil
}

// MarshalJSON re-adds any additional signal keys from Extra.
func (s Signals) MarshalJSON() ([]byte, error) {
	alias := signalsAlias{BuyerIP: s.BuyerIP, UserAgent: s.UserAgent}
	return marshalWithExtra(alias, s.Extra)
}

type paymentInstrumentAlias struct {
	ID             string             `json:"id"`
	HandlerID      string             `json:"handler_id"`
	Type           string             `json:"type"`
	BillingAddress *PostalAddress     `json:"billing_address,omitempty"`
	Credential     *PaymentCredential `json:"credential,omitempty"`
	Display        map[string]any     `json:"display,omitempty"`
}

var paymentInstrumentKnownKeys = []string{"id", "handler_id", "type", "billing_address", "credential", "display"}

// UnmarshalJSON preserves any additional payment instrument properties in Extra.
func (p *PaymentInstrument) UnmarshalJSON(data []byte) error {
	var alias paymentInstrumentAlias
	extra, err := unmarshalWithExtra(data, &alias, paymentInstrumentKnownKeys)
	if err != nil {
		return fmt.Errorf("ucp: payment instrument: %w", err)
	}
	*p = PaymentInstrument{
		ID:             alias.ID,
		HandlerID:      alias.HandlerID,
		Type:           alias.Type,
		BillingAddress: alias.BillingAddress,
		Credential:     alias.Credential,
		Display:        alias.Display,
		Extra:          extra,
	}
	return nil
}

// MarshalJSON re-adds any additional payment instrument properties from Extra.
func (p PaymentInstrument) MarshalJSON() ([]byte, error) {
	m, err := p.fieldMap()
	if err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

// fieldMap renders PaymentInstrument's fields (including Extra) as a
// mutable map of raw JSON, so that types embedding PaymentInstrument
// (SelectedPaymentInstrument, CardPaymentInstrument) can layer their own
// fields on top without falling into the Go embedding trap where the
// embedded type's MarshalJSON would otherwise be promoted verbatim,
// silently dropping the outer type's additional fields.
func (p PaymentInstrument) fieldMap() (map[string]json.RawMessage, error) {
	alias := paymentInstrumentAlias{
		ID:             p.ID,
		HandlerID:      p.HandlerID,
		Type:           p.Type,
		BillingAddress: p.BillingAddress,
		Credential:     p.Credential,
		Display:        p.Display,
	}
	body, err := marshalWithExtra(alias, p.Extra)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	return m, nil
}

type policyAlias struct {
	Type        ReverseDomainName `json:"type"`
	Description Description       `json:"description"`
	AppliesTo   []string          `json:"applies_to,omitempty"`
	URL         string            `json:"url,omitempty"`
}

var policyKnownKeys = []string{"type", "description", "applies_to", "url"}

// UnmarshalJSON preserves any additional, type-specific policy properties
// in Extra.
func (p *Policy) UnmarshalJSON(data []byte) error {
	var alias policyAlias
	extra, err := unmarshalWithExtra(data, &alias, policyKnownKeys)
	if err != nil {
		return fmt.Errorf("ucp: policy: %w", err)
	}
	*p = Policy{Type: alias.Type, Description: alias.Description, AppliesTo: alias.AppliesTo, URL: alias.URL, Extra: extra}
	return nil
}

// MarshalJSON re-adds any additional policy properties from Extra.
func (p Policy) MarshalJSON() ([]byte, error) {
	alias := policyAlias{Type: p.Type, Description: p.Description, AppliesTo: p.AppliesTo, URL: p.URL}
	return marshalWithExtra(alias, p.Extra)
}

type actionInstanceAlias struct {
	ID     string         `json:"id"`
	Config map[string]any `json:"config,omitempty"`
}

var actionInstanceKnownKeys = []string{"id", "config"}

// UnmarshalJSON preserves any additional Action instance properties in Extra.
func (a *ActionInstance) UnmarshalJSON(data []byte) error {
	var alias actionInstanceAlias
	extra, err := unmarshalWithExtra(data, &alias, actionInstanceKnownKeys)
	if err != nil {
		return fmt.Errorf("ucp: action instance: %w", err)
	}
	*a = ActionInstance{ID: alias.ID, Config: alias.Config, Extra: extra}
	return nil
}

// MarshalJSON re-adds any additional Action instance properties from Extra.
func (a ActionInstance) MarshalJSON() ([]byte, error) {
	alias := actionInstanceAlias{ID: a.ID, Config: a.Config}
	return marshalWithExtra(alias, a.Extra)
}

type multiDestinationEntryAlias struct {
	Method FulfillmentMethodType `json:"method"`
}

var multiDestinationEntryKnownKeys = []string{"method"}

// UnmarshalJSON preserves any additional per-method constraint properties
// in Extra.
func (e *MultiDestinationEntry) UnmarshalJSON(data []byte) error {
	var alias multiDestinationEntryAlias
	extra, err := unmarshalWithExtra(data, &alias, multiDestinationEntryKnownKeys)
	if err != nil {
		return fmt.Errorf("ucp: multi destination entry: %w", err)
	}
	*e = MultiDestinationEntry{Method: alias.Method, Extra: extra}
	return nil
}

// MarshalJSON re-adds any additional per-method constraint properties
// from Extra.
func (e MultiDestinationEntry) MarshalJSON() ([]byte, error) {
	alias := multiDestinationEntryAlias{Method: e.Method}
	return marshalWithExtra(alias, e.Extra)
}

type fulfillmentDestinationFilterAlias struct {
	AddressCountry string `json:"address_country,omitempty"`
	AddressRegion  string `json:"address_region,omitempty"`
	PostalCode     string `json:"postal_code,omitempty"`
	Location       string `json:"location,omitempty"`
}

var fulfillmentDestinationFilterKnownKeys = []string{"address_country", "address_region", "postal_code", "location"}

// UnmarshalJSON preserves any additional filter properties in Extra.
func (f *FulfillmentDestinationFilter) UnmarshalJSON(data []byte) error {
	var alias fulfillmentDestinationFilterAlias
	extra, err := unmarshalWithExtra(data, &alias, fulfillmentDestinationFilterKnownKeys)
	if err != nil {
		return fmt.Errorf("ucp/fulfillment: destination filter: %w", err)
	}
	*f = FulfillmentDestinationFilter{
		AddressCountry: alias.AddressCountry,
		AddressRegion:  alias.AddressRegion,
		PostalCode:     alias.PostalCode,
		Location:       alias.Location,
		Extra:          extra,
	}
	return nil
}

// MarshalJSON re-adds any additional filter properties from Extra.
func (f FulfillmentDestinationFilter) MarshalJSON() ([]byte, error) {
	alias := fulfillmentDestinationFilterAlias{
		AddressCountry: f.AddressCountry,
		AddressRegion:  f.AddressRegion,
		PostalCode:     f.PostalCode,
		Location:       f.Location,
	}
	return marshalWithExtra(alias, f.Extra)
}

type signingKeyAlias struct {
	Kid string        `json:"kid"`
	Kty string        `json:"kty"`
	Crv string        `json:"crv,omitempty"`
	X   string        `json:"x,omitempty"`
	Y   string        `json:"y,omitempty"`
	Alg string        `json:"alg,omitempty"`
	Use SigningKeyUse `json:"use,omitempty"`
}

var signingKeyKnownKeys = []string{"kid", "kty", "crv", "x", "y", "alg", "use"}

// UnmarshalJSON preserves any additional public JWK members in Extra.
func (k *SigningKey) UnmarshalJSON(data []byte) error {
	var alias signingKeyAlias
	extra, err := unmarshalWithExtra(data, &alias, signingKeyKnownKeys)
	if err != nil {
		return fmt.Errorf("ucp: signing key: %w", err)
	}
	*k = SigningKey{
		Kid: alias.Kid, Kty: alias.Kty, Crv: alias.Crv, X: alias.X, Y: alias.Y,
		Alg: alias.Alg, Use: alias.Use, Extra: extra,
	}
	return nil
}

// MarshalJSON re-adds any additional public JWK members from Extra.
func (k SigningKey) MarshalJSON() ([]byte, error) {
	alias := signingKeyAlias{Kid: k.Kid, Kty: k.Kty, Crv: k.Crv, X: k.X, Y: k.Y, Alg: k.Alg, Use: k.Use}
	return marshalWithExtra(alias, k.Extra)
}
