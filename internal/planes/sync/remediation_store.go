// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// remediationsCollection holds one record per open or applied rule-based fix,
// per (store, SKU, scoring rule). The sync plane is its only writer: the score
// stage creates and retires records as issues appear and resolve, and the
// apply-remediations command marks them applied. The control plane reads it by
// name, read-only, to list remediations and validate an apply request
// (sarvex-v1-architecture.md A10.1, §2.3).
const remediationsCollection = "sync_catalog_remediations"

// ErrRemediationsNotFound is returned by GetByIDs when any requested id is not
// a remediation of the store.
var ErrRemediationsNotFound = errors.New("sync: remediation not found")

// Remediation is one rule-based fix record. Its bson shape is the contract the
// control plane's read model decodes.
type Remediation struct {
	ID         string `bson:"_id"` // store_id:sku:rule_id — stable across re-scores
	MerchantID string `bson:"merchant_id"`
	StoreID    string `bson:"store_id"`
	SKU        string `bson:"sku"`
	// RuleID is the scoring rule whose issue this fixes; RemediationRuleID is
	// the rubric remediation that fixes it.
	RuleID            string    `bson:"rule_id"`
	RemediationRuleID string    `bson:"remediation_rule_id"`
	Field             string    `bson:"field"`
	FromValue         string    `bson:"from_value"`
	ToValue           string    `bson:"to_value"`
	Type              string    `bson:"type"`  // rule | manual
	Trust             string    `bson:"trust"` // high | low
	Applied           bool      `bson:"applied"`
	WriteBackID       string    `bson:"write_back_id,omitempty"`
	CreatedAt         time.Time `bson:"created_at"`
	UpdatedAt         time.Time `bson:"updated_at"`
}

// RemediationStore is the sync plane's write seam for remediation records.
// Every method is fenced by merchant_id and store_id (docs/TENANCY-PATTERNS.md).
type RemediationStore interface {
	// SyncForSKU makes the SKU's unapplied records exactly open: it upserts
	// each (keeping applied history intact) and deletes unapplied records
	// whose issue has resolved.
	SyncForSKU(ctx context.Context, merchantID, storeID, sku string, open []Remediation) error
	// GetByIDs returns the store's records with the given ids, or
	// ErrRemediationsNotFound if any id is not one of them.
	GetByIDs(ctx context.Context, merchantID, storeID string, ids []string) ([]Remediation, error)
	// MarkApplied records that the record was applied by writeBackID, and the
	// field's value before the fix (fromValue) — what a revert restores.
	MarkApplied(ctx context.Context, merchantID, storeID, id, writeBackID, fromValue string) error
	// MarkReverted reopens a record that writeBackID applied, once its prior
	// value has been restored. A record not applied by writeBackID is left as
	// it is.
	MarkReverted(ctx context.Context, merchantID, storeID, id, writeBackID string) error
}

type mongoRemediationStore struct {
	col *mongo.Collection
}

// NewMongoRemediationStore returns the Mongo-backed RemediationStore and
// ensures the collection's indexes (the owner creates them; readers don't).
func NewMongoRemediationStore(ctx context.Context, db *mongo.Database) (RemediationStore, error) {
	s := &mongoRemediationStore{col: db.Collection(remediationsCollection)}
	_, err := s.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "store_id", Value: 1}, {Key: "_id", Value: 1}}},
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "store_id", Value: 1}, {Key: "type", Value: 1}}},
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "store_id", Value: 1}, {Key: "sku", Value: 1}, {Key: "applied", Value: 1}}},
	})
	if err != nil {
		return nil, fmt.Errorf("%s indexes: %w", remediationsCollection, err)
	}
	return s, nil
}

