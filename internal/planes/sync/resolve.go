// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/samber/lo"

	v1cat "github.com/sarvexlab/sarvex/internal/pkg/ucp/catalogue/v1"
)

// Resolution tiers for AttributeMapping.Tier. Kept as plain strings (not
// control/domain.ResolutionTier) so the sync plane's write-side model stays
// decoupled from the control plane's read model, per the field-for-field
// mirroring already established on CatalogProduct.AttributeMapping.
const (
	TierRule       = "rule"
	TierLearned    = "learned"
	TierUnresolved = "unresolved"
)

// FieldCatalogue is the subset of the UCP definitions catalogue the resolve
// stage depends on: the target field model (catalogue.Catalogue satisfies
// this automatically). Kept minimal so the resolver depends only on what it
// uses, not the full catalogue surface.
type FieldCatalogue interface {
	AttributeFields() []v1cat.AttributeField
}

// FieldResolution is the resolve stage's per-target-field result.
type FieldResolution struct {
	SourceAttr string  // the raw attribute that resolved this field; empty if unresolved
	Value      string  // the resolved (transformed/normalised) value; empty if unresolved
	Confidence float64 // the resolving rule's confidence; 0 if unresolved
	Tier       string  // rule | learned | unresolved
}

// MappedProduct is Domain 2a stage 1 (the "resolve" stage)'s output: every
// UCP/Google target field the catalogue defines, resolved against a
// product's raw attributes via the ontology's rule and learned tiers.
type MappedProduct struct {
	Fields map[string]FieldResolution
}

// AttributeMappings converts the resolution into the catalog store's
// persisted mappings-slot shape, sorted by target field for deterministic
// writes.
func (mp *MappedProduct) AttributeMappings() []AttributeMapping {
	out := lo.MapToSlice(mp.Fields, func(field string, res FieldResolution) AttributeMapping {
		return AttributeMapping{
			SourceAttribute:  res.SourceAttr,
			CatalogAttribute: field,
			Tier:             res.Tier,
			Confidence:       res.Confidence,
			Value:            res.Value,
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].CatalogAttribute < out[j].CatalogAttribute })
	return out
}

// ApplyTo writes the resolution onto product: the mappings slot
// (AttributeMappings, with per-field tier + confidence) and, for resolved
// fields only, the legacy flat MappedFields map that scoring (stage 3, out
// of scope here) already reads. Resolve's output is input to scoring, not
// scoring itself.
func (mp *MappedProduct) ApplyTo(product *CatalogProduct) {
	product.AttributeMappings = mp.AttributeMappings()
	if product.MappedFields == nil {
		product.MappedFields = make(map[string]string)
	}
	for field, res := range mp.Fields {
		if res.Tier == TierUnresolved {
			continue
		}
		product.MappedFields[field] = res.Value
	}
}

// Resolver implements Domain 2a stage 1 (the "resolve" stage): mapping a
// normalised product's raw attributes onto the catalogue's UCP/Google target
// fields via the ontology's rule and learned tiers.
type Resolver interface {
	// Resolve is deterministic: the same product + the same ontology rule
	// set always produces the same MappedProduct.
	Resolve(ctx context.Context, product *CatalogProduct) (*MappedProduct, error)
}

// OntologyResolver implements Resolver against the cross-tenant
// OntologyStore (keyed by attribute name/platform only, never store or
// merchant) and a target field set read from the UCP definitions catalogue.
type OntologyResolver struct {
	ontology     OntologyStore
	targetFields []v1cat.AttributeField
	scope        SurfaceScope
	log          *slog.Logger
}

// NewOntologyResolver builds a resolver whose target field set is read from
// the given catalogue (versioned, loadable content — never hardcoded here).
func NewOntologyResolver(ontology OntologyStore, cat FieldCatalogue, log *slog.Logger) *OntologyResolver {
	if log == nil {
		log = slog.Default()
	}
	var fields []v1cat.AttributeField
	if cat != nil {
		fields = cat.AttributeFields()
	}
	return &OntologyResolver{ontology: ontology, targetFields: fields, scope: noSurfaces{}, log: log}
}

// WithSurfaceScope makes the resolver emit a mapping row only for the target
// fields that apply to the product's store: core fields always, and a
// surface-scoped field (e.g. Google's brand/mpn/gender) only for a store on
// that surface. Without a scope, only core fields resolve.
func (r *OntologyResolver) WithSurfaceScope(scope SurfaceScope) *OntologyResolver {
	if scope != nil {
		r.scope = scope
	}
	return r
}

