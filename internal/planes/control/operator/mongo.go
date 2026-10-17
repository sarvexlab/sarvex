// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package operator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/samber/lo"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// mongoManualStepRepo implements ManualStepRepository.
type mongoManualStepRepo struct {
	col *mongo.Collection
}

// NewMongoManualStepRepository creates a ManualStepRepository backed by MongoDB.
func NewMongoManualStepRepository(db *mongo.Database) (ManualStepRepository, error) {
	r := &mongoManualStepRepo{col: db.Collection("control_manual_steps")}
	if err := r.createIndexes(context.Background()); err != nil {
		return nil, fmt.Errorf("manual_steps indexes: %w", err)
	}
	return r, nil
}

func (r *mongoManualStepRepo) createIndexes(ctx context.Context) error {
	_, err := r.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "target_merchant_id", Value: 1}}},
		{Keys: bson.D{{Key: "created_at", Value: -1}}},
		{Keys: bson.D{{Key: "engagement_stage", Value: 1}, {Key: "created_at", Value: -1}}},
		{Keys: bson.D{{Key: "disposition", Value: 1}, {Key: "created_at", Value: -1}}},
	})
	return err
}

func (r *mongoManualStepRepo) Insert(ctx context.Context, m *ManualStepModel) error {
	_, err := r.col.InsertOne(ctx, m)
	if err != nil {
		return fmt.Errorf("insert manual step: %w", err)
	}
	return nil
}

func (r *mongoManualStepRepo) UpdateDisposition(ctx context.Context, id string, disposition string) (*ManualStepModel, error) {
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var m ManualStepModel
	err := r.col.FindOneAndUpdate(ctx,
		bson.M{"_id": id},
		bson.M{"$set": bson.M{"disposition": disposition, "updated_at": time.Now().UTC()}},
		opts,
	).Decode(&m)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("update disposition %s: %w", id, err)
	}
	return &m, nil
}

func (r *mongoManualStepRepo) ListByMerchant(ctx context.Context, merchantID domain.MerchantID, afterID string, limit int) ([]*ManualStepModel, bool, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	filter := bson.M{"target_merchant_id": string(merchantID)}
	if afterID != "" {
		filter["_id"] = bson.M{"$lt": afterID}
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "_id", Value: -1}}).
		SetLimit(int64(limit + 1))

	cursor, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, false, fmt.Errorf("list manual steps for merchant %s: %w", merchantID, err)
	}
	defer cursor.Close(ctx)

	var steps []*ManualStepModel
	if err = cursor.All(ctx, &steps); err != nil {
		return nil, false, fmt.Errorf("decode manual steps for merchant %s: %w", merchantID, err)
	}

	hasMore := len(steps) > limit
	if hasMore {
		steps = steps[:limit]
	}
	return steps, hasMore, nil
}

func (r *mongoManualStepRepo) ListByDisposition(ctx context.Context, disposition *string, afterID string, limit int) ([]*ManualStepModel, bool, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	filter := bson.M{}
	if disposition != nil {
		filter["disposition"] = *disposition
	}
	if afterID != "" {
		filter["_id"] = bson.M{"$lt": afterID}
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetLimit(int64(limit + 1))

	cursor, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, false, fmt.Errorf("list manual steps by disposition: %w", err)
	}
	defer cursor.Close(ctx)

	var steps []*ManualStepModel
	if err = cursor.All(ctx, &steps); err != nil {
		return nil, false, fmt.Errorf("decode manual steps by disposition: %w", err)
	}

	hasMore := len(steps) > limit
	if hasMore {
		steps = steps[:limit]
	}
	return steps, hasMore, nil
}

func (r *mongoManualStepRepo) ListByStage(ctx context.Context, stage string, disposition *string, afterID string, limit int) ([]*ManualStepModel, bool, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	filter := bson.M{"engagement_stage": stage}
	if disposition != nil {
		filter["disposition"] = *disposition
	}
	if afterID != "" {
		filter["_id"] = bson.M{"$lt": afterID}
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetLimit(int64(limit + 1))

	cursor, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, false, fmt.Errorf("list manual steps by stage %s: %w", stage, err)
	}
	defer cursor.Close(ctx)

	var steps []*ManualStepModel
	if err = cursor.All(ctx, &steps); err != nil {
		return nil, false, fmt.Errorf("decode manual steps by stage %s: %w", stage, err)
	}

	hasMore := len(steps) > limit
	if hasMore {
		steps = steps[:limit]
	}
	return steps, hasMore, nil
}

func (r *mongoManualStepRepo) BulkUpdateDisposition(ctx context.Context, stepIDs []string, disposition string) (int, error) {
	if len(stepIDs) == 0 {
		return 0, nil
	}

	ids := lo.ToAnySlice(stepIDs)

	result, err := r.col.UpdateMany(ctx,
		bson.M{"_id": bson.M{"$in": ids}},
		bson.M{"$set": bson.M{"disposition": disposition, "updated_at": time.Now().UTC()}},
	)
	if err != nil {
		return 0, fmt.Errorf("bulk update disposition: %w", err)
	}

	return int(result.ModifiedCount), nil
}

