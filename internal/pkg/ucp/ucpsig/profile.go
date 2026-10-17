// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package ucpsig

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// ProfileResolver fetches signing profiles for a given URL. Implementations
// MUST enforce HTTPS and SHOULD cache responses to avoid hitting the well-known
// endpoint on every verification.
type ProfileResolver interface {
	Resolve(ctx context.Context, profileURL string) (*Profile, error)
}

// HTTPProfileResolver fetches profiles via HTTPS with in-memory caching and
// in-flight deduplication. The zero value is not usable; call NewHTTPProfileResolver.
type HTTPProfileResolver struct {
	client *http.Client
	ttl    time.Duration

	// Trust is an optional allow-list of profile URLs (or hosts) that the
	// resolver will accept. When non-nil, any URL outside the list yields
	// ErrProfileNotTrusted. Useful on the business side when only known
	// platforms are allowed to drive your endpoint.
	Trust func(profileURL string) bool

	// AllowInsecureHTTP, when true, permits http:// profile URLs. This is
	// FOR LOCAL TESTING ONLY and MUST remain false in production. The UCP
	// spec mandates HTTPS for profile fetches.
	AllowInsecureHTTP bool

	mu      sync.Mutex
	cache   map[string]*cacheEntry
	flights map[string]*flight
}

type cacheEntry struct {
	profile *Profile
	expires time.Time
}

type flight struct {
	done chan struct{}
	p    *Profile
	err  error
}

// NewHTTPProfileResolver constructs a resolver. ttl controls how long a
// successfully-fetched profile is cached. A reasonable default is 5 minutes;
// shorter TTLs improve revocation latency, longer TTLs reduce load.
func NewHTTPProfileResolver(client *http.Client, ttl time.Duration) *HTTPProfileResolver {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &HTTPProfileResolver{
		client:  client,
		ttl:     ttl,
		cache:   make(map[string]*cacheEntry),
		flights: make(map[string]*flight),
	}
}

// Resolve implements ProfileResolver.
func (r *HTTPProfileResolver) Resolve(ctx context.Context, profileURL string) (*Profile, error) {
	u, err := url.Parse(profileURL)
	if err != nil {
		return nil, ErrInvalidProfileURL.withCause(err)
	}
	if u.Scheme != "https" && !(r.AllowInsecureHTTP && u.Scheme == "http") {
		return nil, ErrInvalidProfileURL.withCause(fmt.Errorf("scheme must be https, got %q", u.Scheme))
	}
	if r.Trust != nil && !r.Trust(profileURL) {
		return nil, ErrProfileNotTrusted
	}

	now := time.Now()

	r.mu.Lock()
	if ce, ok := r.cache[profileURL]; ok && now.Before(ce.expires) {
		r.mu.Unlock()
		return ce.profile, nil
	}
	if f, ok := r.flights[profileURL]; ok {
		r.mu.Unlock()
		select {
		case <-f.done:
			return f.p, f.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f := &flight{done: make(chan struct{})}
	r.flights[profileURL] = f
	r.mu.Unlock()

	p, err := r.fetch(ctx, profileURL)

	r.mu.Lock()
	delete(r.flights, profileURL)
	if err == nil {
		r.cache[profileURL] = &cacheEntry{profile: p, expires: time.Now().Add(r.ttl)}
	}
	r.mu.Unlock()

	f.p, f.err = p, err
	close(f.done)
	return p, err
}

func (r *HTTPProfileResolver) fetch(ctx context.Context, profileURL string) (*Profile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, profileURL, nil)
	if err != nil {
		return nil, ErrProfileUnreachable.withCause(err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, ErrProfileUnreachable.withCause(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		// The spec forbids following redirects for profile fetches.
		return nil, ErrProfileUnreachable.withCause(fmt.Errorf("status %d", resp.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB cap
	if err != nil {
		return nil, ErrProfileUnreachable.withCause(err)
	}
	var p Profile
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, ErrProfileUnreachable.withCause(fmt.Errorf("decode profile: %w", err))
	}
	if !p.hasKeys() {
		return nil, ErrProfileUnreachable.withCause(fmt.Errorf("profile has no signing keys"))
	}
	return &p, nil
}

// StaticProfileResolver returns a fixed profile regardless of URL. Useful in
// tests or when keys are provisioned out of band.
type StaticProfileResolver struct {
	Profile *Profile
}

func (s StaticProfileResolver) Resolve(_ context.Context, _ string) (*Profile, error) {
	if s.Profile == nil {
		return nil, ErrProfileUnreachable
	}
	return s.Profile, nil
}
