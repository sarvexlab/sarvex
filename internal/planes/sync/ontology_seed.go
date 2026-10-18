// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	mongoopts "go.mongodb.org/mongo-driver/v2/mongo/options"

	v1cat "github.com/sarvexlab/sarvex/internal/pkg/ucp/catalogue/v1"
)

// OntologyRuleUpserter is the write seam SeedOntology needs. The sync plane is
// the ontology's one writer (sync_ontology), so seeding lives here, not in the
// control plane or the catalogue package.
type OntologyRuleUpserter interface {
	UpsertRule(ctx context.Context, rule OntologyRule) error
}

// UpsertRule inserts or updates one ontology rule by id. created_at is kept
// from the first insert.
func (mos *MongoOntologyStore) UpsertRule(ctx context.Context, rule OntologyRule) error {
	now := time.Now().UTC()
	_, err := mos.db.Collection("sync_ontology").UpdateOne(ctx,
		bson.M{"_id": rule.ID},
		bson.M{
			"$set": bson.M{
				"source_attr":      rule.SourceAttribute,
				"target_field":     rule.TargetField,
				"platform":         rule.Platform,
				"tier":             rule.Tier,
				"confidence":       rule.Confidence,
				"transform_fn":     rule.TransformFn,
				"transform_config": rule.TransformConfig,
				"updated_at":       now,
			},
			"$setOnInsert": bson.M{"created_at": now},
		},
		mongoopts.UpdateOne().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("upsert ontology rule %s: %w", rule.ID, err)
	}
	return nil
}

// SeedOntology loads the catalogue's rule-tier seed mappings into the
// ontology. Idempotent, and it only ever writes rows under the seed id prefix,
// so learned-tier and hand-added rules are never touched.
func SeedOntology(ctx context.Context, store OntologyRuleUpserter, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}
	seed := v1cat.DefaultOntologySeed()
	for _, r := range seed {
		rule := OntologyRule{
			ID:              ontologySeedRuleID(r),
			SourceAttribute: r.SourceAttribute,
			TargetField:     r.TargetField,
			Platform:        "*",
			Tier:            TierRule,
			Confidence:      r.Confidence,
			TransformFn:     "identity",
		}
		if err := store.UpsertRule(ctx, rule); err != nil {
			return err
		}
	}
	log.Info("seeded ontology", "rules", len(seed))
	return nil
}

func ontologySeedRuleID(r v1cat.OntologySeedRule) string {
	return v1cat.OntologySeedIDPrefix + r.TargetField + ":" + r.SourceAttribute
}