// Resolve runs the rule tier, then (only where the rule tier found nothing)
// the learned tier, for every catalogue target field, applying the
// ontology's transform function to normalise the resolved value. A field
// with no matching candidate in either tier is marked unresolved — never
// guessed, never dropped.
func (r *OntologyResolver) Resolve(ctx context.Context, product *CatalogProduct) (*MappedProduct, error) {
	rules, err := r.ontology.GetRulesByPlatform(ctx, "*")
	if err != nil {
		return nil, fmt.Errorf("load ontology rules: %w", err)
	}

	byTargetField := lo.GroupBy(rules, func(rule OntologyRule) string { return rule.TargetField })

	applicable, err := r.scope.ApplicableSurfaces(ctx, product.MerchantID, product.StoreID)
	if err != nil {
		return nil, fmt.Errorf("resolve applicable surfaces: %w", err)
	}

	normalizedRaw := normalizeRawAttributes(product.RawAttributes)

	result := &MappedProduct{Fields: make(map[string]FieldResolution, len(r.targetFields))}
	for _, field := range r.targetFields {
		if field.Surface != "" && !applicable[field.Surface] {
			continue
		}
		result.Fields[field.ID] = r.resolveField(normalizedRaw, byTargetField[field.ID])
	}
	return result, nil
}

// resolveField picks the best candidate for a single target field: the
// highest-confidence rule-tier candidate whose source attribute is present
// on the product, or — only if no rule-tier candidate exists — the
// highest-confidence learned-tier candidate. Rule always beats learned,
// regardless of confidence. Ties within a tier are broken by rule ID so the
// choice never depends on map/slice iteration order.
func (r *OntologyResolver) resolveField(normalizedRaw map[string]normalizedAttr, candidates []OntologyRule) FieldResolution {
	pick := bestCandidate(candidates, TierRule, normalizedRaw)
	if pick == nil {
		pick = bestCandidate(candidates, TierLearned, normalizedRaw)
	}
	if pick == nil {
		return FieldResolution{Tier: TierUnresolved}
	}

	attr := normalizedRaw[normalizeAttrName(pick.SourceAttribute)]
	value, err := r.ontology.ApplyRule(*pick, attr.value)
	if err != nil {
		r.log.Warn("ontology transform failed, leaving field unresolved",
			"rule_id", pick.ID, "target_field", pick.TargetField, "error", err)
		return FieldResolution{Tier: TierUnresolved}
	}

	return FieldResolution{
		SourceAttr: attr.original,
		Value:      value,
		Confidence: pick.Confidence,
		Tier:       pick.Tier,
	}
}

// bestCandidate returns the highest-confidence rule of the given tier whose
// source attribute is present (by normalised-name match) on the product, or
// nil if none match. Ties are broken by the smaller rule ID.
func bestCandidate(candidates []OntologyRule, tier string, normalizedRaw map[string]normalizedAttr) *OntologyRule {
	var best *OntologyRule
	for i := range candidates {
		c := candidates[i]
		if c.Tier != tier {
			continue
		}
		if _, present := normalizedRaw[normalizeAttrName(c.SourceAttribute)]; !present {
			continue
		}
		if best == nil || c.Confidence > best.Confidence || (c.Confidence == best.Confidence && c.ID < best.ID) {
			best = &c
		}
	}
	return best
}

// normalizedAttr is a raw attribute's original key and value, indexed by
// normalised name.
type normalizedAttr struct {
	original string
	value    string
}

// normalizeRawAttributes indexes a product's raw attributes by normalised
// name (see normalizeAttrName), so an ontology rule's source_attr matches
// regardless of the merchant platform's exact casing/spacing.
func normalizeRawAttributes(raw map[string]string) map[string]normalizedAttr {
	out := make(map[string]normalizedAttr, len(raw))
	for k, v := range raw {
		out[normalizeAttrName(k)] = normalizedAttr{original: k, value: v}
	}
	return out
}

// normalizeAttrName canonicalises a raw attribute name for matching: case,
// leading/trailing space, and separator-insensitive, so "Color", "COLOR",
// and "color " all match a rule's source_attr "color" (a "normalised-name
// match" per the rule tier's spec). Exact and synonym matches both reduce to
// this same comparison — a synonym is simply a second ontology rule document
// with a different source_attr targeting the same field.
func normalizeAttrName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	return strings.NewReplacer(" ", "_", "-", "_").Replace(name)
}

// ResolveAndPersist runs the resolve stage for a single product and writes
// the result to the product's mappings slot via the catalog store's write
// seam (the CatalogStore interface, not a raw collection write).
func ResolveAndPersist(ctx context.Context, resolver Resolver, store CatalogStore, product *CatalogProduct) (*MappedProduct, error) {
	mapped, err := resolver.Resolve(ctx, product)
	if err != nil {
		return nil, fmt.Errorf("resolve product mapping: %w", err)
	}

	mapped.ApplyTo(product)

	if err := store.UpsertProduct(ctx, product); err != nil {
		return nil, fmt.Errorf("persist mapped product: %w", err)
	}

	return mapped, nil
}
