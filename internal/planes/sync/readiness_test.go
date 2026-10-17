// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp"
)

// fixtureRubric is a small, hand-built rubric with exactly one rule per
// dimension, each weight 100 — this makes dimension/overall arithmetic
// exact and easy to hand-verify, unlike the full catalogue rubric.
func fixtureRubric() *ScoringRubric {
	return &ScoringRubric{
		Version:          "fixture-1",
		CatalogueVersion: "cat-fixture-1",
		DimensionWeights: map[string]int{
			"completeness":      40,
			"consistency":       20,
			"attribute_quality": 20,
			"delivery_data":     20,
		},
		ScoringRules: []ScoringRule{
			{ID: "r_title_missing", Dimension: "completeness", Field: "title", Condition: "missing", BlocksCheckout: true, Weight: 100, RemediationID: "rem_title"},
			{ID: "r_desc_missing", Dimension: "completeness", Field: "description", Condition: "missing", BlocksCheckout: false, Weight: 50, RemediationID: "rem_desc"},
			{ID: "r_price_inconsistent", Dimension: "consistency", Field: "price", Condition: "inconsistent", BlocksCheckout: true, Weight: 100, RemediationID: "rem_price"},
			{ID: "r_size_freetext", Dimension: "attribute_quality", Field: "size", Condition: "free_text", BlocksCheckout: false, Weight: 100, RemediationID: "rem_size"},
			{ID: "r_ship_weight_missing", Dimension: "delivery_data", Field: "shipping_weight", Condition: "missing", BlocksCheckout: false, Weight: 100, RemediationID: "rem_ship"},
		},
		Remediations: []RemediationRule{
			{ID: "rem_title", ScoringRuleID: "r_title_missing", Field: "title", Action: "error", Description: "Title is required; cannot auto-remediate"},
			{ID: "rem_desc", ScoringRuleID: "r_desc_missing", Field: "description", Action: "default", Description: "Default description applied"},
			{ID: "rem_price", ScoringRuleID: "r_price_inconsistent", Field: "price", Action: "error", Description: "Price inconsistent across sources; merchant must reconcile"},
			{ID: "rem_size", ScoringRuleID: "r_size_freetext", Field: "size", Action: "normalize", Description: "Normalize size to the standard enum"},
			{ID: "rem_ship", ScoringRuleID: "r_ship_weight_missing", Field: "shipping_weight", Action: "default", Description: "Default shipping weight applied"},
		},
	}
}

// completeFixtureProduct passes every fixtureRubric rule: title and
// description set, no raw "size" attribute (so free_text never triggers),
// shipping_weight set, and (with no findings passed) no consistency issue.
func completeFixtureProduct() *CatalogProduct {
	return &CatalogProduct{
		ID:            "m1:s1:SKU-1",
		MerchantID:    "m1",
		StoreID:       "s1",
		SKU:           "SKU-1",
		Title:         "Complete Product",
		RawAttributes: map[string]string{},
		MappedFields: map[string]string{
			"description":     "A complete description",
			"shipping_weight": "1.2kg",
		},
	}
}

func TestEvaluateReadiness_CompleteProductScoresHighEmptyIssues(t *testing.T) {
	product := completeFixtureProduct()
	rubric := fixtureRubric()

	result := EvaluateReadiness(product, rubric, nil)

	if result.Overall != 100 {
		t.Errorf("expected overall 100, got %d", result.Overall)
	}
	want := ReadinessDimensions{Completeness: 100, Consistency: 100, AttributeQuality: 100, DeliveryData: 100}
	if result.Dimensions != want {
		t.Errorf("expected all dimensions 100, got %+v", result.Dimensions)
	}
	if len(result.Issues) != 0 {
		t.Errorf("expected no issues, got %d: %+v", len(result.Issues), result.Issues)
	}
}

