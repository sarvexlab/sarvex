// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package surface_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/pkg/serving"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/surfaces"
	"github.com/sarvexlab/sarvex/internal/planes/sync/surface"
)

// A generator with nothing Google about it: the runner must work for any
// surface.
type fixtureGen struct{}

func (fixtureGen) SurfaceID() string  { return "fixture" }
func (fixtureGen) ArtifactID() string { return "doc" }
func (fixtureGen) Extension() string  { return ".txt" }
func (fixtureGen) Generate(_ context.Context, in surface.Input) (surface.Output, error) {
	return surface.Output{ContentType: "text/plain", Body: []byte(in.Config["greeting"]), Detail: map[string]string{"n": "1"}}, nil
}

type fakeEnrolled map[string]surface.Enrollment

func (f fakeEnrolled) Get(_ context.Context, m, s, surf string) (surface.Enrollment, bool, error) {
	e, ok := f[m+":"+s+":"+surf]
	return e, ok, nil
}
func (f fakeEnrolled) List(_ context.Context, surf string) ([]surface.Enrollment, error) {
	var out []surface.Enrollment
	for _, e := range f {
		if e.SurfaceID == surf {
			out = append(out, e)
		}
	}
	return out, nil
}

type fakeCatalog struct{}

func (fakeCatalog) Products(context.Context, string, string) ([]surface.Product, error) {
	return []surface.Product{{SKU: "a"}}, nil
}

type fakeStore struct {
	mu   sync.Mutex
	recs map[string]*surfaces.ArtifactRecord
}

func newFakeStore() *fakeStore { return &fakeStore{recs: map[string]*surfaces.ArtifactRecord{}} }

func key(m, s, sf, a string) string { return surfaces.ArtifactDocumentID(m, s, sf, a) }

func (f *fakeStore) Get(_ context.Context, m, s, sf, a string) (*surfaces.ArtifactRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r, ok := f.recs[key(m, s, sf, a)]; ok {
		c := *r
		return &c, nil
	}
	return nil, surface.ErrNoRecord
}
func (f *fakeStore) Save(_ context.Context, rec surfaces.ArtifactRecord) (*surfaces.ArtifactRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := key(rec.MerchantID, rec.StoreID, rec.SurfaceID, rec.ArtifactID)
	if old, ok := f.recs[k]; ok {
		rec.Token, rec.FirstFetchedAt, rec.LastFetchedAt = old.Token, old.FirstFetchedAt, old.LastFetchedAt
	}
	f.recs[k] = &rec
	c := rec
	return &c, nil
}
func (f *fakeStore) FindByToken(_ context.Context, tok string) (*surfaces.ArtifactRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.recs {
		if r.Token == tok {
			c := *r
			return &c, nil
		}
	}
	return nil, surface.ErrNoRecord
}
func (f *fakeStore) MarkFetched(_ context.Context, rec *surfaces.ArtifactRecord, t time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.recs[key(rec.MerchantID, rec.StoreID, rec.SurfaceID, rec.ArtifactID)]
	first := r.FirstFetchedAt == nil
	if first {
		r.FirstFetchedAt = &t
	}
	r.LastFetchedAt = &t
	return first, nil
}
func (f *fakeStore) Delete(_ context.Context, m, s, sf, a string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.recs, key(m, s, sf, a))
	return nil
}

type fakeCache struct{ blobs map[string]serving.Artifact }

func (c *fakeCache) PutArtifact(_ context.Context, tok string, a serving.Artifact) error {
	c.blobs[tok] = a
	return nil
}
func (c *fakeCache) DeleteArtifact(_ context.Context, tok string) error {
	delete(c.blobs, tok)
	return nil
}

func newRunner(enrolled fakeEnrolled) (*surface.Runner, *fakeStore, *fakeCache) {
	st, cache := newFakeStore(), &fakeCache{blobs: map[string]serving.Artifact{}}
	return surface.NewRunner(surface.Deps{
		Generators: []surface.Generator{fixtureGen{}},
		Enrolled:   enrolled, Catalog: fakeCatalog{}, Store: st, Cache: cache,
		PublicURL: func(tok string) string { return "https://x/artifacts/" + tok },
	}), st, cache
}

var enrolledM1S1 = fakeEnrolled{"m1:s1:fixture": {MerchantID: "m1", StoreID: "s1", SurfaceID: "fixture", Config: map[string]string{"greeting": "hi"}}}

