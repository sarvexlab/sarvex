// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package wire

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Transport identifies how a service binding is reached.
type Transport string

const (
	TransportREST     Transport = "rest"
	TransportMCP      Transport = "mcp"
	TransportA2A      Transport = "a2a"
	TransportEmbedded Transport = "embedded"
)

// Service is one transport binding for a service vertical. A vertical may
// have multiple service entries (one per transport).
//
// See https://ucp.dev/latest/specification/overview/#service-definition
type Service struct {
	Version   Version         `json:"version"`
	Spec      string          `json:"spec"`
	Schema    string          `json:"schema,omitempty"`
	ID        string          `json:"id,omitempty"`
	Config    json.RawMessage `json:"config,omitempty"`
	Transport Transport       `json:"transport"`
	Endpoint  string          `json:"endpoint,omitempty"`
}

// Capability declares a feature within a service. Extensions populate
// Extends; root capabilities leave it empty.
//
// See https://ucp.dev/latest/specification/overview/#capability-definition
type Capability struct {
	Version Version         `json:"version"`
	Spec    string          `json:"spec"`
	Schema  string          `json:"schema"`
	ID      string          `json:"id,omitempty"`
	Config  json.RawMessage `json:"config,omitempty"`

	// Extends is either a single capability name (string) or an array of
	// names. Use ExtendsList to read it uniformly.
	Extends ExtendsField `json:"extends,omitzero"`
}

// ExtendsList returns the parent capability names. Returns nil for root
// capabilities.
func (c Capability) ExtendsList() []string { return c.Extends.List() }

// CapVersion returns this capability entry's own version string, for the
// generation-agnostic intersection algorithm (wireintersect.Capability).
func (c Capability) CapVersion() string { return string(c.Version) }

// IsExtension reports whether the capability extends one or more parents.
func (c Capability) IsExtension() bool { return len(c.Extends.List()) > 0 }

// ExtendsField is the polymorphic JSON shape of the `extends` field — it
// can be a string or an array of strings. Marshaling normalizes to a
// single string when there's exactly one parent, matching the spec
// examples.
type ExtendsField struct {
	values []string
}

// IsZero reports a root capability's empty extends, so it is omitted from the
// wire rather than written as null (extends is a string or an array when
// present).
func (e ExtendsField) IsZero() bool { return len(e.values) == 0 }

// List returns the parent names. Always safe to call on a zero value.
func (e ExtendsField) List() []string { return e.values }

// NewExtends builds an ExtendsField from parent names.
func NewExtends(parents ...string) ExtendsField { return ExtendsField{values: parents} }

// UnmarshalJSON accepts either a string or []string.
func (e *ExtendsField) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return fmt.Errorf("wire: extends string: %w", err)
		}
		e.values = []string{s}
		return nil
	}
	var arr []string
	if err := json.Unmarshal(data, &arr); err != nil {
		return fmt.Errorf("wire: extends array: %w", err)
	}
	e.values = arr
	return nil
}

// MarshalJSON normalizes single-parent to string, multi-parent to array.
func (e ExtendsField) MarshalJSON() ([]byte, error) {
	switch len(e.values) {
	case 0:
		return []byte("null"), nil
	case 1:
		return json.Marshal(e.values[0])
	default:
		return json.Marshal(e.values)
	}
}

// PaymentHandler is a payment handler declaration. It appears both in a
// profile (advertising support) and in the response envelope (Meta).
//
// See https://ucp.dev/latest/specification/overview/#payment-handlers
type PaymentHandler struct {
	ID                   string              `json:"id"`
	Version              Version             `json:"version"`
	Spec                 string              `json:"spec,omitempty"`
	Schema               string              `json:"schema,omitempty"`
	AvailableInstruments []PaymentInstrument `json:"available_instruments,omitempty"`
	Config               json.RawMessage     `json:"config,omitempty"`
}

