// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/serving"
	"github.com/sarvexlab/sarvex/internal/pkg/utils"
	"github.com/sarvexlab/sarvex/internal/planes/sync"
	"github.com/sarvexlab/sarvex/internal/planes/sync/surface"
	surfaceall "github.com/sarvexlab/sarvex/internal/planes/sync/surface/all"
)

// catalogProducts adapts the sync catalog store to the surface runner's
// CatalogSource. The store pages by store id alone, so results are filtered to
// the merchant as well: a surface artifact must never list another tenant's SKUs.
type catalogProducts struct{ store sync.CatalogStore }

const catalogPage = 500

func (c catalogProducts) Products(ctx context.Context, merchantID, storeID string) ([]surface.Product, error) {
	var out []surface.Product
	cursor := ""
	for {
		page, next, err := c.store.ListByStore(ctx, storeID, catalogPage, cursor)
		if err != nil {
			return nil, err
		}
		for _, p := range page {
			if p.MerchantID == merchantID {
				out = append(out, surface.Product{SKU: p.SKU})
			}
		}
		if next == nil || *next == "" {
			return out, nil
		}
		cursor = *next
	}
}

// startSurfaceArtifacts wires the sync plane's surface artifact runner over
// every registered generator. It returns a stop function.
func startSurfaceArtifacts(ctx context.Context, config *utils.Configurations, db *mongo.Database, redisClient *redis.Client, bus eventbus.EventBus, log *slog.Logger) (func(), error) {
	catalogStore, err := sync.NewMongoDBCatalogStore(db)
	if err != nil {
		return nil, fmt.Errorf("surface artifacts: catalog store: %w", err)
	}
	artifactStore, err := surface.NewMongoArtifactStore(ctx, db)
	if err != nil {
		return nil, err
	}
	origin := config.ServingOrigin()
	runner := surface.NewRunner(surface.Deps{
		Generators: surfaceall.Generators(),
		Enrolled:   surface.NewMongoEnrollmentSource(db),
		Catalog:    catalogProducts{store: catalogStore},
		Store:      artifactStore,
		Cache:      serving.NewRedisArtifacts(redisClient),
		Bus:        bus,
		PublicURL:  func(token string) string { return origin + "/artifacts/" + token },
		Log:        log,
	})
	if err := runner.Start(ctx); err != nil {
		return nil, err
	}
	return func() { runner.Stop(context.Background()) }, nil
}
