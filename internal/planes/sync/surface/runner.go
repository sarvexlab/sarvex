// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package surface

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/serving"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/surfaces"
)

const (
	// debounceWindow coalesces a burst of catalog changes (a bulk import
	// emits one per batch) into one regeneration per store and artifact.
	debounceWindow = 20 * time.Second
	// safetyNetInterval is the backstop for missed events.
	safetyNetInterval = time.Hour
)

// Runner drives every registered Generator. It holds no surface knowledge:
// which stores to generate for comes from the enrollment source, what to
// generate from the Generator, and where it goes from the shared artifact
// record.
type Runner struct {
	generators []Generator
	enrolled   EnrollmentSource
	catalog    CatalogSource
	store      ArtifactStore
	cache      serving.ArtifactWriter
	bus        eventbus.EventBus
	// publicURL renders the address an artifact is served at for a token.
	publicURL func(token string) string
	log       *slog.Logger

	mu     sync.Mutex
	timers map[string]*time.Timer
	subs   []eventbus.Subscription
	stop   chan struct{}
}

// Deps are the Runner's collaborators.
type Deps struct {
	Generators []Generator
	Enrolled   EnrollmentSource
	Catalog    CatalogSource
	Store      ArtifactStore
	Cache      serving.ArtifactWriter
	Bus        eventbus.EventBus
	PublicURL  func(token string) string
	Log        *slog.Logger
}

// NewRunner constructs a Runner.
func NewRunner(d Deps) *Runner {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return &Runner{
		generators: d.Generators,
		enrolled:   d.Enrolled,
		catalog:    d.Catalog,
		store:      d.Store,
		cache:      d.Cache,
		bus:        d.Bus,
		publicURL:  d.PublicURL,
		log:        d.Log,
		timers:     map[string]*time.Timer{},
		stop:       make(chan struct{}),
	}
}

// Regenerate generates one generator's artifact for one store, if the store is
// on the generator's surface. A store that is not is a no-op: that is the
// whole of "artifacts exist only for enrolling stores".
func (r *Runner) Regenerate(ctx context.Context, g Generator, merchantID, storeID string) error {
	enr, ok, err := r.enrolled.Get(ctx, merchantID, storeID, g.SurfaceID())
	if err != nil {
		// Treated as "do not generate": publishing for a store that is not
		// enrolled is the leak this gate exists to prevent, and the safety
		// net retries once the read works again.
		return fmt.Errorf("read enrollment: %w", err)
	}
	if !ok {
		return nil
	}

	existing, err := r.store.Get(ctx, merchantID, storeID, g.SurfaceID(), g.ArtifactID())
	if err != nil && !errors.Is(err, ErrNoRecord) {
		return err
	}
	token := ""
	if existing != nil {
		token = existing.Token
	}
	if token == "" {
		if token, err = newToken(); err != nil {
			return err
		}
	}

	products, err := r.catalog.Products(ctx, merchantID, storeID)
	if err != nil {
		return fmt.Errorf("list catalog: %w", err)
	}
	url := r.publicURL(token) + g.Extension()
	out, err := g.Generate(ctx, Input{
		MerchantID: merchantID, StoreID: storeID, Config: enr.Config, Products: products, URL: url,
	})
	if err != nil {
		return fmt.Errorf("generate %s/%s: %w", g.SurfaceID(), g.ArtifactID(), err)
	}

	// Publish before recording: the control plane must never be told an
	// artifact exists at a URL that does not serve it yet.
	if err := r.cache.PutArtifact(ctx, token, serving.Artifact{ContentType: out.ContentType, Body: out.Body}); err != nil {
		return err
	}
	now := time.Now().UTC()
	if _, err := r.store.Save(ctx, surfaces.ArtifactRecord{
		MerchantID: merchantID, StoreID: storeID, SurfaceID: g.SurfaceID(), ArtifactID: g.ArtifactID(),
		Token: token, URL: url, Detail: out.Detail, GeneratedAt: &now,
	}); err != nil {
		return err
	}
	r.publishUpdated(ctx, merchantID, storeID, g, "generated")
	return nil
}

// Unpublish removes a store's artifact for a generator, on withdrawal.
func (r *Runner) Unpublish(ctx context.Context, g Generator, merchantID, storeID string) error {
	rec, err := r.store.Get(ctx, merchantID, storeID, g.SurfaceID(), g.ArtifactID())
	if errors.Is(err, ErrNoRecord) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := r.cache.DeleteArtifact(ctx, rec.Token); err != nil {
		return err
	}
	return r.store.Delete(ctx, merchantID, storeID, g.SurfaceID(), g.ArtifactID())
}

// HandleSurfaceStateChanged reacts to control.store.surface_state_changed:
// generate when a store starts on the surface, unpublish when it leaves.
func (r *Runner) HandleSurfaceStateChanged(ctx context.Context, e eventbus.DomainEvent) error {
	var p struct {
		StoreID   string `json:"store_id"`
		SurfaceID string `json:"surface_id"`
		ToState   string `json:"to_state"`
	}
	if err := json.Unmarshal(e.Payload(), &p); err != nil {
		r.log.Error("surface: bad state-change payload", "error", err)
		return nil
	}
	for _, g := range r.generators {
		if g.SurfaceID() != p.SurfaceID {
			continue
		}
		var err error
		if p.ToState == surfaceStateAvailable {
			err = r.Unpublish(ctx, g, e.MerchantID(), p.StoreID)
		} else {
			err = r.Regenerate(ctx, g, e.MerchantID(), p.StoreID)
		}
		if err != nil {
			r.log.Error("surface: state-change handling failed", "error", err,
				"surface_id", p.SurfaceID, "store_id", p.StoreID)
		}
	}
	return nil
}

