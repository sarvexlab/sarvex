// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// TestCatalog_Readiness verifies GET /stores/:id/catalog/readiness returns the
// readiness document when one exists, and a zero-value document when the store
// has not been scored yet.
//
// Not-yet-scored is deliberately not a 404: the endpoint declares only a 200 in
// sarvex-v1-control-plane.yaml, and CatalogService.GetReadiness translates a
// missing record into a zero-value Readiness. A store with no catalog audit yet
// is a normal pre-scoring state, and the console renders it as a zero score
// rather than an error.
func TestCatalog_Readiness(t *testing.T) {
	storeID := seedStore(t, merchantAID, "Readiness Store", "woocommerce")

	t.Run("zero_value_when_unseeded", func(t *testing.T) {
		resp := apiGet(t, "/v1/stores/"+string(storeID)+"/catalog/readiness", tokenMerchantA)
		var result map[string]any
		decodeJSON(t, resp, http.StatusOK, &result)

		overall, _ := result["overall"].(float64)
		assert.Zero(t, overall, "an unscored store reports a zero readiness score")
	})

	t.Run("returns_readiness_when_seeded", func(t *testing.T) {
		seedReadiness(t, string(merchantAID), string(storeID), 72)

		resp := apiGet(t, "/v1/stores/"+string(storeID)+"/catalog/readiness", tokenMerchantA)
		var result map[string]any
		decodeJSON(t, resp, http.StatusOK, &result)

		overall, _ := result["overall"].(float64)
		assert.Equal(t, float64(72), overall, "overall score must match seeded value")
		assert.NotNil(t, result["dimensions"])
	})

	t.Run("tenant_isolation", func(t *testing.T) {
		// Merchant B asking for merchant A's store gets the same zero-value
		// document it would get for any store it does not own — never the 72
		// the previous subtest seeded. The query is fenced by merchant_id
		// (ADR-2), so the record is simply not visible to this tenant.
		resp := apiGet(t, "/v1/stores/"+string(storeID)+"/catalog/readiness", tokenMerchantB)
		var result map[string]any
		decodeJSON(t, resp, http.StatusOK, &result)

		overall, _ := result["overall"].(float64)
		assert.Zero(t, overall, "another merchant's readiness score must never be visible")
	})
}

// TestCatalog_Products verifies the products list endpoint shape and pagination.
func TestCatalog_Products(t *testing.T) {
	storeID := seedStore(t, merchantAID, "Products Store", "woocommerce")

	t.Run("empty_list_when_no_products", func(t *testing.T) {
		resp := apiGet(t, "/v1/stores/"+string(storeID)+"/catalog/products", tokenMerchantA)
		data, hasMore, _ := assertListEnvelope(t, resp)
		assert.Len(t, data, 0)
		assert.False(t, hasMore)
	})

	t.Run("returns_products_with_pagination", func(t *testing.T) {
		for i := 1; i <= 3; i++ {
			seedProduct(t, string(merchantAID), string(storeID), fmt.Sprintf("SKU-%03d", i), 80)
		}

		resp := apiGet(t, "/v1/stores/"+string(storeID)+"/catalog/products?limit=2", tokenMerchantA)
		data, hasMore, cursor := assertListEnvelope(t, resp)
		assert.Len(t, data, 2)
		assert.True(t, hasMore)
		assert.NotNil(t, cursor)

		// Second page.
		cursorStr, _ := cursor.(string)
		resp2 := apiGet(t, "/v1/stores/"+string(storeID)+"/catalog/products?limit=2&cursor="+cursorStr, tokenMerchantA)
		data2, hasMore2, _ := assertListEnvelope(t, resp2)
		assert.Len(t, data2, 1)
		assert.False(t, hasMore2)
	})
}

// TestCatalog_Product_Get verifies GET /stores/:id/catalog/products/:sku.
func TestCatalog_Product_Get(t *testing.T) {
	storeID := seedStore(t, merchantAID, "Product Get Store", "woocommerce")
	seedProduct(t, string(merchantAID), string(storeID), "SKU-GETME", 65)

	t.Run("found", func(t *testing.T) {
		resp := apiGet(t, "/v1/stores/"+string(storeID)+"/catalog/products/SKU-GETME", tokenMerchantA)
		var result map[string]any
		decodeJSON(t, resp, http.StatusOK, &result)
		assert.Equal(t, "SKU-GETME", result["sku"])
	})

	t.Run("not_found", func(t *testing.T) {
		resp := apiGet(t, "/v1/stores/"+string(storeID)+"/catalog/products/GHOST-SKU", tokenMerchantA)
		assertErrorEnvelope(t, resp, http.StatusNotFound)
	})
}

// TestCatalog_TriggerAudit verifies POST /stores/:id/catalog/audits queues a job.
func TestCatalog_TriggerAudit(t *testing.T) {
	storeID := seedStore(t, merchantAID, "Audit Store", "woocommerce")

	resp := apiPost(t, "/v1/stores/"+string(storeID)+"/catalog/audits", tokenMerchantA, nil)
	var result map[string]any
	decodeJSON(t, resp, http.StatusAccepted, &result)
	assert.NotEmpty(t, result["job_id"])
	assert.Equal(t, "queued", result["status"])
}

// ── seed helpers ──────────────────────────────────────────────────────────────

// seedReadiness writes a ReadinessModel directly to MongoDB.
func seedReadiness(t *testing.T, merchantID, storeID string, overall int) {
	t.Helper()
	doc := bson.M{
		"_id":               merchantID + ":" + storeID,
		"merchant_id":       merchantID,
		"store_id":          storeID,
		"overall":           overall,
		"completeness":      overall,
		"consistency":       overall,
		"attribute_quality": overall,
		"delivery_data":     overall,
		"last_audit_at":     time.Now(),
		"product_count":     0,
		"updated_at":        time.Now(),
	}
	coll := env.db.Collection("sync_catalog_readiness")
	_, err := coll.UpdateOne(
		context.Background(),
		bson.M{"_id": doc["_id"]},
		bson.M{"$set": doc},
		options.UpdateOne().SetUpsert(true),
	)
	require.NoError(t, err)
}

// seedProduct writes a ProductModel directly to MongoDB.
func seedProduct(t *testing.T, merchantID, storeID, sku string, score int) {
	t.Helper()
	doc := bson.M{
		"_id":         merchantID + ":" + storeID + ":" + sku,
		"merchant_id": merchantID,
		"store_id":    storeID,
		"sku":         sku,
		"title":       "Product " + sku,
		"score":       score,
		"issues":      bson.A{},
		"updated_at":  time.Now(),
	}
	coll := env.db.Collection("sync_catalog_products")
	_, err := coll.UpdateOne(
		context.Background(),
		bson.M{"_id": doc["_id"]},
		bson.M{"$set": doc},
		options.UpdateOne().SetUpsert(true),
	)
	require.NoError(t, err)
}
