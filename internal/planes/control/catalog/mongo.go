// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// ── ReadinessRepository ───────────────────────────────────────────────────────

// mongoReadinessRepo implements ReadinessRepository against the catalog store.
// Collection name must match what sync plane scoring workers write to (G3).
type mongoReadinessRepo struct {
	col *mongo.Collection
}

// NewMongoReadinessRepository creates a ReadinessRepository backed by MongoDB.
// db should be the shared catalog database (written by sync plane, read here).
func NewMongoReadinessRepository(db *mongo.Database) (ReadinessRepository, error) {
	return &mongoReadinessRepo{col: db.Collection("sync_catalog_readiness")}, nil
}

func (r *mongoReadinessRepo) Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (*ReadinessModel, error) {
	var m ReadinessModel
	docID := fmt.Sprintf("%s:%s", merchantID, storeID)
	err := r.col.FindOne(ctx, bson.M{"_id": docID}).Decode(&m)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get readiness for %s:%s: %w", merchantID, storeID, err)
	}
	return &m, nil
}

// ── ProductRepository ─────────────────────────────────────────────────────────

// mongoProductRepo implements ProductRepository against the catalog store.
type mongoProductRepo struct {
	col *mongo.Collection
}

// NewMongoProductRepository creates a ProductRepository backed by MongoDB.
// db should be the shared catalog database (written by sync plane, read here).
func NewMongoProductRepository(db *mongo.Database) (ProductRepository, error) {
	r := &mongoProductRepo{col: db.Collection("sync_catalog_products")}
	if err := r.createIndexes(context.Background()); err != nil {
		return nil, fmt.Errorf("sync_catalog_products indexes: %w", err)
	}
	return r, nil
}

func (r *mongoProductRepo) createIndexes(ctx context.Context) error {
	_, err := r.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "merchant_id", Value: 1}, {Key: "store_id", Value: 1}, {Key: "sku", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "store_id", Value: 1}, {Key: "score", Value: 1}}},
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "store_id", Value: 1}, {Key: "has_blocking_issue", Value: 1}}},
	})
	return err
}

func (r *mongoProductRepo) Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, sku string) (*ProductModel, error) {
	var m ProductModel
	docID := fmt.Sprintf("%s:%s:%s", merchantID, storeID, sku)
	err := r.col.FindOne(ctx, bson.M{"_id": docID}).Decode(&m)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get product %s:%s/%s: %w", merchantID, storeID, sku, err)
	}
	return &m, nil
}

func (r *mongoProductRepo) List(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, afterSKU string, limit int, minScore *int, hasBlockingIssue *bool) ([]*ProductModel, bool, error) {
	filter := bson.M{
		"merchant_id": string(merchantID),
		"store_id":    string(storeID),
	}
	if afterSKU != "" {
		filter["sku"] = bson.M{"$gt": afterSKU}
	}
	if minScore != nil {
		filter["score"] = bson.M{"$gte": *minScore}
	}
	if hasBlockingIssue != nil {
		filter["has_blocking_issue"] = *hasBlockingIssue
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "sku", Value: 1}}).
		SetLimit(int64(limit + 1))

	cursor, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, false, fmt.Errorf("list products for %s:%s: %w", merchantID, storeID, err)
	}
	defer cursor.Close(ctx)

	var products []*ProductModel
	if err = cursor.All(ctx, &products); err != nil {
		return nil, false, fmt.Errorf("decode products for %s:%s: %w", merchantID, storeID, err)
	}

	hasMore := len(products) > limit
	if hasMore {
		products = products[:limit]
	}
	return products, hasMore, nil
}

// ── RemediationRepository ─────────────────────────────────────────────────────

// mongoRemediationRepo implements RemediationRepository as a read-only view of
// the sync plane's sync_catalog_remediations collection. The sync plane owns
// the collection (and its indexes); nothing here writes to it.
type mongoRemediationRepo struct {
	col *mongo.Collection
}

// remediationsCollection is the sync plane's remediation collection, read here
// by name (a plane never imports another — CLAUDE.md §2.1).
const remediationsCollection = "sync_catalog_remediations"

// NewMongoRemediationRepository returns the read-only remediation view.
func NewMongoRemediationRepository(db *mongo.Database) (RemediationRepository, error) {
	return &mongoRemediationRepo{col: db.Collection(remediationsCollection)}, nil
}

func (r *mongoRemediationRepo) List(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, afterID string, limit int, remType *string) ([]*RemediationModel, bool, error) {
	filter := bson.M{
		"merchant_id": string(merchantID),
		"store_id":    string(storeID),
	}
	if afterID != "" {
		filter["_id"] = bson.M{"$gt": afterID}
	}
	if remType != nil {
		filter["type"] = *remType
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "_id", Value: 1}}).
		SetLimit(int64(limit + 1))

	cursor, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, false, fmt.Errorf("list remediations for %s:%s: %w", merchantID, storeID, err)
	}
	defer cursor.Close(ctx)

	var rems []*RemediationModel
	if err = cursor.All(ctx, &rems); err != nil {
		return nil, false, fmt.Errorf("decode remediations for %s:%s: %w", merchantID, storeID, err)
	}

	hasMore := len(rems) > limit
	if hasMore {
		rems = rems[:limit]
	}
	return rems, hasMore, nil
}

