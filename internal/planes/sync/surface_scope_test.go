// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/catalogue"
	v1cat "github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/catalogue/v1"
)

// UpsertRule makes the in-memory ontology a valid SeedOntology target.
func (fos *fakeOntologyStore) UpsertRule(_ context.Context, rule OntologyRule) error {
	fos.rules[rule.ID] = rule
	return nil
}

type fixedScope struct {
	surfaces map[string]bool
	err      error
	calls    int
}

func (f *fixedScope) ApplicableSurfaces(context.Context, string, string) (map[string]bool, error) {
	f.calls++
	return f.surfaces, f.err
}

func seededResolver(t *testing.T, scope SurfaceScope) *OntologyResolver {
	t.Helper()
	store := newFakeOntologyStore()
	if err := SeedOntology(context.Background(), store, slog.Default()); err != nil {
		t.Fatalf("SeedOntology: %v", err)
	}
	cat, err := catalogue.LoadCatalogue(context.Background(), slog.Default())
	if err != nil {
		t.Fatalf("load catalogue: %v", err)
	}
	return NewOntologyResolver(store, cat, slog.Default()).WithSurfaceScope(scope)
}

func resolved(t *testing.T, r *OntologyResolver, raw map[string]string) *MappedProduct {
	t.Helper()
	mp, err := r.Resolve(context.Background(), &CatalogProduct{MerchantID: "m1", StoreID: "s1", RawAttributes: raw})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return mp
}

// The reported symptom: a store that has weight and dimensions showed them as
// "missing" and every mapping row as "Not mapped", because nothing seeded the
// ontology. Seed -> resolve -> score has to clear both.
func TestSeededOntologyResolvesStoreAttributesAndClearsDeliveryIssues(t *testing.T) {
	r := seededResolver(t, noSurfaces{})
	p := scoredProduct()
	p.MappedFields = map[string]string{}
	p.RawAttributes = map[string]string{"weight": "0.5", "dimensions": "10x10x10", "Colour": "Red", "type": "simple"}

	mp := resolved(t, r, p.RawAttributes)
	mp.ApplyTo(p)

	for field, want := range map[string]string{"shipping_weight": "0.5", "shipping_dimensions": "10x10x10", "color": "Red"} {
		got := mp.Fields[field]
		if got.Tier != TierRule || got.Value != want {
			t.Errorf("%s = %+v, want a rule-tier mapping of %q", field, got, want)
		}
	}
	if mp.Fields["size"].Tier != TierUnresolved {
		t.Errorf("size has no source attribute and must stay unresolved, got %+v", mp.Fields["size"])
	}

	res := EvaluateReadiness(p, testRubric(t), nil)
	if res.Dimensions.DeliveryData != 100 || res.Overall != 100 {
		t.Errorf("delivery/overall = %d/%d, want 100/100 (issues: %+v)", res.Dimensions.DeliveryData, res.Overall, res.Issues)
	}
}

func TestSeedOntologyIsIdempotentAndOwnsOnlySeedRows(t *testing.T) {
	store := newFakeOntologyStore()
	store.rules["learned-1"] = OntologyRule{ID: "learned-1", SourceAttribute: "tint", TargetField: "color", Tier: TierLearned}

	for i := range 2 {
		if err := SeedOntology(context.Background(), store, nil); err != nil {
			t.Fatalf("SeedOntology (%d): %v", i, err)
		}
	}
	if want := len(v1cat.DefaultOntologySeed()) + 1; len(store.rules) != want {
		t.Errorf("rules = %d after seeding twice, want %d (seed rows + the learned rule, no duplicates)", len(store.rules), want)
	}
	if store.rules["learned-1"].SourceAttribute != "tint" {
		t.Error("a learned rule must be left untouched by seeding")
	}
}

