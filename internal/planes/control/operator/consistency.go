// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package operator

import (
	"context"
	"fmt"
	"time"

	"github.com/samber/lo"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// ConsistencyRepository is the read/write interface to ConsistencyFinding
// storage, scoped per store. monitoring.DiscrepancySubscriber (which
// structurally satisfies its own narrower FindingRepository via this type's
// Save method) is its one writer; OperatorService.GetHealth and
// ListConsistencyFindings are readers.
type ConsistencyRepository interface {
	// Save upserts a finding by its FindingID, so re-detecting the same
	// divergence refreshes detected_at/severity/sources rather than
	// duplicating the record.
	Save(ctx context.Context, storeID string, finding *domain.ConsistencyFinding) error

	// ListByStore returns findings for storeID, most-recently-detected
	// first, optionally filtered by severity. before is an RFC3339Nano
	// detected_at cursor (exclusive); empty starts from the newest finding.
	ListByStore(ctx context.Context, storeID string, before string, limit int, severity *domain.Severity) ([]*domain.ConsistencyFinding, bool, error)

	// CountByStore returns the number of open findings at the given
	// severity for storeID — used to roll findings up into GetHealth
	// without paging through the full list.
	CountByStore(ctx context.Context, storeID string, severity domain.Severity) (int, error)

	// DeleteRecoveredProbeFindings removes storeID's findings whose SKU is
	// in probeSKUs and whose id is not among stillFailingIDs — the findings
	// a just-completed probe pass no longer reports.
	//
	// Without it findings are write-only, and a single transient failure
	// (a plugin restart, a deploy) leaves a critical finding on the Health
	// page permanently, contradicting the probe statement directly above it
	// that reports the same endpoint healthy.
	//
	// Only ever called with the complete result of a full probe battery —
	// see monitoring.TaskServiceRouter.ReconcileRecoveredProbeFindings,
	// whose by-exclusion reasoning this shares.
	DeleteRecoveredProbeFindings(ctx context.Context, storeID string, probeSKUs []string, stillFailingIDs []string) error
}

// consistencyFindingModel is the BSON-mapped storage document. store_id is
// the query-scoping field the wire-level domain.ConsistencyFinding
// deliberately omits — per sarvex-v1-control-plane.yaml's consistency_finding
// schema, the store is conveyed by the URL path, not the response body.
type consistencyFindingModel struct {
	ID         string            `bson:"_id"` // finding_id
	StoreID    string            `bson:"store_id"`
	SKU        string            `bson:"sku"`
	Field      string            `bson:"field"`
	Sources    map[string]string `bson:"sources"`
	Severity   string            `bson:"severity"`
	DetectedAt time.Time         `bson:"detected_at"`
}

// mongoConsistencyRepo implements ConsistencyRepository.
type mongoConsistencyRepo struct {
	col *mongo.Collection
}

// NewMongoConsistencyRepository creates a ConsistencyRepository backed by MongoDB.
func NewMongoConsistencyRepository(db *mongo.Database) (ConsistencyRepository, error) {
	r := &mongoConsistencyRepo{col: db.Collection("control_consistency_findings")}
	if err := r.createIndexes(context.Background()); err != nil {
		return nil, fmt.Errorf("consistency_findings indexes: %w", err)
	}
	return r, nil
}

func (r *mongoConsistencyRepo) createIndexes(ctx context.Context) error {
	_, err := r.col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "store_id", Value: 1}, {Key: "detected_at", Value: -1}},
	})
	return err
}

func (r *mongoConsistencyRepo) Save(ctx context.Context, storeID string, finding *domain.ConsistencyFinding) error {
	doc := consistencyFindingModel{
		ID:         finding.FindingID,
		StoreID:    storeID,
		SKU:        finding.SKU,
		Field:      finding.Field,
		Sources:    finding.Sources,
		Severity:   string(finding.Severity),
		DetectedAt: finding.DetectedAt,
	}
	_, err := r.col.ReplaceOne(ctx, bson.M{"_id": doc.ID}, doc, options.Replace().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("save consistency finding %s: %w", doc.ID, err)
	}
	return nil
}

func (r *mongoConsistencyRepo) DeleteRecoveredProbeFindings(ctx context.Context, storeID string, probeSKUs []string, stillFailingIDs []string) error {
	if len(probeSKUs) == 0 {
		return nil
	}
	filter := bson.M{
		"store_id": storeID,
		"sku":      bson.M{"$in": probeSKUs},
	}
	// Empty means the pass reported nothing failing, so every probe finding
	// for this store is recovered — $nin on an empty slice correctly matches
	// all of them, but being explicit costs nothing and reads better.
	if len(stillFailingIDs) > 0 {
		filter["_id"] = bson.M{"$nin": stillFailingIDs}
	}

	if _, err := r.col.DeleteMany(ctx, filter); err != nil {
		return fmt.Errorf("delete recovered probe findings for store %s: %w", storeID, err)
	}
	return nil
}

func (r *mongoConsistencyRepo) ListByStore(ctx context.Context, storeID string, before string, limit int, severity *domain.Severity) ([]*domain.ConsistencyFinding, bool, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	filter := bson.M{"store_id": storeID}
	if severity != nil {
		filter["severity"] = string(*severity)
	}
	if before != "" {
		if t, err := time.Parse(time.RFC3339Nano, before); err == nil {
			filter["detected_at"] = bson.M{"$lt": t}
		}
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "detected_at", Value: -1}}).
		SetLimit(int64(limit + 1))

	cursor, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, false, fmt.Errorf("list consistency findings for store %s: %w", storeID, err)
	}
	defer cursor.Close(ctx)

	var docs []consistencyFindingModel
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, false, fmt.Errorf("decode consistency findings for store %s: %w", storeID, err)
	}

	hasMore := len(docs) > limit
	if hasMore {
		docs = docs[:limit]
	}

	findings := lo.Map(docs, func(d consistencyFindingModel, _ int) *domain.ConsistencyFinding {
		return &domain.ConsistencyFinding{
			FindingID:  d.ID,
			SKU:        d.SKU,
			Field:      d.Field,
			Sources:    d.Sources,
			Severity:   domain.Severity(d.Severity),
			DetectedAt: d.DetectedAt,
		}
	})
	return findings, hasMore, nil
}

func (r *mongoConsistencyRepo) CountByStore(ctx context.Context, storeID string, severity domain.Severity) (int, error) {
	count, err := r.col.CountDocuments(ctx, bson.M{"store_id": storeID, "severity": string(severity)})
	if err != nil {
		return 0, fmt.Errorf("count consistency findings for store %s: %w", storeID, err)
	}
	return int(count), nil
}
