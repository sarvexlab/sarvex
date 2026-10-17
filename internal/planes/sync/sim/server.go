// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sim

import (
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/secretmanagement"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/simstore"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/sync/connector"
	"github.com/sarvexlabs/sarvex.cp/internal/platform"
)

// The simulated store model lives in internal/pkg/simstore, shared with every
// platform's surface; these aliases keep this package's own callers (the CLI,
// the Mongo repository, the plugin simulator) unchanged.
type (
	Store           = simstore.Store
	Product         = simstore.Product
	Order           = simstore.Order
	OrderLineItem   = simstore.OrderLineItem
	OrderAddress    = simstore.OrderAddress
	StoreRepository = simstore.StoreRepository
)

// NewStore returns an empty simulated store; see simstore.NewStore.
func NewStore(storeID string) *Store { return simstore.NewStore(storeID) }

// ErrStoreNotFound is simstore.ErrStoreNotFound.
var ErrStoreNotFound = simstore.ErrStoreNotFound

// Server serves a simulated store for every persisted store_id, multi-tenant
// by the {store_id} path segment — one process serves every store, so
// "multiple simulated stores concurrently" is just multiple documents in
// StoreRepository, not multiple listeners. Each store is reachable through
// every registered platform's REST surface (internal/platform/<name>/sim,
// registered from the platform registry), so a store's platform decides which
// wire shape its connector pulls it through, never a second copy of its data.
//
// It also plays the plugin's Role 2 "profile doorway" (CLAUDE.md §4b) for
// whichever store's storefront_url points here: GET /.well-known/ucp calls
// the real, HMAC-signed GET /connector/profile on the sync plane and serves
// the bytes back, caching the last successful response in memory so a
// transient failure still serves something rather than 5xx/vanishing — the
// same contract the real plugins are required to uphold.
type Server struct {
	stores        StoreRepository
	baseURL       string // this server's own externally-reachable base URL, for Permalink + auth derivation
	connectorRepo connector.ConnectorRepository
	secretMgmt    secretmanagement.SecretManagementService
	signer        *Signer
	log           *slog.Logger
	mux           *http.ServeMux
	// surfaces holds each platform's simulated store routes on a mux of its
	// own, in registration order; the first that claims a path serves it.
	surfaces []*http.ServeMux

	profileMu    sync.Mutex
	profileCache map[string][]byte // last-known-good /.well-known/ucp body, by store_id
}