// PaymentInstrument describes an instrument type a handler can accept.
type PaymentInstrument struct {
	Type        string                 `json:"type"`
	Constraints *InstrumentConstraints `json:"constraints,omitempty"`
}

// InstrumentConstraints narrows the instruments a handler accepts.
type InstrumentConstraints struct {
	Brands []string `json:"brands,omitempty"`
}

// JWK is the public JSON Web Key a profile publishes for UCP signatures (EC,
// and from 2026-08-25 OKP/Ed25519). The crypto handling and the private scalar
// live in the ucpsig package; here we carry only the public wire shape.
type JWK struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Crv string `json:"crv,omitempty"`
	X   string `json:"x,omitempty"`
	Y   string `json:"y,omitempty"`
	Use string `json:"use,omitempty"`
	Alg string `json:"alg,omitempty"`
}

// UCP is the inner protocol object inside a profile.
type UCP struct {
	Version           Version                     `json:"version"`
	Services          map[string][]Service        `json:"services,omitempty"`
	Capabilities      map[string][]Capability     `json:"capabilities,omitempty"`
	PaymentHandlers   map[string][]PaymentHandler `json:"payment_handlers,omitempty"`
	SupportedVersions map[string]string           `json:"supported_versions,omitempty"`
	Status            string                      `json:"status,omitempty"`
	WebhookURL        string                      `json:"webhook_url,omitempty"`
}

// Profile is the top-level document published at /.well-known/ucp (for
// businesses) or at an arbitrary HTTPS URL (for platforms), in Sarvex's own
// generation-neutral form.
//
// Keys are the profile's public signing keys. Generations name that member
// differently ("signing_keys", then "keys" — an RFC 7517 JWK Set), so a
// document is written in one generation's form by that generation's own
// internal/spec/vYYYYMMDD/wire package. Reading is lenient: a profile is
// decoded from either name, because a counterparty's profile may be in any
// generation and its keys must be found whichever it is.
type Profile struct {
	UCP  UCP   `json:"ucp"`
	Keys []JWK `json:"keys,omitempty"`
}

// UnmarshalJSON decodes a profile in any generation's form, taking its keys
// from "keys" or, failing that, from "signing_keys".
func (p *Profile) UnmarshalJSON(data []byte) error {
	var doc struct {
		UCP         UCP   `json:"ucp"`
		Keys        []JWK `json:"keys"`
		SigningKeys []JWK `json:"signing_keys"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return err
	}
	p.UCP, p.Keys = doc.UCP, doc.Keys
	if len(p.Keys) == 0 {
		p.Keys = doc.SigningKeys
	}
	return nil
}

// FindKey returns the signing key matching kid, or nil if none.
func (p *Profile) FindKey(kid string) *JWK {
	if p == nil {
		return nil
	}
	for i := range p.Keys {
		if p.Keys[i].Kid == kid {
			return &p.Keys[i]
		}
	}
	return nil
}

// Validate enforces the structural rules in the spec. It does not perform
// I/O — namespace authority and schema-fetch checks happen elsewhere.
func (p *Profile) Validate() error {
	if p == nil {
		return errors.New("wire: nil profile")
	}
	if _, err := ParseVersion(string(p.UCP.Version)); err != nil {
		return fmt.Errorf("wire: ucp.version: %w", err)
	}
	for name, caps := range p.UCP.Capabilities {
		if name == "" {
			return errors.New("wire: empty capability name")
		}
		if !strings.Contains(name, ".") {
			return fmt.Errorf("wire: capability %q must use reverse-domain naming", name)
		}
		for i, c := range caps {
			if c.Spec == "" {
				return fmt.Errorf("wire: capability %q[%d]: spec is required", name, i)
			}
			if c.Schema == "" {
				return fmt.Errorf("wire: capability %q[%d]: schema is required", name, i)
			}
			if _, err := ParseVersion(string(c.Version)); err != nil {
				return fmt.Errorf("wire: capability %q[%d].version: %w", name, i, err)
			}
		}
	}
	return nil
}
