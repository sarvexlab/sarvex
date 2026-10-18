// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package sim_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sarvexlab/sarvex/internal/pkg/simstore"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlab/sarvex/internal/planes/sync/sim"
	specall "github.com/sarvexlab/sarvex/internal/spec/all"
	"github.com/sarvexlab/sarvex/internal/spec/spectest"
)

// canarySurface is a test-only platform's simulated store: one route of its
// own, beside the registered platforms'.
type canarySurface struct{}

func (canarySurface) Register(mux *http.ServeMux, _ simstore.Env) {
	mux.HandleFunc("GET /stores/{store_id}/canary/ping", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
}

func (canarySurface) Credentials(_, _ string) map[string]any { return map[string]any{} }

func (canarySurface) Connector(_, _, _ string) (base.Connector, error) {
	return nil, errors.New("canary platform has no connector")
}

func (canarySurface) CatalogID(p *simstore.Product) string { return p.SKU }

func (canarySurface) PickupLocationID() string { return "" }

// TestCapabilityMatrix_CanariesChangeNothing is the executable form of "adding
// a version or a platform = new code plus one registration line, and existing
// ones do not regress" (docs/plans/version-platform-modularity.md §4.5). It
// runs every existing platform × generation of the matrix with a canary
// generation registered beside the real ones and a canary platform's store
// served beside theirs, and checks every existing golden byte-for-byte.
func TestCapabilityMatrix_CanariesChangeNothing(t *testing.T) {
	env := matrixEnv{codecs: spectest.WithCanary().Codecs(), extraSurfaces: []simstore.Surface{canarySurface{}}}
	for _, p := range matrixPlatforms() {
		for _, v := range specall.Default().Servable() {
			t.Run(p.name+"/"+string(v), func(t *testing.T) {
				runMatrixScenario(t, env, p, v)
			})
		}
	}
}

// shadowSurface claims the canary's route with a different answer.
type shadowSurface struct{ canarySurface }

func (shadowSurface) Register(mux *http.ServeMux, _ simstore.Env) {
	mux.HandleFunc("GET /stores/{store_id}/canary/ping", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
}

// Each platform's surface routes on its own: a platform's route is served with
// its path values set, a path no platform claims is a 404, and a path two
// claim is served by the first instead of panicking the simulator.
func TestServer_RoutesToTheFirstSurfaceClaimingAPath(t *testing.T) {
	srv := sim.NewServer(simstore.NewMemoryRepository(), "http://sim.test", nil, nil, nil, nil)
	srv.AddSurface(canarySurface{})
	srv.AddSurface(shadowSurface{})

	for path, want := range map[string]int{
		"/stores/s1/canary/ping": http.StatusNoContent,
		"/stores/s1/nowhere":     http.StatusNotFound,
	} {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != want {
			t.Errorf("GET %s = %d, want %d", path, rec.Code, want)
		}
	}
}