// NewServer creates a Server. baseURL is this server's own base (e.g.
// SIM_STORE_BASE_URL) — the same value control/sim embeds into
// store_credentials.store_base_url via internal/pkg/simstore, so a request's
// path (/stores/{store_id}/...) is exactly what the real connector will
// request.
//
// connectorRepo, secretMgmt, and signer back the profile doorway only
// (GET /.well-known/ucp) — they recover a store's connector credentials and
// sign the upstream profile fetch exactly as CLI does for mutation signals.
// Pass nils if the caller never needs that route (e.g. tests that only
// exercise the platform REST surfaces); the doorway then simply 404s.
func NewServer(stores StoreRepository, baseURL string, connectorRepo connector.ConnectorRepository, secretMgmt secretmanagement.SecretManagementService, signer *Signer, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	s := &Server{
		stores:        stores,
		baseURL:       strings.TrimRight(baseURL, "/"),
		connectorRepo: connectorRepo,
		secretMgmt:    secretMgmt,
		signer:        signer,
		log:           log,
	}
	s.mux = http.NewServeMux()
	for _, p := range platform.All() {
		s.AddSurface(p.Sim)
	}
	s.mux.HandleFunc("/", s.serveSurface)
	// Unauthenticated, matching the real plugin: /.well-known/ucp is a public
	// doorway any agent can fetch without credentials.
	s.mux.HandleFunc("GET /stores/{store_id}/.well-known/ucp", s.getProfile)
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

// AddSurface registers one more platform's simulated store routes after the
// ones already registered. Tests use it to add a canary platform.
//
// Each surface has a mux of its own, so a path two platforms both claim is
// served by the first rather than panicking the whole simulator; that is what
// a freshly scaffolded platform, still speaking its template's API, needs to
// stay testable. internal/platform's TestSimSurfacesDoNotOverlap is what
// keeps the registered platforms' routes apart.
func (s *Server) AddSurface(surface simstore.Surface) {
	m := http.NewServeMux()
	surface.Register(m, simstore.Env{Stores: s.stores, BaseURL: s.baseURL, Log: s.log})
	s.surfaces = append(s.surfaces, m)
}

// serveSurface hands a request to the first platform surface with a route
// for it.
func (s *Server) serveSurface(w http.ResponseWriter, r *http.Request) {
	for _, m := range s.surfaces {
		// Handler only finds the route; the mux's own ServeHTTP sets the
		// path values ({store_id}, ...) the route's handler reads.
		if _, pattern := m.Handler(r); pattern != "" {
			m.ServeHTTP(w, r)
			return
		}
	}
	http.NotFound(w, r)
}

// getProfile serves the plugin's Role 2 profile doorway: fetch the
// merchant's UCP profile from Sarvex (HMAC-signed, exactly as the real
// WordPress plugin would) and serve those bytes back. A store with no
// completed pairing has nothing to serve yet — the real plugin wouldn't
// either — so that case 404s rather than fabricating content. Any other
// failure (sequence allocation, network, non-200 upstream) falls back to
// the last successful response if one has been cached this process's
// lifetime, matching the "never 30X/5xx/vanish" degradation invariant
// (CLAUDE.md §2 invariant 8) instead of surfacing the transient failure.
func (s *Server) getProfile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	storeID := r.PathValue("store_id")

	if s.connectorRepo == nil || s.secretMgmt == nil || s.signer == nil {
		http.NotFound(w, r)
		return
	}

	connectorID, pairingCode, err := resolveConnector(ctx, s.connectorRepo, s.secretMgmt, storeID)
	if err != nil {
		if cached, ok := s.cachedProfile(storeID); ok {
			s.writeProfile(w, cached)
			return
		}
		http.NotFound(w, r)
		return
	}

	sequence, err := s.signer.NextSequence(ctx, connectorID)
	if err != nil {
		s.log.Warn("sim: profile doorway failed to allocate sequence", "store_id", storeID, "error", err)
		s.serveCachedOrUnavailable(w, storeID)
		return
	}

	resp, err := s.signer.DoRaw(ctx, http.MethodGet, "/connector/profile", connectorID, storeID, pairingCode, sequence, nil)
	if err != nil {
		s.log.Warn("sim: profile doorway fetch failed", "store_id", storeID, "error", err)
		s.serveCachedOrUnavailable(w, storeID)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK {
		s.log.Warn("sim: profile doorway got a bad upstream response", "store_id", storeID, "status", resp.StatusCode, "error", err)
		s.serveCachedOrUnavailable(w, storeID)
		return
	}

	s.cacheProfile(storeID, body)
	s.writeProfile(w, body)
}

func (s *Server) serveCachedOrUnavailable(w http.ResponseWriter, storeID string) {
	if cached, ok := s.cachedProfile(storeID); ok {
		s.writeProfile(w, cached)
		return
	}
	simstore.WriteJSON(w, http.StatusBadGateway, map[string]any{
		"code":    "profile_unavailable",
		"message": "Sarvex's profile endpoint is unreachable and no cached profile is available yet.",
		"data":    map[string]any{"status": http.StatusBadGateway},
	})
}

func (s *Server) writeProfile(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s *Server) cachedProfile(storeID string) ([]byte, bool) {
	s.profileMu.Lock()
	defer s.profileMu.Unlock()
	body, ok := s.profileCache[storeID]
	return body, ok
}

func (s *Server) cacheProfile(storeID string, body []byte) {
	s.profileMu.Lock()
	defer s.profileMu.Unlock()
	if s.profileCache == nil {
		s.profileCache = make(map[string][]byte)
	}
	s.profileCache[storeID] = body
}
