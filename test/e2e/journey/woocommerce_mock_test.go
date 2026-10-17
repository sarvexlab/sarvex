// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package journey_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
)

// wcProduct is a WooCommerce product representation for the E2E mock.
// Some products are intentionally imperfect to exercise readiness scoring and remediation.
type wcProduct struct {
	ID           int              `json:"id"`
	SKU          string           `json:"sku"`
	Name         string           `json:"name"`
	Description  string           `json:"short_description"` // WC field name
	Price        string           `json:"price"`
	RegularPrice string           `json:"regular_price"`
	Status       string           `json:"status"`
	StockStatus  string           `json:"stock_status"`
	Purchasable  bool             `json:"purchasable"`
	Type         string           `json:"type"`
	Images       []map[string]any `json:"images"`
	Attributes   []map[string]any `json:"attributes"`
	Variations   []int            `json:"variations"`
}

// wcVariation is a WooCommerce product variation.
type wcVariation struct {
	ID           int              `json:"id"`
	SKU          string           `json:"sku"`
	Price        string           `json:"price"`
	RegularPrice string           `json:"regular_price"`
	StockStatus  string           `json:"stock_status"`
	Purchasable  bool             `json:"purchasable"`
	Attributes   []map[string]any `json:"attributes"`
}

// e2eProducts is the catalog seeded in the WooCommerce mock.
// Products are intentionally imperfect:
//   - WIDGET-001: missing description (completeness issue) and a name with stray
//     whitespace (attribute quality issue with a rule-based fix: normalize the
//     title, written back as WooCommerce's "name")
//   - GADGET-002: non-standard size attribute (attribute quality issue)
//   - DEVICE-003: good product (should score high)
var e2eProducts = []wcProduct{
	{
		ID: 1001, SKU: "WIDGET-001",
		Name:        "  E2E Test   Widget ", // stray whitespace — title normalization fix
		Description: "",                     // intentionally missing — triggers completeness finding
		Price:       "49.99", RegularPrice: "49.99",
		Status: "publish", StockStatus: "instock", Purchasable: true,
		Type:   "simple",
		Images: []map[string]any{{"src": "https://example.com/widget.jpg"}},
		Attributes: []map[string]any{
			{"name": "Color", "options": []string{"Red", "Blue"}},
		},
	},
	{
		ID: 1002, SKU: "GADGET-002",
		Name:        "E2E Test Gadget",
		Description: "A handy electronic gadget for everyday use.",
		Price:       "129.99", RegularPrice: "129.99",
		Status: "publish", StockStatus: "instock", Purchasable: true,
		Type:   "variable",
		Images: []map[string]any{{"src": "https://example.com/gadget.jpg"}},
		Attributes: []map[string]any{
			{"name": "size", "options": []string{"xs", "medium", "XL"}}, // non-standard casing — attribute quality issue
		},
		Variations: []int{2001, 2002},
	},
	{
		ID: 1003, SKU: "DEVICE-003",
		Name:        "E2E Test Device",
		Description: "Premium electronic device with full specifications.",
		Price:       "299.99", RegularPrice: "299.99",
		Status: "publish", StockStatus: "instock", Purchasable: true,
		Type:   "simple",
		Images: []map[string]any{{"src": "https://example.com/device.jpg"}},
		Attributes: []map[string]any{
			{"name": "Color", "options": []string{"Black"}},
			{"name": "Size", "options": []string{"One Size"}},
		},
	},
}

var e2eVariations = map[int][]wcVariation{
	1002: {
		{ID: 2001, SKU: "GADGET-002-XS", Price: "129.99", RegularPrice: "129.99",
			StockStatus: "instock", Purchasable: true,
			Attributes: []map[string]any{{"name": "size", "option": "xs"}}},
		{ID: 2002, SKU: "GADGET-002-XL", Price: "129.99", RegularPrice: "129.99",
			StockStatus: "instock", Purchasable: true,
			Attributes: []map[string]any{{"name": "size", "option": "XL"}}},
	},
}

