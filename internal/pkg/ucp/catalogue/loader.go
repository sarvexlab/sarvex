// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package catalogue

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/sarvexlab/sarvex/internal/pkg/domain"
	cataloguedata "github.com/sarvexlab/sarvex/internal/pkg/ucp/catalogue/data"
	v1cat "github.com/sarvexlab/sarvex/internal/pkg/ucp/catalogue/v1"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/paymenthandler"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

// Catalogue is the public interface for accessing UCP definitions.
type Catalogue interface {
	// Version returns the catalogue version string.
	Version() string

	// SpecVersion returns the UCP spec version this catalogue conforms to.
	SpecVersion() string

	// Services returns all services in the catalogue.
	Services() []wire.Service

	// AllCapabilities returns all capabilities (including out-of-scope ones).
	AllCapabilities() []wire.Capability

	// CapabilitiesForV1 returns only V1-implemented capabilities.
	CapabilitiesForV1() []wire.Capability

	// PaymentHandlers returns all payment handlers.
	PaymentHandlers() []wire.PaymentHandler

	// PaymentHandlersForPlatform returns the payment handlers that can process
	// payments on the given commerce platform (e.g. Shop Pay only on Shopify).
	PaymentHandlersForPlatform(platform string) []wire.PaymentHandler

	// ScoringRubric returns the scoring rubric.
	ScoringRubric() *v1cat.ScoringRubric

	// RequiredBusinessFacts returns the Google-required set of merchant/store
	// business facts. Loadable content (sarvex-v1-architecture.md A10.6): the business-facts
	// completeness predicate reads this rather than hardcoding required fields.
	RequiredBusinessFacts() []v1cat.RequiredBusinessFact

	// AttributeFields returns the map stage's target field model: the
	// UCP/Google product attributes a normalised product's raw attributes are
	// resolved onto (Domain 2a stage 1, "resolve"). Loadable, versioned
	// content — the mapping pipeline never hardcodes this set.
	AttributeFields() []v1cat.AttributeField
}

// loader implements the Catalogue interface.
type loader struct {
	cat            *v1cat.Catalogue
	rubricRepo     *cataloguedata.ScoringRubricRepo
	useMongoRubric bool
	log            *slog.Logger
	mu             sync.RWMutex
}

// Ensure loader implements Catalogue.
var _ Catalogue = (*loader)(nil)

// LoadCatalogue loads the UCP definitions catalogue from hardcoded code.
// No runtime file I/O: all data is defined as Go code.
// The catalogue is immutable once loaded.
func LoadCatalogue(ctx context.Context, log *slog.Logger) (Catalogue, error) {
	if log == nil {
		log = slog.Default()
	}

	// Get catalogue from code (no file I/O)
	cat := v1cat.DefaultCatalogue()

	// Validate the catalogue
	if err := validateCatalogue(cat); err != nil {
		return nil, fmt.Errorf("validate catalogue: %w", err)
	}

	log.Info("loaded UCP catalogue",
		"version", cat.Version,
		"spec_version", cat.SpecVersion,
		"capabilities", len(cat.Capabilities),
		"payment_handlers", len(cat.PaymentHandlers),
	)

	return &loader{
		cat:            cat,
		rubricRepo:     nil,
		useMongoRubric: false,
		log:            log,
	}, nil
}

// LoadCatalogueWithDB loads the catalogue from code and enables MongoDB-backed scoring rubric.
// If the MongoDB database is provided, the ScoringRubric() method will fetch from the database;
// otherwise it falls back to the code-based default.
func LoadCatalogueWithDB(ctx context.Context, db *mongo.Database, log *slog.Logger) (Catalogue, error) {
	if log == nil {
		log = slog.Default()
	}

	// Get catalogue from code (no file I/O)
	cat := v1cat.DefaultCatalogue()

	// Validate the catalogue
	if err := validateCatalogue(cat); err != nil {
		return nil, fmt.Errorf("validate catalogue: %w", err)
	}

	log.Info("loaded UCP catalogue",
		"version", cat.Version,
		"spec_version", cat.SpecVersion,
		"capabilities", len(cat.Capabilities),
		"payment_handlers", len(cat.PaymentHandlers),
	)

	// Create the MongoDB repo
	repo, err := cataloguedata.NewScoringRubricRepo(db)
	if err != nil {
		return nil, fmt.Errorf("create scoring rubric repo: %w", err)
	}

	log.Info("enabled MongoDB-backed scoring rubric loading")

	return &loader{
		cat:            cat,
		rubricRepo:     repo,
		useMongoRubric: true,
		log:            log,
	}, nil
}

// Version returns the catalogue version.
func (l *loader) Version() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.cat.Version
}