func TestEvaluateReadiness_MissingRequiredAttributeBlocksAndLowersCompleteness(t *testing.T) {
	product := completeFixtureProduct()
	product.Title = "" // triggers r_title_missing (blocking, completeness)
	rubric := fixtureRubric()

	result := EvaluateReadiness(product, rubric, nil)

	if result.Dimensions.Completeness >= 100 {
		t.Errorf("expected completeness below 100 when a completeness rule triggers, got %d", result.Dimensions.Completeness)
	}
	if !anyBlocksCheckout(result.Issues) {
		t.Error("missing title should produce a blocking issue")
	}

	var found *ProductIssue
	for i := range result.Issues {
		if result.Issues[i].IssueID == "r_title_missing" {
			found = &result.Issues[i]
		}
	}
	if found == nil {
		t.Fatal("expected issue for r_title_missing")
	}
	if found.Field != "title" || found.Problem != "missing" || found.Dimension != "completeness" {
		t.Errorf("unexpected issue shape: %+v", found)
	}
	if !found.BlocksCheckout {
		t.Error("expected BlocksCheckout=true per rubric")
	}
	if found.Remediable != "manual" {
		t.Errorf("title remediation action is 'error' -> should be manual, got %q", found.Remediable)
	}
	if found.WhyItMatters == "" {
		t.Error("expected why_it_matters to be populated")
	}
	if found.RevenueImpactMinor != nil {
		t.Error("revenue impact must be nil until traffic data exists")
	}
}

func TestEvaluateReadiness_ConsistencyDimensionFedByFindingsNotProduct(t *testing.T) {
	rubric := fixtureRubric()

	// No findings: consistency rule must not trigger, regardless of product state.
	clean := completeFixtureProduct()
	cleanResult := EvaluateReadiness(clean, rubric, nil)
	if cleanResult.Dimensions.Consistency != 100 {
		t.Errorf("expected consistency 100 with no findings, got %d", cleanResult.Dimensions.Consistency)
	}

	// A consistency finding on "price" must lower the consistency dimension
	// and produce an issue, even though nothing about the product itself changed.
	withFinding := completeFixtureProduct()
	findings := []ConsistencyIssue{{Field: "price", Type: "divergence", Severity: "critical", Description: "price diverged"}}
	result := EvaluateReadiness(withFinding, rubric, findings)

	if result.Dimensions.Consistency != 0 {
		t.Errorf("expected consistency 0 when a finding is fed in, got %d", result.Dimensions.Consistency)
	}
	if result.Overall >= cleanResult.Overall {
		t.Errorf("overall should drop when consistency finding is present: clean=%d, withFinding=%d", cleanResult.Overall, result.Overall)
	}

	found := false
	for _, iss := range result.Issues {
		if iss.IssueID == "r_price_inconsistent" {
			found = true
			if iss.Problem != "inconsistent" || iss.Dimension != "consistency" {
				t.Errorf("unexpected issue shape: %+v", iss)
			}
			if !iss.BlocksCheckout {
				t.Error("price inconsistency is configured to block checkout")
			}
		}
	}
	if !found {
		t.Error("expected an issue for r_price_inconsistent")
	}
}

// singleRuleRubric has exactly one rule per dimension (weight 100 each), so
// a triggered rule always yields dimension score 0 and an untriggered one
// always yields 100 — this makes overall-composition arithmetic exact.
func singleRuleRubric() *ScoringRubric {
	return &ScoringRubric{
		Version:          "single-rule-1",
		CatalogueVersion: "cat-single-1",
		DimensionWeights: map[string]int{
			"completeness":      40,
			"consistency":       20,
			"attribute_quality": 20,
			"delivery_data":     20,
		},
		ScoringRules: []ScoringRule{
			{ID: "r_title_missing", Dimension: "completeness", Field: "title", Condition: "missing", BlocksCheckout: true, Weight: 100, RemediationID: "rem_title"},
			{ID: "r_price_inconsistent", Dimension: "consistency", Field: "price", Condition: "inconsistent", BlocksCheckout: true, Weight: 100, RemediationID: "rem_price"},
			{ID: "r_size_freetext", Dimension: "attribute_quality", Field: "size", Condition: "free_text", BlocksCheckout: false, Weight: 100, RemediationID: "rem_size"},
			{ID: "r_ship_weight_missing", Dimension: "delivery_data", Field: "shipping_weight", Condition: "missing", BlocksCheckout: false, Weight: 100, RemediationID: "rem_ship"},
		},
		Remediations: []RemediationRule{
			{ID: "rem_title", ScoringRuleID: "r_title_missing", Field: "title", Action: "error", Description: "Title required"},
			{ID: "rem_price", ScoringRuleID: "r_price_inconsistent", Field: "price", Action: "error", Description: "Price inconsistent"},
			{ID: "rem_size", ScoringRuleID: "r_size_freetext", Field: "size", Action: "normalize", Description: "Normalize size"},
			{ID: "rem_ship", ScoringRuleID: "r_ship_weight_missing", Field: "shipping_weight", Action: "default", Description: "Default shipping weight"},
		},
	}
}

