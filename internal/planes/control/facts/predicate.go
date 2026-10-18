// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package facts implements the business-facts completeness predicate: it checks
// the persisted merchant- and store-level business facts against the
// catalogue-defined required set (architecture §3 Domain 1 / sarvex-v1-architecture.md A10.6:
// "spec version is data, not code"). Consumed internally by the
// checklist/journey service, which turns a missing fact into an open item —
// that wiring is out of scope here; this package only exposes the predicate.
package facts

import (
	"context"
	"errors"
	"fmt"

	v1cat "github.com/sarvexlab/sarvex/internal/pkg/ucp/catalogue/v1"
	"github.com/sarvexlab/sarvex/internal/planes/control/account"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/store"
)

// Result is the outcome of a completeness check.
type Result struct {
	Complete bool     `json:"complete"`
	Missing  []string `json:"missing"`
}

// AccountReader is the narrow slice of account.Service this package needs.
type AccountReader interface {
	Get(ctx context.Context, id domain.MerchantID) (domain.Account, error)
}

// StoreProfileReader is the narrow slice of store.ProfileService this package needs.
type StoreProfileReader interface {
	GetBusinessProfile(ctx context.Context, storeID domain.StoreID) (*domain.StoreBusinessProfile, error)
	GetConfiguration(ctx context.Context, storeID domain.StoreID) (*domain.StoreConfiguration, error)
}

// CatalogueReader is the narrow slice of catalogue.Catalogue this package needs.
type CatalogueReader interface {
	RequiredBusinessFacts() []v1cat.RequiredBusinessFact
}

// Predicate checks persisted business facts against the catalogue-defined
// required set.
type Predicate interface {
	// Complete reports whether every catalogue-required business fact is
	// present for the given merchant/store, and lists the fields (by their
	// catalogue field name) that are missing.
	Complete(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (Result, error)
}

type predicate struct {
	accounts AccountReader
	profiles StoreProfileReader
	cat      CatalogueReader
}

// New constructs a Predicate.
func New(accounts AccountReader, profiles StoreProfileReader, cat CatalogueReader) Predicate {
	return &predicate{accounts: accounts, profiles: profiles, cat: cat}
}

// Complete implements Predicate.
func (p *predicate) Complete(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (Result, error) {
	merchantFacts, err := p.merchantFacts(ctx, merchantID)
	if err != nil {
		return Result{}, err
	}
	bp, cfg, err := p.storeFacts(ctx, storeID)
	if err != nil {
		return Result{}, err
	}

	merchantValues := merchantFactPresence(merchantFacts)
	storeValues := storeFactPresence(bp, cfg)

	missing := []string{}
	for _, req := range p.cat.RequiredBusinessFacts() {
		var values map[string]bool
		switch req.Level {
		case v1cat.BusinessFactLevelMerchant:
			values = merchantValues
		case v1cat.BusinessFactLevelStore:
			values = storeValues
		default:
			continue
		}
		if !values[req.Field] {
			missing = append(missing, req.Field)
		}
	}

	return Result{Complete: len(missing) == 0, Missing: missing}, nil
}

func (p *predicate) merchantFacts(ctx context.Context, merchantID domain.MerchantID) (domain.BusinessFacts, error) {
	acc, err := p.accounts.Get(ctx, merchantID)
	if err != nil {
		if errors.Is(err, account.ErrNotFound) {
			// No account record yet — every merchant-level fact is missing.
			return domain.BusinessFacts{}, nil
		}
		return domain.BusinessFacts{}, fmt.Errorf("get account: %w", err)
	}
	return acc.BusinessFacts, nil
}

func (p *predicate) storeFacts(ctx context.Context, storeID domain.StoreID) (*domain.StoreBusinessProfile, *domain.StoreConfiguration, error) {
	bp, err := p.profiles.GetBusinessProfile(ctx, storeID)
	if err != nil {
		if !errors.Is(err, store.ErrProfileNotFound) {
			return nil, nil, fmt.Errorf("get store profile: %w", err)
		}
		bp = nil
	}

	cfg, err := p.profiles.GetConfiguration(ctx, storeID)
	if err != nil {
		if !errors.Is(err, store.ErrProfileNotFound) {
			return nil, nil, fmt.Errorf("get store configuration: %w", err)
		}
		cfg = nil
	}

	return bp, cfg, nil
}

// merchantFactPresence maps each persistable merchant business-fact field name
// to whether it currently holds a value.
func merchantFactPresence(f domain.BusinessFacts) map[string]bool {
	return map[string]bool{
		"legal_entity_name":   f.LegalEntityName != "",
		"support_email":       f.SupportEmail != "",
		"support_phone":       f.SupportPhone != "",
		"fulfillment_regions": len(f.FulfillmentRegions) > 0,
	}
}

// storeFactPresence maps each persistable store business-fact field name to
// whether it currently holds a value.
func storeFactPresence(bp *domain.StoreBusinessProfile, cfg *domain.StoreConfiguration) map[string]bool {
	values := map[string]bool{
		"legal_name":          false,
		"support_email":       false,
		"domain":              false,
		"return_policy_url":   false,
		"fulfillment_regions": false,
	}
	if bp != nil {
		values["legal_name"] = bp.LegalName != ""
		values["support_email"] = bp.SupportEmail != ""
		values["domain"] = bp.StorefrontURL != ""
		values["return_policy_url"] = bp.ReturnPolicyURL != ""
	}
	if cfg != nil {
		values["fulfillment_regions"] = len(cfg.FulfillmentRegions) > 0
	}
	return values
}