func (r *mongoRemediationRepo) GetByIDs(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, ids []string) ([]*RemediationModel, error) {
	filter := bson.M{
		"_id":         bson.M{"$in": ids},
		"merchant_id": string(merchantID),
		"store_id":    string(storeID),
	}

	opts := options.Find().SetLimit(10000) // Defensive bound: max 10k remediations per store
	cursor, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("get remediations by ids for %s:%s: %w", merchantID, storeID, err)
	}
	defer cursor.Close(ctx)

	var rems []*RemediationModel
	if err = cursor.All(ctx, &rems); err != nil {
		return nil, fmt.Errorf("decode remediations for %s:%s: %w", merchantID, storeID, err)
	}
	if len(rems) != len(ids) {
		return nil, ErrNotFound
	}
	return rems, nil
}

func (r *mongoRemediationRepo) ListUnappliedBySKU(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, sku string) ([]*RemediationModel, error) {
	filter := bson.M{
		"merchant_id": string(merchantID),
		"store_id":    string(storeID),
		"sku":         sku,
		"applied":     false,
	}

	opts := options.Find().SetLimit(10000) // Defensive bound: max 10k remediations per SKU
	cursor, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("list unapplied remediations for %s:%s sku %s: %w", merchantID, storeID, sku, err)
	}
	defer cursor.Close(ctx)

	var rems []*RemediationModel
	if err = cursor.All(ctx, &rems); err != nil {
		return nil, fmt.Errorf("decode remediations for %s:%s sku %s: %w", merchantID, storeID, sku, err)
	}
	return rems, nil
}

// ── WriteBackRepository ───────────────────────────────────────────────────────

// mongoWriteBackRepo implements WriteBackRepository against the audit log collection.
// This collection is OWNED BY THE CONTROL PLANE.
type mongoWriteBackRepo struct {
	col *mongo.Collection
}

// NewMongoWriteBackRepository creates a WriteBackRepository backed by MongoDB.
func NewMongoWriteBackRepository(db *mongo.Database) (WriteBackRepository, error) {
	r := &mongoWriteBackRepo{col: db.Collection("control_catalog_write_backs")}
	if err := r.createIndexes(context.Background()); err != nil {
		return nil, fmt.Errorf("control_catalog_write_backs indexes: %w", err)
	}
	return r, nil
}

func (r *mongoWriteBackRepo) createIndexes(ctx context.Context) error {
	_, err := r.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "store_id", Value: 1}, {Key: "_id", Value: 1}}},
	})
	return err
}

func (r *mongoWriteBackRepo) Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, writeBackID string) (*WriteBackModel, error) {
	var m WriteBackModel
	err := r.col.FindOne(ctx, bson.M{
		"_id":         writeBackID,
		"merchant_id": string(merchantID),
		"store_id":    string(storeID),
	}).Decode(&m)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrWriteBackNotFound
		}
		return nil, fmt.Errorf("get write-back %s for %s:%s: %w", writeBackID, merchantID, storeID, err)
	}
	return &m, nil
}

func (r *mongoWriteBackRepo) Insert(ctx context.Context, m *WriteBackModel) error {
	_, err := r.col.InsertOne(ctx, m)
	if err != nil {
		return fmt.Errorf("insert write-back for %s: %w", m.MerchantID, err)
	}
	return nil
}

func (r *mongoWriteBackRepo) MarkReverted(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, writeBackID string) error {
	_, err := r.col.UpdateOne(ctx,
		bson.M{
			"_id":         writeBackID,
			"merchant_id": string(merchantID),
			"store_id":    string(storeID),
		},
		bson.M{"$set": bson.M{
			"reverted":   true,
			"updated_at": time.Now().UTC(),
		}},
	)
	if err != nil {
		return fmt.Errorf("mark write-back %s reverted for %s:%s: %w", writeBackID, merchantID, storeID, err)
	}
	return nil
}

// ── AuditJobRepository ────────────────────────────────────────────────────────

// mongoAuditJobRepo implements AuditJobRepository against the control audit jobs
// collection. This collection is OWNED BY THE CONTROL PLANE and polled by the
// sync plane (G3).
type mongoAuditJobRepo struct {
	col *mongo.Collection
}

// NewMongoAuditJobRepository creates an AuditJobRepository backed by MongoDB.
func NewMongoAuditJobRepository(db *mongo.Database) (AuditJobRepository, error) {
	r := &mongoAuditJobRepo{col: db.Collection("control_catalog_audits")}
	if err := r.createIndexes(context.Background()); err != nil {
		return nil, fmt.Errorf("control_catalog_audits indexes: %w", err)
	}
	return r, nil
}

func (r *mongoAuditJobRepo) createIndexes(ctx context.Context) error {
	_, err := r.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "created_at", Value: -1}}},
	})
	return err
}

func (r *mongoAuditJobRepo) Insert(ctx context.Context, m *AuditJobModel) error {
	_, err := r.col.InsertOne(ctx, m)
	if err != nil {
		return fmt.Errorf("insert audit job for %s: %w", m.MerchantID, err)
	}
	return nil
}