func TestEvaluateReadiness_OverallComposition(t *testing.T) {
	product := completeFixtureProduct()
	product.Title = "" // trigger only the completeness rule
	rubric := singleRuleRubric()

	result := EvaluateReadiness(product, rubric, nil)

	want := ReadinessDimensions{Completeness: 0, Consistency: 100, AttributeQuality: 100, DeliveryData: 100}
	if result.Dimensions != want {
		t.Fatalf("unexpected dimensions: %+v", result.Dimensions)
	}
	// weights: completeness 40, consistency 20, attribute_quality 20, delivery_data 20 (sum 100)
	// overall = (0*40 + 100*20 + 100*20 + 100*20) / 100 = 6000/100 = 60
	if result.Overall != 60 {
		t.Errorf("expected overall 60 from weighted composition, got %d", result.Overall)
	}
}

func TestEvaluateReadiness_ThresholdPredicate_ZeroBlockers(t *testing.T) {
	rubric := fixtureRubric()

	clean := completeFixtureProduct()
	if r := EvaluateReadiness(clean, rubric, nil); anyBlocksCheckout(r.Issues) {
		t.Error("clean product should not block checkout")
	}

	blocked := completeFixtureProduct()
	blocked.Title = ""
	if r := EvaluateReadiness(blocked, rubric, nil); !anyBlocksCheckout(r.Issues) {
		t.Error("product missing title should block checkout")
	}
}

func TestEvaluateReadiness_IssueSortFallsBackToBlocksCheckoutThenWeight(t *testing.T) {
	product := completeFixtureProduct()
	product.Title = ""                              // r_title_missing: blocking, weight 100
	product.MappedFields["description"] = ""        // r_desc_missing: non-blocking, weight 50
	delete(product.MappedFields, "shipping_weight") // r_ship_weight_missing: non-blocking, weight 100
	rubric := fixtureRubric()

	result := EvaluateReadiness(product, rubric, nil)
	if len(result.Issues) != 3 {
		t.Fatalf("expected 3 issues, got %d: %+v", len(result.Issues), result.Issues)
	}

	// None have a revenue impact (V1 has no traffic data), so the sort falls
	// back to blocks_checkout first, then weight: title (blocking) must lead;
	// among the two non-blocking issues, the heavier weight (ship, 100) must
	// precede the lighter one (desc, 50).
	if result.Issues[0].IssueID != "r_title_missing" {
		t.Errorf("expected blocking issue first, got %s", result.Issues[0].IssueID)
	}
	if result.Issues[1].IssueID != "r_ship_weight_missing" || result.Issues[2].IssueID != "r_desc_missing" {
		t.Errorf("expected non-blocking issues ordered by weight desc (ship, then desc), got %s, %s",
			result.Issues[1].IssueID, result.Issues[2].IssueID)
	}
}

func TestEvaluateReadiness_CatalogueVersionPinned(t *testing.T) {
	rubric := fixtureRubric()
	rubric.CatalogueVersion = "2099-01-01"

	result := EvaluateReadiness(completeFixtureProduct(), rubric, nil)
	if result.CatalogueVersion != "2099-01-01" {
		t.Errorf("expected result pinned to rubric's catalogue version, got %q", result.CatalogueVersion)
	}
}

func TestEvaluateReadiness_RubricChangeAltersScore_NoCodeChange(t *testing.T) {
	product := completeFixtureProduct()
	product.Title = "" // fails the completeness rule only

	rubricA := singleRuleRubric() // completeness weight 40
	rubricB := singleRuleRubric()
	rubricB.DimensionWeights = map[string]int{
		"completeness":      100,
		"consistency":       0,
		"attribute_quality": 0,
		"delivery_data":     0,
	}

	resultA := EvaluateReadiness(product, rubricA, nil)
	resultB := EvaluateReadiness(product, rubricB, nil)

	if resultA.Overall == resultB.Overall {
		t.Fatalf("expected different overall scores from different catalogue-content weighting, got %d for both", resultA.Overall)
	}
	if resultB.Overall != 0 {
		t.Errorf("with all weight on the failed dimension, overall should be 0, got %d", resultB.Overall)
	}
}

