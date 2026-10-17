// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package simstore

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

// Surface is one backend platform's simulated store: its REST API over the
// platform-neutral Store model, the credentials a simulated store of that
// platform is paired with, and the hooks tests need to drive the platform's
// real connector against it. Each platform module provides one
// (internal/platform/<name>/sim) and the platform registry carries it, so the
// simulator serves every registered platform without naming any.
type Surface interface {
	// Register adds the platform's routes, all under /stores/{store_id}/, to mux.
	Register(mux *http.ServeMux, env Env)

	// Credentials is the store_credentials payload (sarvex-v1-control-plane.yaml)
	// a simulated store of this platform is paired with, derived from storeID
	// alone so the pairing side and the serving side never coordinate.
	Credentials(simServerBaseURL, storeID string) map[string]any

	// Connector builds the platform's real connector pointed at a simulated
	// store served from simServerBaseURL.
	Connector(simServerBaseURL, merchantID, storeID string) (base.Connector, error)

	// CatalogID is the id the platform's catalog lookup takes for p.
	CatalogID(p *Product) string

	// PickupLocationID is the id of the pickup destination a simulated store
	// of this platform offers once pickup is enabled.
	PickupLocationID() string
}

// Env is what a Surface's handlers need from the server hosting them.
type Env struct {
	Stores StoreRepository
	// BaseURL is the simulator server's own externally reachable base URL.
	BaseURL string
	Log     *slog.Logger
}

// StoreAddress is a simulated store's physical address.
type StoreAddress struct {
	Street, Street2, City, Country, Region, Postcode string
}

// Address is the address every simulated store reports — where local pickup
// sends buyers.
var Address = StoreAddress{
	Street:   "1 Sim Street",
	City:     "Springfield",
	Country:  "US",
	Region:   "CA",
	Postcode: "94000",
}

// HasShippingAddress reports whether a shipping address has been set on
// the order — enough of one to quote a rate against. A country or street line
// is the minimal signal; an all-zero address (none supplied yet) yields none.
func HasShippingAddress(a OrderAddress) bool {
	return a.Country != "" || a.Address1 != "" || a.Postcode != ""
}

// QueryInt reads an integer query parameter, or def when absent or malformed.
func QueryInt(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// WriteJSON writes v as a JSON response with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