func (r *mongoManualStepRepo) CountByStageAndDay(ctx context.Context, merchantID *domain.MerchantID, startDate, endDate string) ([]TimelineAggregation, error) {
	pipeline := mongo.Pipeline{}

	matchStage := bson.M{}
	if merchantID != nil {
		matchStage["target_merchant_id"] = string(*merchantID)
	}
	if startDate != "" || endDate != "" {
		dateFilter := bson.M{}
		if startDate != "" {
			dateFilter["$gte"] = startDate
		}
		if endDate != "" {
			dateFilter["$lte"] = endDate
		}
		matchStage["created_at"] = dateFilter
	}
	pipeline = append(pipeline, bson.D{{Key: "$match", Value: matchStage}})

	// Group by date (YYYY-MM-DD) and stage, summing counts and averaging days_in_stage.
	pipeline = append(pipeline, bson.D{
		{Key: "$group", Value: bson.M{
			"_id": bson.M{
				"date":  bson.M{"$dateToString": bson.M{"format": "%Y-%m-%d", "date": "$created_at"}},
				"stage": "$engagement_stage",
			},
			"count":             bson.M{"$sum": 1},
			"avg_days_in_stage": bson.M{"$avg": "$days_in_stage_at_capture"},
		}},
	})

	// Sort by date desc, then stage.
	pipeline = append(pipeline, bson.D{
		{Key: "$sort", Value: bson.M{
			"_id.date":  -1,
			"_id.stage": 1,
		}},
	})

	cursor, err := r.col.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("aggregate timeline metrics: %w", err)
	}
	defer cursor.Close(ctx)

	type result struct {
		ID struct {
			Date  string `bson:"date"`
			Stage string `bson:"stage"`
		} `bson:"_id"`
		Count          int     `bson:"count"`
		AvgDaysInStage float64 `bson:"avg_days_in_stage"`
	}

	var results []result
	if err = cursor.All(ctx, &results); err != nil {
		return nil, fmt.Errorf("decode timeline metrics: %w", err)
	}

	agg := lo.Map(results, func(r result, _ int) TimelineAggregation {
		return TimelineAggregation{
			Date:           r.ID.Date,
			Stage:          r.ID.Stage,
			Count:          r.Count,
			AvgDaysInStage: r.AvgDaysInStage,
		}
	})

	return agg, nil
}

// mongoAuditLogRepo implements AuditLogRepository.
type mongoAuditLogRepo struct {
	col *mongo.Collection
}

// NewMongoAuditLogRepository creates an AuditLogRepository backed by MongoDB.
func NewMongoAuditLogRepository(db *mongo.Database) (AuditLogRepository, error) {
	r := &mongoAuditLogRepo{col: db.Collection("control_audit_log")}
	if err := r.createIndexes(context.Background()); err != nil {
		return nil, fmt.Errorf("audit_log indexes: %w", err)
	}
	return r, nil
}

func (r *mongoAuditLogRepo) createIndexes(ctx context.Context) error {
	_, err := r.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "occurred_at", Value: -1}}},
		{Keys: bson.D{{Key: "target_merchant_id", Value: 1}, {Key: "occurred_at", Value: -1}}},
	})
	return err
}

func (r *mongoAuditLogRepo) Append(ctx context.Context, e *AuditEntryModel) error {
	_, err := r.col.InsertOne(ctx, e)
	if err != nil {
		return fmt.Errorf("append audit entry: %w", err)
	}
	return nil
}

func (r *mongoAuditLogRepo) List(ctx context.Context, targetMerchant *domain.MerchantID, afterID string, limit int) ([]*AuditEntryModel, bool, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	filter := bson.M{}
	if targetMerchant != nil {
		filter["target_merchant_id"] = string(*targetMerchant)
	}
	if afterID != "" {
		filter["_id"] = bson.M{"$lt": afterID}
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "_id", Value: -1}}).
		SetLimit(int64(limit + 1))

	cursor, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, false, fmt.Errorf("list audit entries: %w", err)
	}
	defer cursor.Close(ctx)

	var entries []*AuditEntryModel
	if err = cursor.All(ctx, &entries); err != nil {
		return nil, false, fmt.Errorf("decode audit entries: %w", err)
	}

	hasMore := len(entries) > limit
	if hasMore {
		entries = entries[:limit]
	}
	return entries, hasMore, nil
}

// mongoBoardRepo implements BoardRepository.
type mongoBoardRepo struct {
	journeyCol *mongo.Collection
	storeCol   *mongo.Collection
	accountCol *mongo.Collection
}

