// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package serving_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	servingpkg "github.com/sarvexlab/sarvex/internal/pkg/serving"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/planes/serving"
)

// recordingBus keeps what the handler publishes.
type recordingBus struct {
	eventbus.EventBus
	mu     sync.Mutex
	events []eventbus.DomainEvent
}

func (b *recordingBus) Publish(_ context.Context, e eventbus.DomainEvent) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, e)
	return nil
}

// Needs a real Redis (the artifact store is HSET/HGETALL); skips without
// REDIS_ADDR, like the repo's other Redis-backed tests.
func TestArtifactHandler_ServesFromCacheAndReportsFetch(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR not set; skipping Redis artifact handler test")
	}
	client := redis.NewClient(&redis.Options{Addr: addr, DB: 15})
	t.Cleanup(func() { _ = client.FlushDB(context.Background()); _ = client.Close() })
	store := servingpkg.NewRedisArtifacts(client)
	ctx := context.Background()
	require.NoError(t, store.PutArtifact(ctx, "tok123", servingpkg.Artifact{ContentType: "application/xml", Body: []byte("<rss/>")}))

	bus := &recordingBus{}
	h := serving.NewArtifactHandler(store, bus, nil, slog.Default())
	e := echo.New()
	e.GET(serving.ArtifactRoute, h.Get)

	// Served with the right type, extension tolerated, never cached.
	for _, path := range []string{"/artifacts/tok123.xml", "/artifacts/tok123"} {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		assert.Equal(t, http.StatusOK, rec.Code, path)
		assert.Equal(t, "<rss/>", rec.Body.String())
		assert.Contains(t, rec.Header().Get("Content-Type"), "application/xml")
		assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	}
	require.Len(t, bus.events, 2)
	assert.Equal(t, ucp.EventSurfaceArtifactFetched, bus.events[0].EventType())
	assert.JSONEq(t, `{"token":"tok123"}`, string(bus.events[0].Payload()))
	assert.Empty(t, bus.events[0].StoreID(), "the serving plane resolves no tenant for a fetch")

	// Unknown token: 404 and nothing reported.
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/artifacts/nope.xml", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Len(t, bus.events, 2)

	// Unpublished: gone, and deleting again is fine.
	require.NoError(t, store.DeleteArtifact(ctx, "tok123"))
	require.NoError(t, store.DeleteArtifact(ctx, "tok123"))
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/artifacts/tok123.xml", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// A snapshot swap rewrites every "serving:{store}:" key; a published artifact
// must survive it.
func TestArtifacts_SurviveASnapshotSwap(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR not set; skipping Redis artifact test")
	}
	cache := servingpkg.NewRedisCache(addr, "", 15, time.Minute)
	t.Cleanup(func() { _ = cache.Client().FlushDB(context.Background()) })
	arts := servingpkg.NewRedisArtifacts(cache.Client())
	ctx := context.Background()
	require.NoError(t, arts.PutArtifact(ctx, "tok", servingpkg.Artifact{ContentType: "text/plain", Body: []byte("x")}))

	require.NoError(t, cache.SwapSnapshot(ctx, &servingpkg.CacheSnapshot{
		StoreID: "s1", MerchantID: "m1", BuiltAt: time.Now(),
		Profile:  &servingpkg.CacheProfile{},
		Products: map[string]*servingpkg.CacheProduct{"a": {SKU: "a"}},
	}))

	got, err := arts.GetArtifact(ctx, "tok")
	require.NoError(t, err)
	assert.Equal(t, "x", string(got.Body))
}

// Rate limiting needs no Redis: the in-process limiter is the contract's other
// implementation, and the handler only sees the interface.
func TestArtifactHandler_RateLimitsPerTokenAndPerClient(t *testing.T) {
	store := &memArtifacts{m: map[string]servingpkg.Artifact{
		"hot":  {ContentType: "text/plain", Body: []byte("x")},
		"cold": {ContentType: "text/plain", Body: []byte("y")},
	}}
	bus := &recordingBus{}
	h := serving.NewArtifactHandler(store, bus, serving.NewRateLimiter(nil), slog.Default())
	e := echo.New()
	e.GET(serving.ArtifactRoute, h.Get)

	get := func(token, ip string) int {
		req := httptest.NewRequest(http.MethodGet, "/artifacts/"+token, nil)
		req.RemoteAddr = ip + ":1234"
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec.Code
	}

	// One token from one address: the 5/s bucket drains, then 429.
	var codes []int
	for range 8 {
		codes = append(codes, get("hot", "10.0.0.1"))
	}
	assert.Equal(t, http.StatusOK, codes[0])
	assert.Equal(t, http.StatusTooManyRequests, codes[7])

	// Limited requests are neither served nor reported as fetches.
	ok := 0
	for _, c := range codes {
		if c == http.StatusOK {
			ok++
		}
	}
	assert.Len(t, bus.events, ok)

	// Another address is not affected by the first's token bucket... but the
	// token's own bucket is shared, so the same token stays limited.
	assert.Equal(t, http.StatusTooManyRequests, get("hot", "10.0.0.2"))

	// A client probing many tokens exhausts its per-client bucket, even though
	// each token individually is fresh (and none exists).
	var probe []int
	for i := range 8 {
		probe = append(probe, get("guess"+string(rune('a'+i)), "10.9.9.9"))
	}
	assert.Equal(t, http.StatusNotFound, probe[0])
	assert.Equal(t, http.StatusTooManyRequests, probe[7])

	// An unrelated client and token is unaffected.
	assert.Equal(t, http.StatusOK, get("cold", "10.0.0.3"))
}

type memArtifacts struct {
	m map[string]servingpkg.Artifact
}

func (s *memArtifacts) GetArtifact(_ context.Context, token string) (*servingpkg.Artifact, error) {
	a, ok := s.m[token]
	if !ok {
		return nil, servingpkg.ErrNotFound
	}
	return &a, nil
}
