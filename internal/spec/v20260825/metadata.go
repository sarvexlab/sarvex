// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package v20260825

import (
	"encoding/json"
	"fmt"
)

// Entity is the shared foundation for all UCP entities (Service,
// Capability, PaymentHandler). Version is the entity's own version in
// YYYY-MM-DD format — distinct from the protocol version this module
// tracks in the root types package, since a capability, service, or
// handler may version independently of the base UCP protocol.
type Entity struct {
	Version string `json:"version"`
	// Spec is a URL to the human-readable specification document.
	Spec string `json:"spec,omitempty"`
	// Schema is a URL to the JSON Schema defining this entity's structure and payloads.
	Schema string `json:"schema,omitempty"`
	// ID disambiguates when multiple instances of this entity exist.
	ID string `json:"id,omitempty"`
	// Config is entity-specific configuration; structure defined by each entity's schema.
	Config map[string]any `json:"config,omitempty"`
}

// VersionConstraint is a version range requirement with a minimum and
// optional maximum (inclusive on both ends).
type VersionConstraint struct {
	Min string `json:"min"`
	Max string `json:"max,omitempty"`
}

// Requires declares protocol and capability version requirements for an
// extension schema.
type Requires struct {
	Protocol *VersionConstraint `json:"protocol,omitempty"`
	// Capabilities is keyed by reverse-domain capability name.
	Capabilities map[ReverseDomainName]VersionConstraint `json:"capabilities,omitempty"`
}

// MapOrder is a preferred key-traversal order hint for map-valued fields
// in the scope annotated by the containing "ucp" member — new in this
// spec version. Each key names a target map, and its value lists target
// keys in preferred order; lists may be partial and are not allowlists.
type MapOrder map[string][]string

// Capability declares a UCP capability or extension. Extensions are
// capabilities with a non-empty Extends, naming the parent capability(s)
// they extend (a single name, or multiple for multi-parent extensions);
// root capabilities leave Extends empty.
//
// The same shape serves both "discovery" contexts (platform_schema,
// business_schema — where the entity is being declared/configured) and
// the "response" context (response_schema — where it merely confirms an
// active capability). ValidateDiscovery enforces the platform-only
// requirement that Spec and Schema be present; ValidateBusinessDiscovery
// enforces the (new in this spec version — see DISCREPANCIES.md)
// business-level requirement that Schema be present.
type Capability struct {
	Entity
	Extends []ReverseDomainName `json:"-"`
}

type capabilityAlias Entity

// UnmarshalJSON handles the oneOf(string, array-of-string) shape of the
// wire "extends" field.
func (c *Capability) UnmarshalJSON(data []byte) error {
	var alias capabilityAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return fmt.Errorf("ucp: capability: %w", err)
	}
	var extendsHolder struct {
		Extends json.RawMessage `json:"extends"`
	}
	if err := json.Unmarshal(data, &extendsHolder); err != nil {
		return fmt.Errorf("ucp: capability: %w", err)
	}
	var extends []ReverseDomainName
	if len(extendsHolder.Extends) > 0 {
		var single string
		if err := json.Unmarshal(extendsHolder.Extends, &single); err == nil {
			extends = []ReverseDomainName{ReverseDomainName(single)}
		} else {
			var arr []string
			if err := json.Unmarshal(extendsHolder.Extends, &arr); err != nil {
				return fmt.Errorf("ucp: capability: extends: %w", err)
			}
			for _, s := range arr {
				extends = append(extends, ReverseDomainName(s))
			}
		}
	}
	*c = Capability{Entity: Entity(alias), Extends: extends}
	return nil
}