// NewMongoBoardRepository creates a BoardRepository that joins journey,
// store, and account data.
func NewMongoBoardRepository(db *mongo.Database) (BoardRepository, error) {
	return &mongoBoardRepo{
		journeyCol: db.Collection("control_journey"),
		storeCol:   db.Collection("control_stores"),
		accountCol: db.Collection("control_accounts"),
	}, nil
}

func (r *mongoBoardRepo) ListCards(ctx context.Context, state *domain.JourneyState, attention *bool, afterStoreID string, limit int) ([]*BoardCardModel, bool, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	// Build filter for journey collection.
	filter := bson.M{}
	if state != nil {
		filter["state"] = string(*state)
	}
	if afterStoreID != "" {
		filter["_id"] = bson.M{"$gt": afterStoreID}
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "_id", Value: 1}}).
		SetLimit(int64(limit + 1))

	cursor, err := r.journeyCol.Find(ctx, filter, opts)
	if err != nil {
		return nil, false, fmt.Errorf("list journey states: %w", err)
	}
	defer cursor.Close(ctx)

	// journey._id is the store_id (one journey document per store) — see
	// journey.JourneyModel's doc comment. It is NOT a merchant_id; the
	// merchant is resolved below via a control_stores join.
	type journeyDoc struct {
		StoreID        string    `bson:"_id"`
		State          string    `bson:"state"`
		EnteredStateAt time.Time `bson:"entered_state_at"`
	}
	var journeyDocs []journeyDoc
	if err = cursor.All(ctx, &journeyDocs); err != nil {
		return nil, false, fmt.Errorf("decode journey states: %w", err)
	}

	hasMore := len(journeyDocs) > limit
	if hasMore {
		journeyDocs = journeyDocs[:limit]
	}

	// Resolve store_id -> merchant_id via control_stores (the only source
	// of truth for that mapping; journey records don't carry merchant_id).
	storeIDs := lo.Map(journeyDocs, func(j journeyDoc, _ int) any { return j.StoreID })

	storeCursor, err := r.storeCol.Find(ctx, bson.M{"_id": bson.M{"$in": storeIDs}})
	if err != nil {
		return nil, false, fmt.Errorf("list stores: %w", err)
	}
	defer storeCursor.Close(ctx)

	type storeDoc struct {
		StoreID    string `bson:"_id"`
		MerchantID string `bson:"merchant_id"`
	}
	var storeDocs []storeDoc
	if err = storeCursor.All(ctx, &storeDocs); err != nil {
		return nil, false, fmt.Errorf("decode stores: %w", err)
	}

	merchantByStore := lo.SliceToMap(storeDocs, func(s storeDoc) (string, string) { return s.StoreID, s.MerchantID })

	// Batch-load account names, keyed by the now-correct merchant_id.
	dedupedMerchantIDs := lo.Uniq(lo.FilterMap(journeyDocs, func(j journeyDoc, _ int) (string, bool) {
		merchantID, ok := merchantByStore[j.StoreID]
		return merchantID, ok
	}))
	merchantIDs := lo.ToAnySlice(dedupedMerchantIDs)

	accountCursor, err := r.accountCol.Find(ctx, bson.M{"_id": bson.M{"$in": merchantIDs}})
	if err != nil {
		return nil, false, fmt.Errorf("list accounts: %w", err)
	}
	defer accountCursor.Close(ctx)

	type accountDoc struct {
		ID   string `bson:"_id"`
		Name string `bson:"name"`
	}
	var accountDocs []accountDoc
	if err = accountCursor.All(ctx, &accountDocs); err != nil {
		return nil, false, fmt.Errorf("decode accounts: %w", err)
	}

	// Build map of merchant_id -> account name for fast lookup.
	nameMap := lo.SliceToMap(accountDocs, func(a accountDoc) (string, string) { return a.ID, a.Name })

	// Build board cards. A journey record whose store no longer exists (or
	// was never linked to a merchant) is skipped rather than surfaced with
	// an empty merchant_id — an orphaned journey record, not a real card.
	cards := lo.FilterMap(journeyDocs, func(j journeyDoc, _ int) (*BoardCardModel, bool) {
		merchantID, ok := merchantByStore[j.StoreID]
		if !ok {
			return nil, false
		}
		return &BoardCardModel{
			MerchantID:     merchantID,
			StoreID:        j.StoreID,
			Name:           nameMap[merchantID],
			State:          j.State,
			EnteredStateAt: j.EnteredStateAt,
		}, true
	})

	// Apply attention filter if needed (stale-or-blocked).
	if attention != nil && *attention {
		now := time.Now().UTC()
		cards = lo.Filter(cards, func(card *BoardCardModel, _ int) bool {
			daysInState := int(now.Sub(card.EnteredStateAt).Hours() / 24)
			return daysInState > 7 // blocked threshold
		})
	}

	return cards, hasMore, nil
}