func (s *mongoRemediationStore) SyncForSKU(ctx context.Context, merchantID, storeID, sku string, open []Remediation) error {
	keep := make([]string, 0, len(open))
	now := time.Now().UTC()
	for _, r := range open {
		keep = append(keep, r.ID)
		filter := bson.M{"_id": r.ID, "merchant_id": merchantID, "store_id": storeID}
		update := bson.M{
			// An applied record is history: a re-score never reopens it.
			"$set": bson.M{
				"sku": sku, "rule_id": r.RuleID, "remediation_rule_id": r.RemediationRuleID,
				"field": r.Field, "from_value": r.FromValue, "to_value": r.ToValue,
				"type": r.Type, "trust": r.Trust, "updated_at": now,
			},
			"$setOnInsert": bson.M{"applied": false, "created_at": now},
		}
		_, err := s.col.UpdateOne(ctx, bson.M{"$and": []bson.M{filter, {"applied": bson.M{"$ne": true}}}}, update, options.UpdateOne().SetUpsert(true))
		if mongo.IsDuplicateKeyError(err) {
			continue // already applied: left as history
		}
		if err != nil {
			return fmt.Errorf("upsert remediation %s: %w", r.ID, err)
		}
	}
	_, err := s.col.DeleteMany(ctx, bson.M{
		"merchant_id": merchantID, "store_id": storeID, "sku": sku,
		"applied": false, "_id": bson.M{"$nin": keep},
	})
	if err != nil {
		return fmt.Errorf("retire resolved remediations for %s:%s sku %s: %w", merchantID, storeID, sku, err)
	}
	return nil
}

func (s *mongoRemediationStore) GetByIDs(ctx context.Context, merchantID, storeID string, ids []string) ([]Remediation, error) {
	cursor, err := s.col.Find(ctx, bson.M{"_id": bson.M{"$in": ids}, "merchant_id": merchantID, "store_id": storeID})
	if err != nil {
		return nil, fmt.Errorf("get remediations for %s:%s: %w", merchantID, storeID, err)
	}
	var out []Remediation
	if err := cursor.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("decode remediations for %s:%s: %w", merchantID, storeID, err)
	}
	if len(out) != len(uniq(ids)) {
		return nil, ErrRemediationsNotFound
	}
	return out, nil
}

func (s *mongoRemediationStore) MarkApplied(ctx context.Context, merchantID, storeID, id, writeBackID, fromValue string) error {
	_, err := s.col.UpdateOne(ctx,
		bson.M{"_id": id, "merchant_id": merchantID, "store_id": storeID},
		bson.M{"$set": bson.M{"applied": true, "write_back_id": writeBackID, "from_value": fromValue, "updated_at": time.Now().UTC()}},
	)
	if err != nil {
		return fmt.Errorf("mark remediation %s applied for %s:%s: %w", id, merchantID, storeID, err)
	}
	return nil
}

func (s *mongoRemediationStore) MarkReverted(ctx context.Context, merchantID, storeID, id, writeBackID string) error {
	_, err := s.col.UpdateOne(ctx,
		bson.M{"_id": id, "merchant_id": merchantID, "store_id": storeID, "applied": true, "write_back_id": writeBackID},
		bson.M{"$set": bson.M{"applied": false, "updated_at": time.Now().UTC()}, "$unset": bson.M{"write_back_id": ""}},
	)
	if err != nil {
		return fmt.Errorf("mark remediation %s reverted for %s:%s: %w", id, merchantID, storeID, err)
	}
	return nil
}

func uniq(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := ids[:0:0]
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// remediationsFor turns a scored product's issues into its open remediation
// records: one per issue the rubric has a remediation for. An issue a rule can
// fix carries the fix's target value and high trust; one only the merchant can
// fix is recorded as manual, with no proposed value and low trust.
func remediationsFor(p *CatalogProduct, rubric *ScoringRubric) []Remediation {
	var out []Remediation
	for _, issue := range p.Issues {
		rule := rubric.FindScoringRule(issue.IssueID)
		if rule == nil || rule.RemediationID == "" {
			continue
		}
		rem := rubric.FindRemediation(rule.RemediationID)
		if rem == nil {
			continue
		}
		r := Remediation{
			ID:                p.StoreID + ":" + p.SKU + ":" + rule.ID,
			MerchantID:        p.MerchantID,
			StoreID:           p.StoreID,
			SKU:               p.SKU,
			RuleID:            rule.ID,
			RemediationRuleID: rem.ID,
			Field:             issue.Field,
			Type:              "manual",
			Trust:             "low",
		}
		if f, ok := remediationFields[issue.Field]; ok {
			r.FromValue = f.get(p)
		} else {
			r.FromValue = p.MappedFields[issue.Field]
		}
		if to, ok := remediatedValue(p, rem); ok && issue.Remediable == "rule" {
			r.Type, r.Trust, r.ToValue = "rule", "high", to
		}
		out = append(out, r)
	}
	return out
}
