// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package onboard

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sarvexlab/sarvex/cmd/sarvex/internal/client"
)

// fakeControlPlane is a stand-in for the control plane that records the
// calls made against it and answers them the way the real one does.
//
// The point is not to test the control plane — the integration suites do
// that — but to pin the *sequence and shape* of what onboarding sends. Get
// the order wrong (pair before seeding, say) and the real thing fails in a
// way that is slow and confusing to diagnose.
type fakeControlPlane struct {
	mu    sync.Mutex
	calls []string

	// verifyAfter makes verification succeed only from the nth attempt, so
	// the polling path is exercised rather than assumed.
	verifyAfter int
	verifyCount int

	// ucpState and journeyState are what the lifecycle endpoints report
	// once verification has passed.
	ucpState     string
	journeyState string
}

func (f *fakeControlPlane) record(method, path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method+" "+path)
}

func (f *fakeControlPlane) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeControlPlane) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	f.record(r.Method, path)

	if r.Header.Get("Authorization") == "" {
		http.Error(w, `{"error":{"code":"unauthorized"}}`, http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodPost && path == "/operator/merchants":
		writeJSON(w, http.StatusCreated, client.Account{MerchantID: "merch-1", Name: "Acme Widgets"})

	case r.Method == http.MethodPost && path == "/stores":
		writeJSON(w, http.StatusCreated, client.Store{StoreID: "store-1", MerchantID: "merch-1", DisplayName: "Acme Widgets", Platform: "woocommerce"})

	case r.Method == http.MethodPut && strings.HasSuffix(path, "/profile"),
		r.Method == http.MethodPut && strings.HasSuffix(path, "/payment-config"),
		r.Method == http.MethodPost && strings.HasSuffix(path, "/catalog/audits"):
		w.WriteHeader(http.StatusNoContent)

	case r.Method == http.MethodPost && strings.HasSuffix(path, "/connectors/pairing"):
		writeJSON(w, http.StatusCreated, client.Pairing{ConnectorID: "conn-1", PairingCode: "code", StoreID: "store-1"})

	case r.Method == http.MethodPost && strings.HasSuffix(path, "/activation/verify"):
		f.mu.Lock()
		f.verifyCount++
		passed := f.verifyCount >= f.verifyAfter
		f.mu.Unlock()
		if passed {
			f.mu.Lock()
			f.ucpState, f.journeyState = StateActive, JourneyLive
			f.mu.Unlock()
			writeJSON(w, http.StatusOK, client.VerificationReport{Passed: true, State: StateActive})
			return
		}
		writeJSON(w, http.StatusOK, client.VerificationReport{
			Passed: false, State: "profile_published",
			Checks: []client.CheckResult{{CheckID: "profile_reachable", Passed: false, Detail: "not yet"}},
		})

	case r.Method == http.MethodGet && strings.HasSuffix(path, "/activation"):
		f.mu.Lock()
		state := f.ucpState
		f.mu.Unlock()
		if state == "" {
			state = "connector_linked"
		}
		writeJSON(w, http.StatusOK, client.Activation{StoreID: "store-1", UCPState: state})

	case r.Method == http.MethodGet && strings.HasSuffix(path, "/journey"):
		f.mu.Lock()
		state := f.journeyState
		f.mu.Unlock()
		if state == "" {
			state = "connected"
		}
		writeJSON(w, http.StatusOK, client.Journey{State: state})

	case r.Method == http.MethodGet && path == "/tasks":
		writeJSON(w, http.StatusOK, map[string]any{"data": []client.Task{}, "page": map[string]any{}})

	default:
		http.Error(w, `{"error":{"code":"not_found"}}`, http.StatusNotFound)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// recordingSeeder notes when seeding happened relative to the HTTP calls.
type recordingSeeder struct {
	fake     *fakeControlPlane
	seededAt int // number of calls recorded when Seed ran
	products int
}

func (s *recordingSeeder) Seed(_ context.Context, _ string, products int, _ bool) error {
	s.seededAt = len(s.fake.recorded())
	s.products = products
	return nil
}

func TestRunDrivesTheFullOnboardingSequence(t *testing.T) {
	fake := &fakeControlPlane{verifyAfter: 1}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	seeder := &recordingSeeder{fake: fake}
	operator, err := client.NewOperator(srv.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("operator client: %v", err)
	}

	result, err := Run(t.Context(), operator, Options{
		Name:         "Acme Widgets",
		Products:     7,
		Timeout:      10 * time.Second,
		PollInterval: 10 * time.Millisecond,
		Seeder:       seeder,
		Progress:     io.Discard,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !result.AllLive() {
		t.Errorf("store did not go live: %+v", result.Stores)
	}
	if result.MerchantID != "merch-1" {
		t.Errorf("MerchantID = %q, want merch-1", result.MerchantID)
	}
	if result.Seed == 0 {
		t.Error("result carries no seed, so the run cannot be reproduced")
	}

	calls := fake.recorded()
	// The merchant must come first — everything else is scoped to the id it
	// returns — and the store must exist before anything configures it.
	wantOrder := []string{
		"POST /operator/merchants",
		"POST /stores",
		"PUT /stores/store-1/profile",
		"PUT /stores/store-1/payment-config",
		"POST /stores/store-1/connectors/pairing",
	}
	assertSubsequence(t, calls, wantOrder)
}

// Pairing triggers the first import, and products added after that import
// do not retroactively appear in it. Seeding must therefore happen before
// pairing — a rule that is invisible in the code and expensive to rediscover.
func TestCatalogIsSeededBeforePairing(t *testing.T) {
	fake := &fakeControlPlane{verifyAfter: 1}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	seeder := &recordingSeeder{fake: fake}
	operator, _ := client.NewOperator(srv.URL, 5*time.Second)

	if _, err := Run(t.Context(), operator, Options{
		Name: "Acme Widgets", Products: 7,
		Timeout: 10 * time.Second, PollInterval: 10 * time.Millisecond,
		Seeder: seeder, Progress: io.Discard,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	pairingIndex := indexOf(fake.recorded(), "POST /stores/store-1/connectors/pairing")
	if pairingIndex < 0 {
		t.Fatal("pairing was never called")
	}
	if seeder.seededAt > pairingIndex {
		t.Errorf("catalog was seeded after pairing (seed at call %d, pairing at call %d); "+
			"the first import runs on pairing and will not pick those products up",
			seeder.seededAt, pairingIndex)
	}
	if seeder.products != 7 {
		t.Errorf("seeded %d products, want the 7 that were requested", seeder.products)
	}
}

// Verification is asked for repeatedly rather than once: the states before
// it clear asynchronously, so a first "not yet" is normal, not fatal.
func TestVerificationIsRetriedUntilItPasses(t *testing.T) {
	fake := &fakeControlPlane{verifyAfter: 3}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	operator, _ := client.NewOperator(srv.URL, 5*time.Second)
	result, err := Run(t.Context(), operator, Options{
		Name: "Acme Widgets", Timeout: 10 * time.Second, PollInterval: time.Millisecond,
		Progress: io.Discard,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.AllLive() {
		t.Errorf("store did not go live after retried verification: %+v", result.Stores)
	}
	if fake.verifyCount < 3 {
		t.Errorf("verification was attempted %d times, want at least 3", fake.verifyCount)
	}
}

// A store that never verifies is a reportable outcome, not an error: the
// caller needs the result so it can say where the store stopped.
func TestStoreThatNeverVerifiesIsReportedNotErrored(t *testing.T) {
	fake := &fakeControlPlane{verifyAfter: 1 << 30}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	operator, _ := client.NewOperator(srv.URL, 5*time.Second)
	result, err := Run(t.Context(), operator, Options{
		Name: "Acme Widgets", Timeout: 50 * time.Millisecond, PollInterval: time.Millisecond,
		Progress: io.Discard,
	})
	if err != nil {
		t.Fatalf("Run returned an error for a store that merely did not finish: %v", err)
	}
	if result.AllLive() {
		t.Error("a store that never verified was reported as live")
	}
	if len(result.Stores) != 1 {
		t.Fatalf("got %d stores in the result, want 1", len(result.Stores))
	}
	if result.Stores[0].UCPState == StateActive {
		t.Errorf("UCPState = %q, want something short of %q", result.Stores[0].UCPState, StateActive)
	}
}

func TestRunRequiresAName(t *testing.T) {
	operator, _ := client.NewOperator("http://127.0.0.1:1", time.Second)
	if _, err := Run(t.Context(), operator, Options{}); err == nil {
		t.Fatal("Run with no merchant name succeeded")
	}
}

func TestMultipleStoresAreAllCreated(t *testing.T) {
	fake := &fakeControlPlane{verifyAfter: 1}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	operator, _ := client.NewOperator(srv.URL, 5*time.Second)
	result, err := Run(t.Context(), operator, Options{
		Name: "Acme Widgets", Stores: 3,
		Timeout: 10 * time.Second, PollInterval: time.Millisecond,
		Progress: io.Discard,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(result.Stores) != 3 {
		t.Fatalf("got %d stores, want 3", len(result.Stores))
	}
	// Distinct display names, or their catalogs collide.
	seen := map[string]bool{}
	for _, s := range result.Stores {
		if seen[s.DisplayName] {
			t.Errorf("duplicate store display name %q", s.DisplayName)
		}
		seen[s.DisplayName] = true
	}
}

func assertSubsequence(t *testing.T, got, want []string) {
	t.Helper()
	i := 0
	for _, call := range got {
		if i < len(want) && call == want[i] {
			i++
		}
	}
	if i != len(want) {
		t.Errorf("call sequence did not contain the expected order.\nwant (in order): %v\ngot: %v", want, got)
	}
}

func indexOf(haystack []string, needle string) int {
	for i, v := range haystack {
		if v == needle {
			return i
		}
	}
	return -1
}

// Timeout is a budget for the whole run, not for each phase.
//
// poll used to compute its own deadline on every call, so the three
// sequential waits (connector link, verification, journey) each took the
// full Timeout. --timeout 4m then permitted twelve minutes of wall time,
// and whatever bounded the caller's context cancelled the run mid-report
// instead of letting it say where the store actually stopped.
func TestTimeoutBoundsTheWholeRunNotEachPhase(t *testing.T) {
	// A control plane that never lets the store leave draft, so every
	// polling phase runs to its deadline.
	fake := &fakeControlPlane{verifyAfter: 1 << 30}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	operator, err := client.NewOperator(srv.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("operator client: %v", err)
	}

	const budget = 300 * time.Millisecond
	start := time.Now()
	if _, err := Run(t.Context(), operator, Options{
		Name:         "Acme Widgets",
		Timeout:      budget,
		PollInterval: 10 * time.Millisecond,
		Progress:     io.Discard,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	elapsed := time.Since(start)

	// Generous ceiling: the point is that it is bounded by roughly one
	// budget, not by one per phase. Two budgets would already mean the
	// per-phase behaviour is back.
	if ceiling := 2 * budget; elapsed > ceiling {
		t.Errorf("run took %s with a %s budget (ceiling %s) — the timeout is being applied per phase again",
			elapsed, budget, ceiling)
	}
}