// ── ScoreAndPersist integration-style test (in-memory fakes; no external deps) ──

// fakeReadinessAggregateStore is an in-memory ReadinessAggregateStore for
// unit/integration-style tests, mirroring the fakeCatalogStore/fakeEventBus
// pattern in testing_helpers_test.go.
type fakeReadinessAggregateStore struct {
	records map[string]*StoreReadinessRecord // keyed by store_id
}

func newFakeReadinessAggregateStore() *fakeReadinessAggregateStore {
	return &fakeReadinessAggregateStore{records: make(map[string]*StoreReadinessRecord)}
}

func (f *fakeReadinessAggregateStore) GetStoreReadiness(ctx context.Context, storeID string) (*StoreReadinessRecord, error) {
	rec, ok := f.records[storeID]
	if !ok {
		return nil, ErrStoreReadinessNotFound
	}
	return rec, nil
}

func (f *fakeReadinessAggregateStore) UpsertStoreReadiness(ctx context.Context, rec *StoreReadinessRecord) error {
	f.records[rec.StoreID] = rec
	return nil
}

func journeySignals(t *testing.T, bus *fakeEventBus) []string {
	t.Helper()
	var signals []string
	for _, evt := range bus.published {
		if evt.EventType() != ucp.EventJourneySignal {
			continue
		}
		var payload JourneySignalPayload
		if err := json.Unmarshal(evt.Payload(), &payload); err != nil {
			t.Fatalf("unmarshal journey.signal: %v", err)
		}
		signals = append(signals, payload.Signal)
	}
	return signals
}

func rebuildEvents(t *testing.T, bus *fakeEventBus) []rebuildRequestPayload {
	t.Helper()
	var out []rebuildRequestPayload
	for _, evt := range bus.published {
		if evt.EventType() != ucp.EventRebuildRequested {
			continue
		}
		var payload rebuildRequestPayload
		if err := json.Unmarshal(evt.Payload(), &payload); err != nil {
			t.Fatalf("unmarshal serving.rebuild_requested: %v", err)
		}
		out = append(out, payload)
	}
	return out
}

