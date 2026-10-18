// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package endpointhealth

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/monitoring"
)

// recordingProber captures what was probed. ProbeStore is called from the
// subscriber's own goroutine, so every field is mutex-guarded and probed()
// signals completion rather than making the test sleep.
type recordingProber struct {
	mu    sync.Mutex
	refs  []monitoring.StoreRef
	calls chan monitoring.StoreRef
}

func newRecordingProber() *recordingProber {
	return &recordingProber{calls: make(chan monitoring.StoreRef, 8)}
}

func (p *recordingProber) ProbeStore(_ context.Context, ref monitoring.StoreRef) {
	p.mu.Lock()
	p.refs = append(p.refs, ref)
	p.mu.Unlock()
	p.calls <- ref
}

// probed waits briefly for one probe and returns it.
func (p *recordingProber) probed(t *testing.T) monitoring.StoreRef {
	t.Helper()
	select {
	case ref := <-p.calls:
		return ref
	case <-time.After(2 * time.Second):
		t.Fatal("no store was probed")
		return monitoring.StoreRef{}
	}
}

func (p *recordingProber) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.refs)
}

type stubRefResolver struct {
	ref monitoring.StoreRef
	err error

	mu       sync.Mutex
	gotMerch domain.MerchantID
	gotStore domain.StoreID
}