// SpecVersion returns the UCP spec version.
func (l *loader) SpecVersion() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.cat.SpecVersion
}

// Services returns all services.
func (l *loader) Services() []wire.Service {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.cat.Services
}

// AllCapabilities returns all capabilities.
func (l *loader) AllCapabilities() []wire.Capability {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.cat.Capabilities
}

// v1ImplementedCapabilities is the set of capabilities the serving plane
// actually has endpoints for, and therefore the only ones a store's profile
// may declare.
//
// This is deliberately narrower than the catalogue. Per sarvex-v1-architecture.md
// catalogue represents the *full* UCP capability surface — identity_linking
// is in it, correctly — while V1 implements the checkout-first subset. The
// profile is a promise to agents about what they can call, so it must reflect
// the subset, not the surface: declaring a capability with no route behind it
// would let negotiation agree on it (internal/planes/serving/api/handler.go
// registers /catalog/*, /carts/*, /checkout-sessions/*, and /orders/*, and
// nothing else), and the agent's first call would 404 after a successful
// handshake.
//
// cart joined once POST/GET/PUT /carts (internal/planes/serving/cart_service.go)
// shipped: it is the pre-purchase basket an agent builds before checkout.
//
// order joined catalog/checkout once GET/PUT /orders/{id}
// (internal/planes/serving/order_service.go, order_contract.go) shipped:
// checkout completion places an order, and an agent has no way to retrieve
// or update it (fulfillment events, adjustments) without this capability
// being negotiable — and the official UCP conformance suite's reference
// shopping agent declares dev.ucp.shopping.order unconditionally on every
// negotiated request, so its absence here failed capability negotiation
// for every checkout, not just order-specific calls.
var v1ImplementedCapabilities = map[string]bool{
	string(domain.CapabilityTypeCatalog):  true,
	string(domain.CapabilityTypeCheckout): true,
	string(domain.CapabilityTypeOrder):    true,
	string(domain.CapabilityTypeCart):     true,
}

// v1ImplementedExtensions is the set of extensions the serving plane and the
// WooCommerce connector actually honour end to end. An extension being
// attached to an implemented parent does NOT make it reachable — the profile
// promises agents behaviour, so each extension is listed only once it works:
//
//   - fulfillment: connector offers shipping options/destinations
//     (woocommerce ApplyFulfillmentExtension) and round-trips the selection.
//   - discount: connector applies coupon codes and returns applied discounts
//     (woocommerce ApplyDiscountExtension).
//   - buyer_consent: the connector persists the buyer's consent object on the
//     merchant order (meta_data) and returns it in checkout responses.
//
// Pickup needs no entry of its own: it is a fulfillment method type inside
// the fulfillment extension, offered (or not) in the checkout's fulfillment
// object according to what the merchant has enabled.
//
// Deliberately absent (declared in the catalogue, not advertised):
//   - ap2_mandate: no serving or connector code.
var v1ImplementedExtensions = map[string]bool{
	string(domain.ExtensionTypeFulfillment):  true,
	string(domain.ExtensionTypeDiscount):     true,
	string(domain.ExtensionTypeBuyerConsent): true,
}

// CapabilitiesForV1 returns only V1-implemented capabilities, plus the
// implemented extensions that extend one of them. Extensions are gated on
// v1ImplementedExtensions as well as on their parent being reachable.
func (l *loader) CapabilitiesForV1() []wire.Capability {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var result []wire.Capability
	for _, capability := range l.cat.Capabilities {
		if v1ImplementedCapabilities[capability.ID] {
			result = append(result, capability)
			continue
		}
		if !v1ImplementedExtensions[capability.ID] {
			continue
		}
		for _, parent := range capability.ExtendsList() {
			if v1ImplementedCapabilities[parent] {
				result = append(result, capability)
				break
			}
		}
	}
	return result
}

// PaymentHandlers returns all payment handlers.
func (l *loader) PaymentHandlers() []wire.PaymentHandler {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.cat.PaymentHandlers
}

// PaymentHandlersForPlatform returns the handlers applicable to platform. A
// handler with no spec is excluded: Sarvex cannot say what it needs or whether
// it works, so it is not offered.
func (l *loader) PaymentHandlersForPlatform(platform string) []wire.PaymentHandler {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var out []wire.PaymentHandler
	for _, h := range l.cat.PaymentHandlers {
		if spec, ok := paymenthandler.Lookup(h.ID); ok && spec.AppliesTo(platform) {
			out = append(out, h)
		}
	}
	return out
}