// TestScoreAndPersist_Integration exercises the full stage-3 write seam
// against the real UCP definitions catalogue rubric: mapped product +
// catalogue + a consistency finding -> ReadinessResult persisted, a serving
// cache rebuild requested, the store-level aggregate recomputed, and the
// journey threshold signal firing exactly on each zero-blockers crossing.
func TestScoreAndPersist_Integration(t *testing.T) {
	ctx := context.Background()
	rubric := testRubric(t)

	store := newFakeCatalogStore()
	bus := newFakeEventBus()
	readiness := newFakeReadinessAggregateStore()
	deps := ScoreDeps{Store: store, Bus: bus, Readiness: readiness}

	product := &CatalogProduct{
		MerchantID: "merch-a",
		StoreID:    "store-a",
		SKU:        "SKU-1",
		Title:      "Widget",
		Currency:   "USD",
		PriceMinor: 0, // triggers rule_price_missing (blocking)
		Images:     []string{"https://example.test/widget.jpg"},
	}

	// 1. First score: one blocker (price missing). No prior state, so no
	// crossing yet (never was at zero blockers).
	result1, err := ScoreAndPersist(ctx, deps, product, rubric, nil, true)
	if err != nil {
		t.Fatalf("ScoreAndPersist (1): %v", err)
	}
	if !anyBlocksCheckout(result1.Issues) {
		t.Fatal("expected a blocking issue for missing price")
	}
	if product.CatalogueVersion != rubric.CatalogueVersion || product.CatalogueVersion == "" {
		t.Errorf("expected product pinned to rubric catalogue version, got %q", product.CatalogueVersion)
	}
	persisted, err := store.GetProduct(ctx, "store-a", "SKU-1")
	if err != nil {
		t.Fatalf("GetProduct: %v", err)
	}
	if len(persisted.Issues) == 0 || !persisted.BlocksCheckout {
		t.Error("expected the readiness slot to be persisted with the blocking issue")
	}

	rec, err := readiness.GetStoreReadiness(ctx, "store-a")
	if err != nil {
		t.Fatalf("GetStoreReadiness: %v", err)
	}
	if rec.ProductCount != 1 || rec.BlockingSKUCount != 1 {
		t.Errorf("expected store aggregate {count:1, blocking:1}, got %+v", rec)
	}
	if signals := journeySignals(t, bus); len(signals) != 0 {
		t.Errorf("expected no journey signal on first (still-blocked) score, got %v", signals)
	}

	// 2. Fix the price: crosses to zero blockers -> readiness_threshold_met, once.
	product.PriceMinor = 1999
	if _, err := ScoreAndPersist(ctx, deps, product, rubric, nil, true); err != nil {
		t.Fatalf("ScoreAndPersist (2): %v", err)
	}
	if signals := journeySignals(t, bus); len(signals) != 1 || signals[0] != "readiness_threshold_met" {
		t.Fatalf("expected exactly one readiness_threshold_met signal, got %v", signals)
	}
	rec, _ = readiness.GetStoreReadiness(ctx, "store-a")
	if rec.BlockingSKUCount != 0 {
		t.Errorf("expected zero blocking SKUs after fix, got %d", rec.BlockingSKUCount)
	}

	// 3. Re-score with no state change: must NOT re-fire the signal.
	if _, err := ScoreAndPersist(ctx, deps, product, rubric, nil, true); err != nil {
		t.Fatalf("ScoreAndPersist (3): %v", err)
	}
	if signals := journeySignals(t, bus); len(signals) != 1 {
		t.Fatalf("expected the signal count to stay at 1 (no re-fire), got %v", signals)
	}

	// 4. A consistency finding on price regresses the product to blocking
	// again -> crosses back -> readiness_threshold_lost, once.
	findings := []ConsistencyIssue{{Field: "price", Type: "divergence", Severity: "critical", Description: "price diverged from source"}}
	result4, err := ScoreAndPersist(ctx, deps, product, rubric, findings, true)
	if err != nil {
		t.Fatalf("ScoreAndPersist (4): %v", err)
	}
	if !anyBlocksCheckout(result4.Issues) {
		t.Fatal("expected the consistency finding to produce a blocking issue")
	}
	signals := journeySignals(t, bus)
	if len(signals) != 2 || signals[1] != "readiness_threshold_lost" {
		t.Fatalf("expected [readiness_threshold_met, readiness_threshold_lost], got %v", signals)
	}

	rebuilds := rebuildEvents(t, bus)
	if len(rebuilds) != 4 {
		t.Fatalf("expected 4 serving.rebuild_requested events (one per ScoreAndPersist call), got %d", len(rebuilds))
	}
	last := rebuilds[len(rebuilds)-1]
	if last.Reason != "product_changed" {
		t.Errorf("unexpected final serving.rebuild_requested payload: %+v", last)
	}

	// Tenant isolation sanity: a second store must never see store-a's aggregate.
	if _, err := readiness.GetStoreReadiness(ctx, "store-b"); err != ErrStoreReadinessNotFound {
		t.Errorf("expected ErrStoreReadinessNotFound for an unscored store, got %v", err)
	}
}