func TestRegenerate_NothingForAStoreNotOnTheSurface(t *testing.T) {
	r, st, cache := newRunner(fakeEnrolled{})
	require.NoError(t, r.Regenerate(context.Background(), fixtureGen{}, "m1", "s1"))
	assert.Empty(t, st.recs)
	assert.Empty(t, cache.blobs)
}

func TestRegenerate_PublishesAndKeepsTokenStable(t *testing.T) {
	r, st, cache := newRunner(enrolledM1S1)
	ctx := context.Background()
	require.NoError(t, r.Regenerate(ctx, fixtureGen{}, "m1", "s1"))
	first, err := st.Get(ctx, "m1", "s1", "fixture", "doc")
	require.NoError(t, err)
	assert.NotEmpty(t, first.Token)
	assert.Equal(t, "https://x/artifacts/"+first.Token+".txt", first.URL)
	assert.Equal(t, "hi", string(cache.blobs[first.Token].Body))

	require.NoError(t, r.Regenerate(ctx, fixtureGen{}, "m1", "s1"))
	second, _ := st.Get(ctx, "m1", "s1", "fixture", "doc")
	assert.Equal(t, first.Token, second.Token, "the URL handed to the surface must not rotate")
}

func TestTenantFence_OtherStoreGetsNothing(t *testing.T) {
	r, st, _ := newRunner(enrolledM1S1)
	require.NoError(t, r.Regenerate(context.Background(), fixtureGen{}, "m2", "s1"))
	assert.Empty(t, st.recs, "same store id under another merchant is not enrolled")
}

func TestUnpublish_RemovesBlobAndRecord(t *testing.T) {
	r, st, cache := newRunner(enrolledM1S1)
	ctx := context.Background()
	require.NoError(t, r.Regenerate(ctx, fixtureGen{}, "m1", "s1"))
	require.NoError(t, r.Unpublish(ctx, fixtureGen{}, "m1", "s1"))
	assert.Empty(t, st.recs)
	assert.Empty(t, cache.blobs)
	assert.NoError(t, r.Unpublish(ctx, fixtureGen{}, "m1", "s1"), "idempotent")
}

type evt struct{ payload []byte }

func (evt) EventType() string       { return "" }
func (evt) MerchantID() string      { return "m1" }
func (evt) StoreID() string         { return "" }
func (e evt) OccurredAt() time.Time { return time.Now() }
func (e evt) Payload() []byte       { return e.payload }

func TestFetch_FirstFetchRecordedOnce(t *testing.T) {
	r, st, _ := newRunner(enrolledM1S1)
	ctx := context.Background()
	require.NoError(t, r.Regenerate(ctx, fixtureGen{}, "m1", "s1"))
	rec, _ := st.Get(ctx, "m1", "s1", "fixture", "doc")
	assert.False(t, rec.Observed(surfaces.SignalFirstFetch))

	e := evt{payload: []byte(`{"token":"` + rec.Token + `"}`)}
	require.NoError(t, r.HandleArtifactFetched(ctx, e))
	rec, _ = st.Get(ctx, "m1", "s1", "fixture", "doc")
	require.True(t, rec.Observed(surfaces.SignalFirstFetch))
	firstAt := *rec.FirstFetchedAt

	time.Sleep(2 * time.Millisecond)
	require.NoError(t, r.HandleArtifactFetched(ctx, e))
	rec, _ = st.Get(ctx, "m1", "s1", "fixture", "doc")
	assert.True(t, firstAt.Equal(*rec.FirstFetchedAt), "first fetch is never overwritten")

	assert.NoError(t, r.HandleArtifactFetched(ctx, evt{payload: []byte(`{"token":"nope"}`)}), "unknown token is ignored")
}

func TestSurfaceStateChanged_GeneratesThenUnpublishes(t *testing.T) {
	r, st, _ := newRunner(enrolledM1S1)
	ctx := context.Background()
	require.NoError(t, r.HandleSurfaceStateChanged(ctx, evt{payload: []byte(`{"store_id":"s1","surface_id":"fixture","to_state":"prerequisites_pending"}`)}))
	assert.Len(t, st.recs, 1)
	require.NoError(t, r.HandleSurfaceStateChanged(ctx, evt{payload: []byte(`{"store_id":"s1","surface_id":"other","to_state":"prerequisites_pending"}`)}))
	assert.Len(t, st.recs, 1, "another surface's transition is ignored")
	require.NoError(t, r.HandleSurfaceStateChanged(ctx, evt{payload: []byte(`{"store_id":"s1","surface_id":"fixture","to_state":"available"}`)}))
	assert.Empty(t, st.recs)
}
