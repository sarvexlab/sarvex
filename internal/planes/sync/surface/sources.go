// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package surface

import (
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Enrollment is one store's non-available enrollment on a surface.
type Enrollment struct {
	MerchantID string
	StoreID    string
	SurfaceID  string
	Config     map[string]string
}

// EnrollmentSource answers who is on a surface.
type EnrollmentSource interface {
	// Get returns the store's enrollment on the surface, and false when the
	// store has not started on it (state `available` or no record).
	Get(ctx context.Context, merchantID, storeID, surfaceID string) (Enrollment, bool, error)
	// List returns every store that has started on the surface — the safety
	// net's work list.
	List(ctx context.Context, surfaceID string) ([]Enrollment, error)
}

// CatalogSource lists a store's products.
type CatalogSource interface {
	Products(ctx context.Context, merchantID, storeID string) ([]Product, error)
}

// controlSurfacesCollection is the control plane's per-store, per-surface
// enrollment collection, read here by name and never written.
const controlSurfacesCollection = "control_store_surfaces"

// surfaceStateAvailable duplicates control's domain.SurfaceAvailable as data;
// planes never import one another.
const surfaceStateAvailable = "available"

type mongoEnrollments struct{ col *mongo.Collection }

// NewMongoEnrollmentSource reads control_store_surfaces.
func NewMongoEnrollmentSource(db *mongo.Database) EnrollmentSource {
	return &mongoEnrollments{col: db.Collection(controlSurfacesCollection)}
}

type enrollmentDoc struct {
	MerchantID string            `bson:"merchant_id"`
	StoreID    string            `bson:"store_id"`
	SurfaceID  string            `bson:"surface_id"`
	Config     map[string]string `bson:"config"`
}

func (d enrollmentDoc) enrollment() Enrollment {
	return Enrollment(d)
}

func (m *mongoEnrollments) Get(ctx context.Context, merchantID, storeID, surfaceID string) (Enrollment, bool, error) {
	// Query-level tenant fence: both ids are in the filter (ADR-2).
	var d enrollmentDoc
	err := m.col.FindOne(ctx, bson.M{
		"merchant_id": merchantID,
		"store_id":    storeID,
		"surface_id":  surfaceID,
		"state":       bson.M{"$ne": surfaceStateAvailable},
	}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Enrollment{}, false, nil
	}
	if err != nil {
		return Enrollment{}, false, fmt.Errorf("read surface enrollment: %w", err)
	}
	return d.enrollment(), true, nil
}

func (m *mongoEnrollments) List(ctx context.Context, surfaceID string) ([]Enrollment, error) {
	// Cross-tenant by design: the safety net visits every enrolled store, and
	// each result carries its own merchant_id/store_id pair for the per-store
	// work that follows.
	cur, err := m.col.Find(ctx, bson.M{
		"surface_id": surfaceID,
		"state":      bson.M{"$ne": surfaceStateAvailable},
	})
	if err != nil {
		return nil, fmt.Errorf("list surface enrollments: %w", err)
	}
	defer func() { _ = cur.Close(ctx) }()
	var docs []enrollmentDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode surface enrollments: %w", err)
	}
	out := make([]Enrollment, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.enrollment())
	}
	return out, nil
}