// TestScoreAndPersist_SkipsRebuildDuringBulkImport is a regression test: a
// bulk catalog import (RescoreProductBatch -> recomputeStoreAggregate=false)
// used to fire one serving.rebuild_requested per product on top of
// CatalogImportHandler's own single end-of-import rebuild — for a large
// catalog, thousands of redundant full-snapshot rebuilds racing each other
// non-atomically in Redis (SwapSnapshot's delete-then-set pipeline), which
// is what caused transient catalog/:itemId 404s under load-test observed
// against a live stack. recomputeStoreAggregate=false must now produce zero
// rebuild events; true (steady-state) must still produce exactly one, as
// TestScoreAndPersist_Integration above already covers.
func TestScoreAndPersist_SkipsRebuildDuringBulkImport(t *testing.T) {
	ctx := context.Background()
	rubric := testRubric(t)

	store := newFakeCatalogStore()
	bus := newFakeEventBus()
	deps := ScoreDeps{Store: store, Bus: bus, Readiness: newFakeReadinessAggregateStore()}

	for i, sku := range []string{"SKU-1", "SKU-2", "SKU-3"} {
		product := &CatalogProduct{
			MerchantID: "merch-a",
			StoreID:    "store-a",
			SKU:        sku,
			Title:      "Widget",
			Currency:   "USD",
			PriceMinor: 1999,
		}
		if _, err := ScoreAndPersist(ctx, deps, product, rubric, nil, false); err != nil {
			t.Fatalf("ScoreAndPersist (%d): %v", i, err)
		}
	}

	if rebuilds := rebuildEvents(t, bus); len(rebuilds) != 0 {
		t.Fatalf("expected zero rebuild events mid-bulk-import (recomputeStoreAggregate=false), got %d: %+v", len(rebuilds), rebuilds)
	}
	if signals := journeySignals(t, bus); len(signals) != 0 {
		t.Fatalf("expected zero journey signals mid-bulk-import, got %v", signals)
	}
}

// TestRecomputeStoreReadinessAndSignal_FreshConnectionIgnoresStalePriorAggregate
// is a regression test for a real bug: ConnectorService.Disconnect deletes
// the connector and resets the journey to Prospect, but never touches this
// store's readiness aggregate (sync-plane-owned data it has no reason to
// reach into). A store only ever gets disconnected/reconnected after it was
// already working, so the leftover aggregate almost always still reads
// BlockingSKUCount==0. On reconnect, CatalogImportHandler runs a fresh
// "initial" import and calls FinalizeBatchReadiness exactly once — without
// freshConnection, wasZeroBlockers reads true from that stale leftover, the
// fresh (also zero-blocking) computation matches it, no crossing is
// detected, and readiness_threshold_met never fires — stranding the store
// on Synced permanently, since nothing else ever re-checks a crossing that
// already silently happened.
func TestRecomputeStoreReadinessAndSignal_FreshConnectionIgnoresStalePriorAggregate(t *testing.T) {
	ctx := context.Background()

	store := newFakeCatalogStore()
	if err := store.UpsertProduct(ctx, &CatalogProduct{
		MerchantID: "merch-a", StoreID: "store-a", SKU: "SKU-1",
		Title: "Widget", Currency: "USD", PriceMinor: 1999, // no blocking issues
	}); err != nil {
		t.Fatalf("seed product: %v", err)
	}

	readiness := newFakeReadinessAggregateStore()
	// Simulate the leftover from before a disconnect: already at zero
	// blockers, exactly as a working store's aggregate would read.
	if err := readiness.UpsertStoreReadiness(ctx, &StoreReadinessRecord{
		StoreID: "store-a", MerchantID: "merch-a", BlockingSKUCount: 0, ProductCount: 1,
	}); err != nil {
		t.Fatalf("seed stale readiness: %v", err)
	}

	bus := newFakeEventBus()
	deps := ScoreDeps{Store: store, Bus: bus, Readiness: readiness}

	// Without freshConnection: reproduces the bug — the crossing is real
	// (this run has never signaled readiness_threshold_met) but is missed
	// because the stale leftover already reads zero-blocking.
	if err := recomputeStoreReadinessAndSignal(ctx, deps, "merch-a", "store-a", false); err != nil {
		t.Fatalf("recomputeStoreReadinessAndSignal (stale, not fresh): %v", err)
	}
	if signals := journeySignals(t, bus); len(signals) != 0 {
		t.Fatalf("expected no signal without freshConnection (demonstrates the bug), got %v", signals)
	}

	// With freshConnection: the same stale leftover must not suppress the signal.
	if err := recomputeStoreReadinessAndSignal(ctx, deps, "merch-a", "store-a", true); err != nil {
		t.Fatalf("recomputeStoreReadinessAndSignal (fresh): %v", err)
	}
	if signals := journeySignals(t, bus); len(signals) != 1 || signals[0] != "readiness_threshold_met" {
		t.Fatalf("expected exactly one readiness_threshold_met signal with freshConnection, got %v", signals)
	}
}
