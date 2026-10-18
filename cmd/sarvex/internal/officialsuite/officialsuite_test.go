// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package officialsuite

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ucpagent "github.com/sarvexlab/sarvex/internal/pkg/ucp/agent"
)

// fakeFault records SetStock calls instead of mutating a real store, so a
// test can assert Build forced the second item out of stock without a sim
// backend.
type fakeFault struct {
	stockCalls []string
}

func (f *fakeFault) SetStock(_ context.Context, _, sku string, quantity int) error {
	if quantity != 0 {
		panic("test only expects Build to force stock to zero")
	}
	f.stockCalls = append(f.stockCalls, sku)
	return nil
}

func (f *fakeFault) BreakAttribute(context.Context, string, string, string) error { return nil }
func (f *fakeFault) Fix(context.Context, string, string, string) error            { return nil }

func newTestAgent(t *testing.T, products []map[string]any) *ucpagent.Agent {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"products":   products,
			"pagination": map[string]any{"cursor": "", "has_next_page": false},
		})
	}))
	t.Cleanup(srv.Close)
	return ucpagent.NewAgent(srv.URL, "", nil)
}

func twoProducts() []map[string]any {
	return []map[string]any{
		{
			"id": "sku-1", "title": "Widget",
			"price_range": map[string]any{"min": map[string]any{"amount": 1000, "currency": "USD"}},
		},
		{
			"id": "sku-2", "title": "Gadget",
			"price_range": map[string]any{"min": map[string]any{"amount": 2500, "currency": "USD"}},
		},
	}
}

func TestBuild(t *testing.T) {
	agent := newTestAgent(t, twoProducts())
	fault := &fakeFault{}

	input, fixtures, err := Build(context.Background(), agent, fault, "store-1", "2026-04-08")
	require.NoError(t, err)

	assert.Equal(t, "2026-04-08", input.UCPVersion)
	assert.Equal(t, RequiredCapabilities, input.RequiredCapabilities)
	assert.Equal(t, "USD", input.Currency)
	assert.Len(t, input.Items, 2)
	assert.Equal(t, "sku-1", input.Items[0].ID)
	assert.EqualValues(t, 1000, input.Items[0].Price)

	// The second item is the one forced out of stock, and it must not be
	// the well-known non-existent id.
	assert.Equal(t, "sku-2", input.OutOfStockItem.ID)
	assert.Equal(t, []string{"sku-2"}, fault.stockCalls)
	assert.NotEqual(t, input.Items[0].ID, input.NonExistentItem.ID)
	assert.NotEqual(t, input.Items[1].ID, input.NonExistentItem.ID)

	assert.Equal(t, "sku-1", fixtures.TestFixtures.ValidItem.SKU)
	assert.InDelta(t, 10.0, fixtures.TestFixtures.ValidItem.ExpectedPrice, 0.001)
	assert.Equal(t, 1, fixtures.TestFixtures.ValidItem.Quantity)
}

func TestBuild_NoFaultInjector(t *testing.T) {
	agent := newTestAgent(t, twoProducts())

	input, _, err := Build(context.Background(), agent, nil, "store-1", "2026-08-25")
	require.NoError(t, err)
	assert.Equal(t, "sku-2", input.OutOfStockItem.ID)
}

func TestBuild_TooFewItems(t *testing.T) {
	agent := newTestAgent(t, twoProducts()[:1])

	_, _, err := Build(context.Background(), agent, nil, "store-1", "2026-04-08")
	require.Error(t, err)
}

func TestWriteInputAndFixtures(t *testing.T) {
	dir := t.TempDir()
	input := &ConformanceInput{
		UCPVersion:           "2026-04-08",
		RequiredCapabilities: RequiredCapabilities,
		Currency:             "USD",
		Items:                []Item{{ID: "sku-1", Title: "Widget", Price: 1000}},
		OutOfStockItem:       ItemRef{ID: "sku-2", Title: "Gadget"},
		NonExistentItem:      ItemRef{ID: nonExistentItemID, Title: "Non-existent Item"},
	}
	fixtures := &TestFixtures{TestFixtures: FixtureValues{
		ValidItem: ValidItem{SKU: "sku-1", ExpectedPrice: 10.0, Quantity: 1},
	}}

	inputPath, err := WriteInput(dir, input)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "conformance_input-2026-04-08.json"), inputPath)

	fixturesPath, err := WriteFixtures(dir, fixtures)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "test_fixtures.json"), fixturesPath)

	// Round-trip through a generic map to pin the exact wire keys the
	// official suite's README documents — a Go field rename would silently
	// break the suite's parsing without this.
	raw, err := os.ReadFile(inputPath) //nolint:gosec // G304: inputPath is this test's own WriteInput return value, not external input
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	for _, key := range []string{"ucp_version", "required_capabilities", "currency", "items", "out_of_stock_item", "non_existent_item"} {
		assert.Contains(t, decoded, key)
	}

	raw, err = os.ReadFile(fixturesPath) //nolint:gosec // G304: fixturesPath is this test's own WriteFixtures return value, not external input
	require.NoError(t, err)
	var decodedFixtures map[string]any
	require.NoError(t, json.Unmarshal(raw, &decodedFixtures))
	testFixtures, ok := decodedFixtures["test_fixtures"].(map[string]any)
	require.True(t, ok)
	validItem, ok := testFixtures["valid_item"].(map[string]any)
	require.True(t, ok)
	for _, key := range []string{"sku", "expected_price", "quantity"} {
		assert.Contains(t, validItem, key)
	}
}