func (r *stubRefResolver) ResolveStoreRef(_ context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (monitoring.StoreRef, error) {
	r.mu.Lock()
	r.gotMerch, r.gotStore = merchantID, storeID
	r.mu.Unlock()
	if r.err != nil {
		return monitoring.StoreRef{}, r.err
	}
	return r.ref, nil
}

// stateChangedEvent builds a control.store.activation_state_changed event
// shaped exactly as activation.ActivationService.publishStateChanged
// marshals it.
func stateChangedEvent(merchantID, storeID, from, to string) eventbus.DomainEvent {
	payload, _ := json.Marshal(map[string]string{
		"store_id":   storeID,
		"from_state": from,
		"to_state":   to,
		"signal":     "verification_passed",
	})
	return &stubDomainEvent{
		eventType:  ucp.EventStoreActivationStateChanged,
		merchantID: merchantID,
		storeID:    storeID,
		payload:    payload,
	}
}

type stubDomainEvent struct {
	eventType  string
	merchantID string
	storeID    string
	payload    []byte
}

func (e *stubDomainEvent) EventType() string     { return e.eventType }
func (e *stubDomainEvent) MerchantID() string    { return e.merchantID }
func (e *stubDomainEvent) StoreID() string       { return e.storeID }
func (e *stubDomainEvent) OccurredAt() time.Time { return time.Now().UTC() }
func (e *stubDomainEvent) Payload() []byte       { return e.payload }

func newTestActivationSubscriber(prober storeProber, resolver storeRefResolver) *ActivationSubscriber {
	// nil bus: Start is not needed to exercise handle, and wiring a real
	// bus would test the bus rather than this.
	return NewActivationSubscriber(prober, resolver, nil, nil)
}

// The whole point: entering verified probes the store now, instead of
// leaving it to a cycle up to ProbeInterval away. Without this the store
// sits at verified — activation's only promoter to active is a passing
// profile_reachable probe.
func TestActivationSubscriberProbesOnVerified(t *testing.T) {
	prober := newRecordingProber()
	resolver := &stubRefResolver{ref: monitoring.StoreRef{
		MerchantID: "merch_1", StoreID: "store_1", Platform: "woocommerce",
	}}
	sub := newTestActivationSubscriber(prober, resolver)

	err := sub.handle(t.Context(), stateChangedEvent("merch_1", "store_1", "profile_published", "verified"))
	require.NoError(t, err)

	ref := prober.probed(t)
	assert.Equal(t, "store_1", ref.StoreID)
	assert.Equal(t, "merch_1", ref.MerchantID)
	assert.Equal(t, "woocommerce", ref.Platform,
		"the probe needs the platform, not just the id — it decides which connector shape to expect")

	require.NoError(t, sub.Stop(t.Context()))
}

// The lookup must be fenced on merchant as well as store (ADR-2): the
// event carries both, so there is no reason to resolve by store id alone.
func TestActivationSubscriberResolvesTenantFenced(t *testing.T) {
	prober := newRecordingProber()
	resolver := &stubRefResolver{ref: monitoring.StoreRef{MerchantID: "merch_7", StoreID: "store_7"}}
	sub := newTestActivationSubscriber(prober, resolver)

	require.NoError(t, sub.handle(t.Context(), stateChangedEvent("merch_7", "store_7", "profile_published", "verified")))
	prober.probed(t)

	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	assert.Equal(t, domain.MerchantID("merch_7"), resolver.gotMerch,
		"resolved without the merchant id, so the query underneath cannot be tenant-fenced")
	assert.Equal(t, domain.StoreID("store_7"), resolver.gotStore)

	require.NoError(t, sub.Stop(t.Context()))
}

// Every other transition is somebody else's business. Probing on all of
// them would turn each state change into an outbound HTTP burst against
// the merchant.
func TestActivationSubscriberIgnoresOtherTransitions(t *testing.T) {
	for _, to := range []string{"draft", "connector_linked", "profile_published", "active", "suspended"} {
		t.Run(to, func(t *testing.T) {
			prober := newRecordingProber()
			resolver := &stubRefResolver{ref: monitoring.StoreRef{StoreID: "store_1"}}
			sub := newTestActivationSubscriber(prober, resolver)

			require.NoError(t, sub.handle(t.Context(), stateChangedEvent("merch_1", "store_1", "verified", to)))
			require.NoError(t, sub.Stop(t.Context()))

			assert.Zero(t, prober.count(), "entering %s must not trigger a probe", to)
		})
	}
}

// A store the resolver cannot find is permanent, not retryable: returning
// an error would have the bus redeliver it forever. The next cycle still
// covers the store, which is the behavior this only shortens.
func TestActivationSubscriberSurvivesUnknownStore(t *testing.T) {
	prober := newRecordingProber()
	resolver := &stubRefResolver{err: errors.New("not found")}
	sub := newTestActivationSubscriber(prober, resolver)

	require.NoError(t, sub.handle(t.Context(), stateChangedEvent("merch_1", "store_gone", "profile_published", "verified")))
	require.NoError(t, sub.Stop(t.Context()))

	assert.Zero(t, prober.count())
}

func TestActivationSubscriberSurvivesMalformedPayload(t *testing.T) {
	prober := newRecordingProber()
	sub := newTestActivationSubscriber(prober, &stubRefResolver{})

	ev := &stubDomainEvent{
		eventType:  ucp.EventStoreActivationStateChanged,
		merchantID: "merch_1",
		storeID:    "store_1",
		payload:    []byte("{not json"),
	}
	require.NoError(t, sub.handle(t.Context(), ev))
	require.NoError(t, sub.Stop(t.Context()))

	assert.Zero(t, prober.count())
}

// The probe outlives the handler's delivery context by design — hence
// context.WithoutCancel. If it inherited cancellation, every probe would
// die the instant handle returned and the store would never reach active.
func TestActivationSubscriberProbeOutlivesTheDeliveryContext(t *testing.T) {
	release := make(chan struct{})
	prober := &contextCapturingProber{release: release, probed: make(chan context.Context, 1)}
	resolver := &stubRefResolver{ref: monitoring.StoreRef{MerchantID: "merch_1", StoreID: "store_1"}}
	sub := newTestActivationSubscriber(prober, resolver)

	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, sub.handle(ctx, stateChangedEvent("merch_1", "store_1", "profile_published", "verified")))
	// Ending the delivery context is what the bus does the moment the
	// handler returns.
	cancel()

	var probeCtx context.Context
	select {
	case probeCtx = <-prober.probed:
	case <-time.After(2 * time.Second):
		t.Fatal("no store was probed")
	}
	assert.NoError(t, probeCtx.Err(), "the probe context was cancelled with the delivery context")

	// It must still be bounded, not merely uncancellable.
	deadline, ok := probeCtx.Deadline()
	assert.True(t, ok, "the probe context has no deadline, so a hanging store leaks a goroutine")
	assert.WithinDuration(t, time.Now().Add(onVerifiedProbeTimeout), deadline, 10*time.Second)

	close(release)
	require.NoError(t, sub.Stop(t.Context()))
}

// contextCapturingProber hands its context back and blocks until released,
// so the test can inspect the context while the probe is still running.
type contextCapturingProber struct {
	release chan struct{}
	probed  chan context.Context
}

func (p *contextCapturingProber) ProbeStore(ctx context.Context, _ monitoring.StoreRef) {
	p.probed <- ctx
	<-p.release
}
