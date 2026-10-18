// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"testing"
	"time"
)

// TestMongoOntologyStore_GetRulesByPlatform_MultipleRulesSortedCorrectly
// exercises the real MongoDB sort path with more than one matching rule.
// GetRulesByPlatform previously passed a multi-key bson.M (an unordered Go
// map) as the sort spec, which the MongoDB driver rejects once there's more
// than one rule for a platform ("multi-key map passed in for ordered
// parameter sort") — invisible to any prior test since none exercised this
// query against a real database with more than one rule.
func TestMongoOntologyStore_GetRulesByPlatform_MultipleRulesSortedCorrectly(t *testing.T) {
	db := connectTestMongo(t)
	store := NewMongoOntologyStore(db)

	now := time.Now().UTC()
	rules := []OntologyRule{
		{ID: "rule-low", SourceAttribute: "size", TargetField: "size", Platform: "woocommerce", Tier: "rule", Confidence: 0.5, TransformFn: "identity", CreatedAt: now, UpdatedAt: now},
		{ID: "rule-high", SourceAttribute: "title", TargetField: "title", Platform: "woocommerce", Tier: "rule", Confidence: 0.9, TransformFn: "identity", CreatedAt: now, UpdatedAt: now},
		{ID: "rule-wildcard", SourceAttribute: "color", TargetField: "color", Platform: "*", Tier: "learned", Confidence: 0.7, TransformFn: "identity", CreatedAt: now, UpdatedAt: now},
		{ID: "rule-other-platform", SourceAttribute: "size", TargetField: "size", Platform: "magento", Tier: "rule", Confidence: 0.99, TransformFn: "identity", CreatedAt: now, UpdatedAt: now},
	}
	coll := db.Collection("sync_ontology")
	for _, r := range rules {
		if _, err := coll.InsertOne(context.Background(), r); err != nil {
			t.Fatalf("seed rule %s: %v", r.ID, err)
		}
	}

	got, err := store.GetRulesByPlatform(context.Background(), "woocommerce")
	if err != nil {
		t.Fatalf("GetRulesByPlatform: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("expected 3 rules (woocommerce + wildcard, not magento), got %d: %+v", len(got), got)
	}
	if got[0].ID != "rule-high" {
		t.Errorf("expected highest-confidence rule first, got %q", got[0].ID)
	}
	for _, r := range got {
		if r.ID == "rule-other-platform" {
			t.Errorf("magento-only rule leaked into woocommerce query: %+v", r)
		}
	}
}
