// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// UpdateCheckout must send the destination in the ucp.dev fulfillment wire
// shape (methods → destinations + selected_destination_id) — the flat shape
// it used to send is silently dropped by the serving plane — and, when the
// business answers with shipping options, choose one with a second PUT so the
// checkout can complete.
func TestUpdateCheckout_SendsWireFulfillmentAndSelectsOffered(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPut, r.Method)
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		bodies = append(bodies, body)

		w.Header().Set("Content-Type", "application/json")
		selected := "null"
		if len(bodies) > 1 {
			selected = `"standard"`
		}
		_, _ = w.Write([]byte(`{"id":"co-1","fulfillment":{"methods":[{"id":"m1","type":"shipping","line_item_ids":["li-1"],` +
			`"groups":[{"id":"g1","line_item_ids":["li-1"],"selected_option_id":` + selected + `,` +
			`"options":[{"id":"standard","title":"Standard","totals":[]},{"id":"express","title":"Express","totals":[]}]}]}]}}`))
	}))
	defer srv.Close()

	a := NewAgent(srv.URL, srv.URL, nil)
	resp, err := a.UpdateCheckout(context.Background(), "store-1", "co-1",
		&BuyerInput{Email: "buyer@example.com"},
		&AddressInput{Street: "1 Main St", City: "Sydney", Region: "NSW", PostalCode: "2000", CountryCode: "AU"})
	require.NoError(t, err)

	require.Len(t, bodies, 2, "destination PUT, then option-selection PUT")

	first := wireMethod(t, bodies[0])
	assert.Equal(t, "shipping", first["type"])
	assert.Equal(t, "dest-1", first["selected_destination_id"])
	dest := asMap(t, asSlice(t, first["destinations"])[0])
	assert.Equal(t, "dest-1", dest["id"])
	assert.Equal(t, "1 Main St", dest["street_address"])
	assert.Nil(t, first["groups"], "no option is chosen before the business offers any")

	second := wireMethod(t, bodies[1])
	assert.Equal(t, "dest-1", second["selected_destination_id"], "full replacement: destination is resent")
	group := asMap(t, asSlice(t, second["groups"])[0])
	assert.Equal(t, "g1", group["id"])
	assert.Equal(t, "standard", group["selected_option_id"], "first offered option is chosen")
	assert.Equal(t, "buyer@example.com", asMap(t, bodies[1]["buyer"])["email"], "full replacement: buyer is resent")

	require.NotNil(t, resp.Fulfillment)
	assert.Equal(t, "standard", *resp.Fulfillment.Methods[0].Groups[0].SelectedOptionID)
}

// With no address there is nothing to fulfil to, so a single PUT is sent.
func TestUpdateCheckout_BuyerOnlyIsOnePut(t *testing.T) {
	puts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		puts++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"co-1"}`))
	}))
	defer srv.Close()

	_, err := NewAgent(srv.URL, srv.URL, nil).UpdateCheckout(context.Background(), "store-1", "co-1",
		&BuyerInput{Email: "buyer@example.com"}, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, puts)
}

func asMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	require.True(t, ok, "expected a JSON object, got %T", v)
	return m
}

func asSlice(t *testing.T, v any) []any {
	t.Helper()
	s, ok := v.([]any)
	require.True(t, ok, "expected a JSON array, got %T", v)
	require.NotEmpty(t, s)
	return s
}

// wireMethod returns the first fulfillment method of a request body.
func wireMethod(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	return asMap(t, asSlice(t, asMap(t, body["fulfillment"])["methods"])[0])
}