// A surface-scoped field is shown, resolved and scored only for a store on
// that surface — a surface is not a property of every store.
func TestSurfaceScopedFieldsResolveOnlyForStoresOnTheSurface(t *testing.T) {
	const scopedSurface = "example_surface"
	raw := map[string]string{"example_attr": "Acme", "color": "Red"}

	newResolver := func(scope SurfaceScope) *OntologyResolver {
		r := seededResolver(t, scope)
		store, ok := r.ontology.(*fakeOntologyStore)
		if !ok {
			t.Fatalf("ontology is %T, want *fakeOntologyStore", r.ontology)
		}
		r.targetFields = append(r.targetFields, v1cat.AttributeField{ID: "example_attr", Description: "scoped", Surface: scopedSurface})
		store.rules["example"] = OntologyRule{
			ID: "example", SourceAttribute: "example_attr", TargetField: "example_attr",
			Platform: "*", Tier: TierRule, Confidence: 0.9, TransformFn: "identity",
		}
		return r
	}

	t.Run("core only", func(t *testing.T) {
		mp := resolved(t, newResolver(&fixedScope{}), raw)
		if _, ok := mp.Fields["example_attr"]; ok {
			t.Error("a scoped field must not be resolved (or shown) for a store not on the surface")
		}
		if mp.Fields["color"].Value != "Red" {
			t.Errorf("core field color should still resolve, got %+v", mp.Fields["color"])
		}
	})

	t.Run("on the surface", func(t *testing.T) {
		mp := resolved(t, newResolver(&fixedScope{surfaces: map[string]bool{scopedSurface: true}}), raw)
		if mp.Fields["example_attr"].Value != "Acme" {
			t.Errorf("scoped field should resolve for a store on the surface, got %+v", mp.Fields["example_attr"])
		}
	})
}

func TestResolverFailsWhenSurfacesCannotBeRead(t *testing.T) {
	r := seededResolver(t, &fixedScope{err: errors.New("mongo down")})
	if _, err := r.Resolve(context.Background(), &CatalogProduct{MerchantID: "m1", StoreID: "s1"}); err == nil {
		t.Fatal("a failed surface read must fail the resolve (retry), not silently drop the surface's fields")
	}
}

func TestRubricForSurfacesScopesRulesAndDimensionWeights(t *testing.T) {
	rubric := testRubric(t)
	rubric.ScoringRules = append(rubric.ScoringRules, ScoringRule{
		ID: "rule_example_missing", Dimension: "completeness", Field: "example_attr", Condition: "missing",
		Weight: 100, Surface: "example_surface",
	})
	p := scoredProduct() // has no example_attr

	core := rubric.ForSurfaces(nil)
	if len(core.ScoringRules) != len(rubric.ScoringRules)-1 {
		t.Fatalf("core rubric should drop the one scoped rule, got %d of %d", len(core.ScoringRules), len(rubric.ScoringRules))
	}
	coreRes := EvaluateReadiness(p, core, nil)
	if _, ok := issueIDs(coreRes)["rule_example_missing"]; ok || coreRes.Dimensions.Completeness != 100 {
		t.Errorf("a store off the surface must not be marked down for example_attr: %+v", coreRes)
	}

	onSurface := rubric.ForSurfaces(map[string]bool{"example_surface": true})
	res := EvaluateReadiness(p, onSurface, nil)
	if _, ok := issueIDs(res)["rule_example_missing"]; !ok || res.Dimensions.Completeness >= 100 {
		t.Errorf("a store on the surface must be scored on example_attr: %+v", res)
	}

	if len(rubric.ScoringRules) != len(core.ScoringRules)+1 {
		t.Error("ForSurfaces must not modify the receiver")
	}
}

func TestCachedSurfaceScope(t *testing.T) {
	inner := &fixedScope{surfaces: map[string]bool{"a": true}}
	c := NewCachedSurfaceScope(inner, time.Minute)
	now := time.Now()
	c.now = func() time.Time { return now }
	ctx := context.Background()

	for range 3 {
		if _, err := c.ApplicableSurfaces(ctx, "m1", "s1"); err != nil {
			t.Fatal(err)
		}
	}
	if inner.calls != 1 {
		t.Errorf("inner read %d times, want 1 (cached)", inner.calls)
	}

	// Another store is a different entry.
	_, _ = c.ApplicableSurfaces(ctx, "m1", "s2")
	if inner.calls != 2 {
		t.Errorf("a second store must read through, calls=%d", inner.calls)
	}

	// Invalidate drops one store; expiry drops the rest.
	c.Invalidate("m1", "s1")
	_, _ = c.ApplicableSurfaces(ctx, "m1", "s1")
	if inner.calls != 3 {
		t.Errorf("after Invalidate the store must be re-read, calls=%d", inner.calls)
	}
	now = now.Add(2 * time.Minute)
	_, _ = c.ApplicableSurfaces(ctx, "m1", "s2")
	if inner.calls != 4 {
		t.Errorf("after the TTL the store must be re-read, calls=%d", inner.calls)
	}

	// A failed read is not cached.
	inner.err = errors.New("boom")
	c.Invalidate("m1", "s1")
	if _, err := c.ApplicableSurfaces(ctx, "m1", "s1"); err == nil {
		t.Error("error should propagate")
	}
	inner.err = nil
	if _, err := c.ApplicableSurfaces(ctx, "m1", "s1"); err != nil {
		t.Errorf("a failure must not be cached: %v", err)
	}
}