// HandleCatalogChanged reacts to a catalog change by scheduling a debounced
// regeneration of every artifact for the store.
func (r *Runner) HandleCatalogChanged(_ context.Context, e eventbus.DomainEvent) error {
	merchantID, storeID := e.MerchantID(), e.StoreID()
	if merchantID == "" || storeID == "" {
		return nil
	}
	for _, g := range r.generators {
		r.schedule(g, merchantID, storeID)
	}
	return nil
}

func (r *Runner) schedule(g Generator, merchantID, storeID string) {
	key := merchantID + ":" + storeID + ":" + g.SurfaceID() + ":" + g.ArtifactID()
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, pending := r.timers[key]; pending {
		return // the pending run will see the latest catalog
	}
	r.timers[key] = time.AfterFunc(debounceWindow, func() {
		r.mu.Lock()
		delete(r.timers, key)
		r.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := r.Regenerate(ctx, g, merchantID, storeID); err != nil {
			r.log.Error("surface: debounced regeneration failed", "error", err, "key", key)
		}
	})
}

// HandleArtifactFetched attributes a serving-plane fetch to its artifact.
func (r *Runner) HandleArtifactFetched(ctx context.Context, e eventbus.DomainEvent) error {
	var p struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(e.Payload(), &p); err != nil {
		// A malformed event can never become valid on redelivery: drop it.
		r.log.Error("surface: bad artifact-fetched payload", "error", err)
		return nil
	}
	if p.Token == "" {
		return nil
	}
	rec, err := r.store.FindByToken(ctx, p.Token)
	if err != nil {
		// An unknown token is a token we no longer (or never) issued:
		// nothing to attribute, and not worth a log line.
		if !errors.Is(err, ErrNoRecord) {
			r.log.Error("surface: fetch attribution failed", "error", err)
		}
		return nil
	}
	first, err := r.store.MarkFetched(ctx, rec, e.OccurredAt())
	if err != nil {
		r.log.Error("surface: record fetch failed", "error", err, "store_id", rec.StoreID)
		return nil
	}
	if first {
		r.publishUpdatedFor(ctx, rec.MerchantID, rec.StoreID, rec.SurfaceID, rec.ArtifactID, "first_fetch")
	}
	return nil
}

// RunSafetyNet regenerates every enrolled store's artifacts once.
func (r *Runner) RunSafetyNet(ctx context.Context) {
	for _, g := range r.generators {
		list, err := r.enrolled.List(ctx, g.SurfaceID())
		if err != nil {
			r.log.Error("surface: safety net list failed", "error", err, "surface_id", g.SurfaceID())
			continue
		}
		for _, enr := range list {
			if err := r.Regenerate(ctx, g, enr.MerchantID, enr.StoreID); err != nil {
				r.log.Error("surface: safety net regeneration failed", "error", err,
					"surface_id", g.SurfaceID(), "store_id", enr.StoreID)
			}
		}
	}
}

// Start subscribes to the bus and starts the safety net.
func (r *Runner) Start(ctx context.Context) error {
	if len(r.generators) == 0 {
		return nil
	}
	for _, s := range []struct {
		topic   string
		handler eventbus.EventHandler
	}{
		{ucp.EventStoreSurfaceStateChanged, r.HandleSurfaceStateChanged},
		{ucp.EventCatalogProductsReady, r.HandleCatalogChanged},
		{ucp.EventSurfaceArtifactFetched, r.HandleArtifactFetched},
	} {
		sub, err := r.bus.Subscribe(ctx, s.topic, s.handler)
		if err != nil {
			return fmt.Errorf("subscribe %s: %w", s.topic, err)
		}
		r.subs = append(r.subs, sub)
	}
	go func() {
		t := time.NewTicker(safetyNetInterval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				r.RunSafetyNet(context.Background())
			case <-r.stop:
				return
			}
		}
	}()
	return nil
}

// Stop unsubscribes and stops the safety net and any pending debounce.
func (r *Runner) Stop(ctx context.Context) {
	close(r.stop)
	for _, s := range r.subs {
		if err := r.bus.Unsubscribe(ctx, s); err != nil {
			r.log.Error("surface: unsubscribe failed", "topic", s.Topic(), "error", err)
		}
	}
	r.mu.Lock()
	for k, t := range r.timers {
		t.Stop()
		delete(r.timers, k)
	}
	r.mu.Unlock()
}

func (r *Runner) publishUpdated(ctx context.Context, merchantID, storeID string, g Generator, what string) {
	r.publishUpdatedFor(ctx, merchantID, storeID, g.SurfaceID(), g.ArtifactID(), what)
}

func (r *Runner) publishUpdatedFor(ctx context.Context, merchantID, storeID, surfaceID, artifactID, what string) {
	if r.bus == nil {
		return
	}
	payload, _ := json.Marshal(map[string]string{
		"store_id": storeID, "surface_id": surfaceID, "artifact_id": artifactID, "what": what,
	})
	if err := r.bus.Publish(ctx, &event{
		typ: ucp.EventSurfaceArtifactUpdated, merchantID: merchantID, storeID: storeID, payload: payload, at: time.Now().UTC(),
	}); err != nil {
		r.log.Warn("surface: failed to publish artifact update", "error", err, "store_id", storeID)
	}
}

func newToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("mint artifact token: %w", err)
	}
	return strings.ToLower(hex.EncodeToString(b)), nil
}

type event struct {
	typ, merchantID, storeID string
	payload                  []byte
	at                       time.Time
}

func (e *event) EventType() string     { return e.typ }
func (e *event) MerchantID() string    { return e.merchantID }
func (e *event) StoreID() string       { return e.storeID }
func (e *event) OccurredAt() time.Time { return e.at }
func (e *event) Payload() []byte       { return e.payload }