// wcMock is an in-process WooCommerce REST API v3 mock server.
// It tracks write-backs (PUT /wp-json/wc/v3/products/:id) for assertion.
type wcMock struct {
	mu         sync.RWMutex
	products   map[int]wcProduct
	writeBacks []wcWriteBack
	server     *httptest.Server
}

// wcWriteBack records a write-back received by the mock.
type wcWriteBack struct {
	ProductID int
	Fields    map[string]any
}

func newWCMock() *wcMock {
	m := &wcMock{
		products: make(map[int]wcProduct),
	}
	for _, p := range e2eProducts {
		m.products[p.ID] = p
	}

	mux := http.NewServeMux()

	// GET /wp-json/wc/v3/products — list all products (connector uses this for initial sync)
	mux.HandleFunc("/wp-json/wc/v3/products", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		m.mu.RLock()
		products := make([]wcProduct, 0, len(m.products))
		sku := r.URL.Query().Get("sku")
		for _, p := range m.products {
			if sku == "" || p.SKU == sku { // WooCommerce filters by ?sku=
				products = append(products, p)
			}
		}
		m.mu.RUnlock()
		writeJSON(w, products)
	})

	// GET /wp-json/wc/v3/products/:id — get single product
	// PUT /wp-json/wc/v3/products/:id — write-back (remediation)
	mux.HandleFunc("/wp-json/wc/v3/products/", func(w http.ResponseWriter, r *http.Request) {
		// Parse path: /wp-json/wc/v3/products/{id} or /wp-json/wc/v3/products/{id}/variations
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/wp-json/wc/v3/products/"), "/")
		idStr := parts[0]
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Error(w, `{"message":"invalid product id"}`, http.StatusBadRequest)
			return
		}

		// /wp-json/wc/v3/products/:id/variations
		if len(parts) >= 2 && parts[1] == "variations" && r.Method == http.MethodGet {
			vars, ok := e2eVariations[id]
			if !ok {
				writeJSON(w, []wcVariation{})
				return
			}
			writeJSON(w, vars)
			return
		}

		switch r.Method {
		case http.MethodGet:
			m.mu.RLock()
			p, ok := m.products[id]
			m.mu.RUnlock()
			if !ok {
				http.Error(w, `{"message":"product not found"}`, http.StatusNotFound)
				return
			}
			writeJSON(w, p)

		case http.MethodPut:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "read error", http.StatusInternalServerError)
				return
			}
			var fields map[string]any
			if err := json.Unmarshal(body, &fields); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			m.mu.Lock()
			p, ok := m.products[id]
			if !ok {
				m.mu.Unlock()
				http.Error(w, `{"message":"product not found"}`, http.StatusNotFound)
				return
			}
			// Apply write-back fields
			if name, ok := fields["name"].(string); ok {
				p.Name = name
			}
			if desc, ok := fields["short_description"].(string); ok {
				p.Description = desc
			}
			if attrs, ok := fields["attributes"].([]any); ok {
				mapped := make([]map[string]any, 0, len(attrs))
				for _, a := range attrs {
					if am, ok := a.(map[string]any); ok {
						mapped = append(mapped, am)
					}
				}
				p.Attributes = mapped
			}
			m.products[id] = p
			m.writeBacks = append(m.writeBacks, wcWriteBack{ProductID: id, Fields: fields})
			m.mu.Unlock()
			writeJSON(w, p)

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// POST /wp-json/wc/v3/webhooks — accept but ignore webhook subscriptions
	mux.HandleFunc("/wp-json/wc/v3/webhooks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, map[string]any{"id": 1, "status": "active"})
	})

	// POST /wp-json/wc/v3/orders — create order (for checkout flow)
	mux.HandleFunc("/wp-json/wc/v3/orders", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"id": 9001, "status": "processing", "total": "49.99"}`)
	})

	m.server = httptest.NewServer(mux)
	return m
}

func (m *wcMock) URL() string { return m.server.URL }

func (m *wcMock) Close() { m.server.Close() }

func (m *wcMock) WriteBacks() []wcWriteBack {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]wcWriteBack, len(m.writeBacks))
	copy(result, m.writeBacks)
	return result
}

func (m *wcMock) Product(id int) (wcProduct, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.products[id]
	return p, ok
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
