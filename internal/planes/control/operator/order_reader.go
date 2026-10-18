// Copyright 2026 Sarvex Lab
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

// ErrOrderNotFound means no order exists for the given lookup.
var ErrOrderNotFound = errors.New("operator: order not found")

// OrderReader is the read-only interface to Order records the sync plane's
// OrderIntentHandler writes (internal/planes/sync/checkout_order_intent.go,
// internal/planes/sync/order.go) — one writer, read cross-plane (read-only,
// by collection name).
type OrderReader interface {
	List(ctx context.Context, storeID domain.StoreID, cursor string, limit int, status *domain.OrderStatus) (domain.ListOf[domain.Order], error)
	Get(ctx context.Context, storeID domain.StoreID, orderID string) (domain.Order, error)
}

// mongoOrderDoc mirrors the sync plane's Order bson shape
// (internal/planes/sync/order.go) — redeclared here since control and sync
// share only the collection schema, not Go types, across the plane
// boundary.
type mongoOrderDoc struct {
	OrderID           string    `bson:"_id"`
	StoreID           string    `bson:"store_id"`
	MerchantID        string    `bson:"merchant_id"`
	CheckoutSessionID string    `bson:"checkout_session_id"`
	StoreOrderRef     string    `bson:"store_order_ref"`
	Status            string    `bson:"status"`
	SyncState         string    `bson:"sync_state"`
	ValueMinor        int64     `bson:"value_minor"`
	Currency          string    `bson:"currency"`
	PlacedAt          time.Time `bson:"placed_at"`
}

func (d *mongoOrderDoc) toDomain() domain.Order {
	ref := d.StoreOrderRef
	return domain.Order{
		OrderID:           d.OrderID,
		CheckoutSessionID: d.CheckoutSessionID,
		StoreOrderRef:     &ref,
		Status:            domain.OrderStatus(d.Status),
		SyncState:         domain.OrderSyncState(d.SyncState),
		Value:             domain.Money{AmountMinor: d.ValueMinor, Currency: d.Currency},
		PlacedAt:          d.PlacedAt,
	}
}

// mongoOrderReader implements OrderReader against the sync-plane-owned
// sync_orders collection.
type mongoOrderReader struct {
	coll *mongo.Collection
}

// NewMongoOrderReader opens the sync_orders collection read-only.
func NewMongoOrderReader(db *mongo.Database) OrderReader {
	return &mongoOrderReader{coll: db.Collection("sync_orders")}
}

// List returns a store's orders, most recent first (by _id, matching this
// package's AuditLogRepository cursor convention), optionally filtered by
// status.
func (r *mongoOrderReader) List(ctx context.Context, storeID domain.StoreID, cursor string, limit int, status *domain.OrderStatus) (domain.ListOf[domain.Order], error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	filter := bson.M{"store_id": string(storeID)}
	if status != nil {
		filter["status"] = string(*status)
	}
	if cursor != "" {
		filter["_id"] = bson.M{"$lt": cursor}
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "_id", Value: -1}}).
		SetLimit(int64(limit + 1))

	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return domain.ListOf[domain.Order]{}, fmt.Errorf("list orders: %w", err)
	}
	defer cur.Close(ctx)

	var docs []mongoOrderDoc
	if err := cur.All(ctx, &docs); err != nil {
		return domain.ListOf[domain.Order]{}, fmt.Errorf("decode orders: %w", err)
	}

	hasMore := len(docs) > limit
	if hasMore {
		docs = docs[:limit]
	}

	items := lo.Map(docs, func(d mongoOrderDoc, _ int) domain.Order { return d.toDomain() })

	var nextCursor *string
	if hasMore && len(docs) > 0 {
		last := docs[len(docs)-1].OrderID
		nextCursor = &last
	}

	return domain.ListOf[domain.Order]{
		Data: items,
		Page: domain.Page{Cursor: nextCursor, HasMore: hasMore},
	}, nil
}

// Get returns a single order, scoped to storeID so a store can never read
// another store's order by guessing an order_id.
func (r *mongoOrderReader) Get(ctx context.Context, storeID domain.StoreID, orderID string) (domain.Order, error) {
	var doc mongoOrderDoc
	err := r.coll.FindOne(ctx, bson.M{"_id": orderID, "store_id": string(storeID)}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return domain.Order{}, ErrOrderNotFound
	}
	if err != nil {
		return domain.Order{}, fmt.Errorf("get order: %w", err)
	}
	return doc.toDomain(), nil
}
