// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sim_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	syncsim "github.com/sarvexlab/sarvex/internal/planes/sync/sim"
	"github.com/sarvexlab/sarvex/internal/planes/sync/surface"
	surfaceall "github.com/sarvexlab/sarvex/internal/planes/sync/surface/all"
)

// TestCapabilityMatrix_SurfaceArtifacts holds every registered surface artifact
// generator to the platform axis of the matrix.
//
// A surface artifact is built from the catalog the sync plane pulls from the
// store, so it has to be right for whatever that platform's connector returns:
// WooCommerce and Magento key products differently (numeric id vs SKU), and
// the feed must still list each product exactly once, under the SKU the
// catalog carries, and only for products an agent can actually look up. The
// generators are taken from the sync registration root, so adding a surface
// extends this automatically — as adding a platform does.
//
// The spec-generation axis does not apply: an artifact is not a UCP response
// and takes no spec version (the generators' Input has none), so there is
// nothing for a generation to render differently. If one ever needs a version,
// it must come with a Servable() loop here.
func TestCapabilityMatrix_SurfaceArtifacts(t *testing.T) {
	gens := surfaceall.Generators()
	if len(gens) == 0 {
		t.Skip("no surface artifact generators registered; this matrix extends itself when one is")
	}
	for _, p := range matrixPlatforms() {
		for _, g := range gens {
			t.Run(p.name+"/"+g.SurfaceID()+"/"+g.ArtifactID(), func(t *testing.T) {
				storeID := "store-artifact-" + p.name
				repo := newMemRepo()
				store := syncsim.NewStore(storeID)
				store.Seed(4, false)
				if err := repo.Upsert(context.Background(), store); err != nil {
					t.Fatalf("seed: %v", err)
				}
				ts := httptest.NewServer(syncsim.NewServer(repo, "http://sim-server.internal", nil, nil, nil, nil).Handler())
				defer ts.Close()
				conn := p.conn(t, ts.URL, storeID)
				ctx := context.Background()

				// What the sync plane pulls from this platform.
				entities, err := conn.FetchCatalog(ctx, storeID, 50)
				if err != nil {
					t.Fatalf("fetch catalog: %v", err)
				}
				if len(entities) != 4 {
					t.Fatalf("%s: pulled %d products, want 4", p.name, len(entities))
				}
				in := surface.Input{MerchantID: "merchant-1", StoreID: storeID, URL: "https://x/artifacts/t"}
				for _, e := range entities {
					in.Products = append(in.Products, surface.Product{SKU: e.SKU})
				}

				out, err := g.Generate(ctx, in)
				if err != nil {
					t.Fatalf("generate: %v", err)
				}
				body := string(out.Body)
				if out.ContentType == "" || len(out.Body) == 0 {
					t.Fatalf("%s: empty artifact", p.name)
				}

				// Every pulled product appears exactly once, and each is one
				// an agent can look up on this platform.
				for _, e := range entities {
					if n := strings.Count(body, ">"+e.SKU+"<"); n < 1 {
						t.Errorf("%s: sku %q missing from the artifact", p.name, e.SKU)
					}
					prod := store.Products[e.SKU]
					if prod == nil {
						t.Fatalf("%s: pulled sku %q is not in the simulated store", p.name, e.SKU)
					}
					if _, err := conn.CatalogLookup(ctx, storeID, p.catalogID(prod)); err != nil {
						t.Errorf("%s: artifact lists sku %q but an agent cannot look it up: %v", p.name, e.SKU, err)
					}
				}

				// Output is deterministic for the same catalog.
				again, err := g.Generate(ctx, in)
				if err != nil || string(again.Body) != body {
					t.Errorf("%s: artifact is not deterministic (err=%v)", p.name, err)
				}
			})
		}
	}
}