// ScoringRubric returns the scoring rubric.
// If MongoDB is configured, fetches from the database; otherwise returns the in-code default rubric.
// Falls back to the in-code default if the MongoDB fetch fails.
func (l *loader) ScoringRubric() *v1cat.ScoringRubric {
	l.mu.RLock()
	defer l.mu.RUnlock()

	// If MongoDB is not configured, return from embedded catalogue
	if !l.useMongoRubric || l.rubricRepo == nil {
		return l.cat.ScoringRubric
	}

	// Try to fetch from MongoDB
	rubric, err := l.rubricRepo.GetLatestScoringRubric(context.Background())
	if err != nil {
		l.log.Warn("failed to fetch scoring rubric from MongoDB, falling back to the in-code default",
			"error", err)
		return l.cat.ScoringRubric
	}

	// If not found in MongoDB, return embedded version
	if rubric == nil {
		l.log.Debug("scoring rubric not found in MongoDB, using the in-code default")
		return l.cat.ScoringRubric
	}

	return rubric
}

// RequiredBusinessFacts returns the Google-required business facts set.
func (l *loader) RequiredBusinessFacts() []v1cat.RequiredBusinessFact {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.cat.RequiredBusinessFacts
}

// AttributeFields returns the map stage's target field model.
func (l *loader) AttributeFields() []v1cat.AttributeField {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.cat.AttributeFields
}

// validateCatalogue checks the catalogue for schema and cross-reference consistency.
func validateCatalogue(cat *v1cat.Catalogue) error {
	// Check required fields
	if cat.Version == "" {
		return fmt.Errorf("catalogue version is required")
	}
	if cat.SpecVersion == "" {
		return fmt.Errorf("spec_version is required")
	}
	if len(cat.Services) == 0 {
		return fmt.Errorf("at least one service is required")
	}
	if cat.ScoringRubric == nil {
		return fmt.Errorf("scoring_rubric is required")
	}

	// Build ID maps for validation
	capabilityIDs := make(map[string]bool)
	for _, cap := range cat.Capabilities {
		if cap.ID == "" {
			return fmt.Errorf("capability missing ID")
		}
		if capabilityIDs[cap.ID] {
			return fmt.Errorf("duplicate capability ID: %s", cap.ID)
		}
		capabilityIDs[cap.ID] = true
	}

	ruleIDs := make(map[string]bool)
	for _, rule := range cat.ScoringRubric.Rules {
		if rule.ID == "" {
			return fmt.Errorf("scoring rule missing ID")
		}
		if ruleIDs[rule.ID] {
			return fmt.Errorf("duplicate rule ID: %s", rule.ID)
		}
		ruleIDs[rule.ID] = true
	}

	remediationIDs := make(map[string]bool)
	for _, rem := range cat.ScoringRubric.Remediations {
		if rem.ID == "" {
			return fmt.Errorf("remediation missing ID")
		}
		if remediationIDs[rem.ID] {
			return fmt.Errorf("duplicate remediation ID: %s", rem.ID)
		}
		remediationIDs[rem.ID] = true

		// Verify linked scoring rule exists
		if rem.ScoringRuleID != "" && !ruleIDs[rem.ScoringRuleID] {
			return fmt.Errorf("remediation %s references non-existent rule %s", rem.ID, rem.ScoringRuleID)
		}
	}

	// Verify payment handlers have valid IDs
	for _, handler := range cat.PaymentHandlers {
		if handler.ID == "" {
			return fmt.Errorf("payment handler missing ID")
		}
	}

	// Verify required business facts have valid IDs, levels, and field names.
	factIDs := make(map[string]bool)
	for _, fact := range cat.RequiredBusinessFacts {
		if fact.ID == "" {
			return fmt.Errorf("required business fact missing ID")
		}
		if factIDs[fact.ID] {
			return fmt.Errorf("duplicate required business fact ID: %s", fact.ID)
		}
		factIDs[fact.ID] = true
		if fact.Level != v1cat.BusinessFactLevelMerchant && fact.Level != v1cat.BusinessFactLevelStore {
			return fmt.Errorf("required business fact %s has invalid level: %s", fact.ID, fact.Level)
		}
		if fact.Field == "" {
			return fmt.Errorf("required business fact %s missing field", fact.ID)
		}
	}

	// Verify attribute fields (the map stage's target field model) have
	// unique, non-empty IDs.
	attributeFieldIDs := make(map[string]bool)
	for _, field := range cat.AttributeFields {
		if field.ID == "" {
			return fmt.Errorf("attribute field missing ID")
		}
		if attributeFieldIDs[field.ID] {
			return fmt.Errorf("duplicate attribute field ID: %s", field.ID)
		}
		attributeFieldIDs[field.ID] = true
	}

	return nil
}