// MarshalJSON emits "extends" as a bare string when there is exactly one
// parent, or as an array for zero or multiple parents.
func (c Capability) MarshalJSON() ([]byte, error) {
	m, err := c.FieldMap()
	if err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

// FieldMap renders Capability's fields (including the resolved "extends"
// value) as a mutable map of raw JSON. Exported so that types outside
// this package which compose Capability (e.g. the fulfillment
// extension's typed platform/business capability descriptors) can layer
// additional fields — such as a typed "config" — on top without falling
// into the Go embedding trap where Capability's own MarshalJSON would
// otherwise be promoted verbatim, silently dropping the outer type's
// additional fields.
func (c Capability) FieldMap() (map[string]json.RawMessage, error) {
	body, err := json.Marshal(capabilityAlias(c.Entity))
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	if len(c.Extends) == 0 {
		return m, nil
	}
	var extendsJSON []byte
	if len(c.Extends) == 1 {
		extendsJSON, err = json.Marshal(c.Extends[0])
	} else {
		extendsJSON, err = json.Marshal(c.Extends)
	}
	if err != nil {
		return nil, err
	}
	m["extends"] = extendsJSON
	return m, nil
}

// ValidateDiscovery enforces the platform_schema requirement that Spec
// and Schema be present. Not applicable to business_schema/response_schema.
func (c Capability) ValidateDiscovery() error {
	if c.Spec == "" || c.Schema == "" {
		return fmt.Errorf("ucp: capability: platform discovery requires spec and schema")
	}
	return nil
}

// ValidateBusinessDiscovery enforces the business_schema requirement that
// Schema be present. New in this spec version: v20260408's
// business_schema had no additional required fields beyond Entity's
// "version" (see DISCREPANCIES.md).
func (c Capability) ValidateBusinessDiscovery() error {
	if c.Schema == "" {
		return fmt.Errorf("ucp: capability: business discovery requires schema")
	}
	return nil
}

// Service is a service binding for a specific transport. Each transport
// binding is a separate entry in a UCP metadata services array.
type Service struct {
	Entity
	Transport ServiceTransport `json:"transport"`
	// Endpoint is the endpoint URL for this transport binding.
	Endpoint string `json:"endpoint,omitempty"`
}

// ValidateDiscovery enforces the platform_schema requirement that Spec be
// present, and that Schema be present for rest/mcp/embedded transports
// (a2a is exempt).
func (s Service) ValidateDiscovery() error {
	if s.Spec == "" {
		return fmt.Errorf("ucp: service: platform discovery requires spec")
	}
	switch s.Transport {
	case ServiceTransportREST, ServiceTransportMCP, ServiceTransportEmbedded:
		if s.Schema == "" {
			return fmt.Errorf("ucp: service: platform discovery requires schema for transport %q", s.Transport)
		}
	}
	return nil
}

// PaymentHandler defines how payment instruments are processed. See
// payment.go.

// UCPMetadataBase is the shared shape of UCP protocol metadata across
// discovery profiles and API responses ("ucp" field). Each response kind
// wraps this base in its own named type per the required-field
// differences documented in the spec (see CheckoutResponseMetadata,
// CartResponseMetadata, OrderResponseMetadata, CatalogResponseMetadata,
// PlatformMetadata, BusinessMetadata, ErrorResponseMetadata).
type UCPMetadataBase struct {
	Version string `json:"version"`
	// MapOrder is a preferred key-traversal order hint for the sibling
	// registry fields (Services, Capabilities, PaymentHandlers). New in
	// this spec version.
	MapOrder MapOrder `json:"map_order,omitempty"`
	// Status defaults to "success" when absent.
	Status UCPStatus `json:"status,omitempty"`
	// Services is a registry keyed by reverse-domain name.
	Services map[ReverseDomainName][]Service `json:"services,omitempty"`
	// Capabilities is a registry keyed by reverse-domain name.
	Capabilities map[ReverseDomainName][]Capability `json:"capabilities,omitempty"`
	// PaymentHandlers is a registry keyed by reverse-domain name.
	PaymentHandlers map[ReverseDomainName][]PaymentHandler `json:"payment_handlers,omitempty"`
}

// PlatformMetadata is the full UCP metadata for platform-level
// configuration, hosted at a URI advertised by the platform in request
// headers.
type PlatformMetadata struct {
	UCPMetadataBase
}

// Validate enforces the platform_schema requirement that Services and
// PaymentHandlers be present.
func (m PlatformMetadata) Validate() error {
	if m.Services == nil {
		return fmt.Errorf("ucp: platform metadata: services required")
	}
	if m.PaymentHandlers == nil {
		return fmt.Errorf("ucp: platform metadata: payment_handlers required")
	}
	return nil
}

// BusinessMetadata is UCP metadata for business/merchant-level
// configuration — a subset of PlatformMetadata with business-specific
// settings.
type BusinessMetadata struct {
	UCPMetadataBase
	// SupportedVersions maps previous protocol versions this business
	// supports to profile URIs. Businesses that support older protocol
	// versions SHOULD advertise each version and link to its profile.
	SupportedVersions map[string]string `json:"supported_versions,omitempty"`
}

// Validate enforces the business_schema requirement that Services and
// PaymentHandlers be present.
func (m BusinessMetadata) Validate() error {
	if m.Services == nil {
		return fmt.Errorf("ucp: business metadata: services required")
	}
	if m.PaymentHandlers == nil {
		return fmt.Errorf("ucp: business metadata: payment_handlers required")
	}
	return nil
}

// CheckoutResponseMetadata is UCP metadata for checkout responses.
type CheckoutResponseMetadata struct {
	UCPMetadataBase
}

// Validate enforces the response_checkout_schema requirement that
// PaymentHandlers be present.
func (m CheckoutResponseMetadata) Validate() error {
	if m.PaymentHandlers == nil {
		return fmt.Errorf("ucp: checkout response metadata: payment_handlers required")
	}
	return nil
}

// CartResponseMetadata is UCP metadata for cart responses. No payment
// handlers are needed pre-checkout.
type CartResponseMetadata struct {
	UCPMetadataBase
}

// OrderResponseMetadata is UCP metadata for order responses. No payment
// handlers are needed post-purchase.
type OrderResponseMetadata struct {
	UCPMetadataBase
}

// CatalogResponseMetadata is UCP metadata for catalog (search/lookup) responses.
type CatalogResponseMetadata struct {
	UCPMetadataBase
}

// ErrorResponseMetadata is UCP metadata with Status "error", used for
// ErrorResponse.
type ErrorResponseMetadata struct {
	UCPMetadataBase
}

// Validate enforces that Status is "error".
func (m ErrorResponseMetadata) Validate() error {
	if m.Status != UCPStatusError {
		return fmt.Errorf("ucp: error response metadata: status must be %q, got %q", UCPStatusError, m.Status)
	}
	return nil
}

// SigningKey is a public JSON Web Key published in a discovery profile's
// "keys" array, used to verify signed responses, webhooks, and other
// authenticated messages from the profile's owner. The profile's "keys"
// array is a valid RFC 7517 JWK Set: well-known key types are EC (P-256,
// P-384) and OKP (Ed25519); the kty/crv/alg vocabularies are open, and
// verifiers select keys by Kid. The schema explicitly forbids private-key
// members (d, p, q, dp, dq, qi, oth, k) — a profile publishes public keys
// only.
type SigningKey struct {
	Kid string        `json:"kid"`
	Kty string        `json:"kty"`
	Crv string        `json:"crv,omitempty"`
	X   string        `json:"x,omitempty"`
	Y   string        `json:"y,omitempty"`
	Alg string        `json:"alg,omitempty"`
	Use SigningKeyUse `json:"use,omitempty"`

	// Extra carries any additional public JWK members not modeled above
	// (the schema allows additional properties, e.g. for key types other
	// than EC/OKP); consumers of this package ignore unknown members.
	Extra map[string]any `json:"-"`
}

// PlatformDiscoveryProfile is the full discovery profile for platforms,
// exposing complete service, capability, and payment handler registries.
// Hosted at a URI advertised by the platform in request headers.
//
// Breaking change from v20260408: the wire field publishing public keys
// is "keys", not "signing_keys" (see DISCREPANCIES.md). The Go field is
// renamed to Keys to match.
type PlatformDiscoveryProfile struct {
	UCP  PlatformMetadata `json:"ucp"`
	Keys []SigningKey     `json:"keys,omitempty"`
}

// BusinessDiscoveryProfile is the discovery profile for
// businesses/merchants — a subset of PlatformDiscoveryProfile with
// business-specific configuration. Hosted at /.well-known/ucp.
//
// Breaking change from v20260408: the wire field publishing public keys
// is "keys", not "signing_keys" (see DISCREPANCIES.md). The Go field is
// renamed to Keys to match.
type BusinessDiscoveryProfile struct {
	UCP  BusinessMetadata `json:"ucp"`
	Keys []SigningKey     `json:"keys,omitempty"`
}
