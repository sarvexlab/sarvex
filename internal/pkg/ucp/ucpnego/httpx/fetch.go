// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package httpx

import (
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/wire"
)

// MinProfileTTL is the floor the spec sets for profile caching: at least
// 60 seconds regardless of what the origin sends. See:
// https://ucp.dev/latest/specification/overview/#fetching
const MinProfileTTL = 60 * time.Second

// DefaultFetchTimeout bounds outbound profile fetches. Tune via Fetcher.
const DefaultFetchTimeout = 5 * time.Second

// ProfileSource fetches and caches UCP profiles. Implementations must:
//   - reject non-HTTPS URLs;
//   - refuse to follow redirects;
//   - enforce a TTL floor of MinProfileTTL;
//   - bound resource consumption regardless of the number of unique URLs.
type ProfileSource interface {
	Get(ctx context.Context, profileURL string) (*wire.Profile, error)
}

// Fetcher is a production-ready ProfileSource backed by a bounded LRU.
// Zero-value is not usable; build one with NewFetcher.
type Fetcher struct {
	client   *http.Client
	cache    *lruCache
	timeout  time.Duration
	ttlFloor time.Duration
	maxBytes int64
}

// FetcherOption configures a Fetcher.
type FetcherOption func(*Fetcher)

// WithHTTPClient overrides the default client. The client's redirect
// policy is forced to refuse all redirects regardless of caller config.
func WithHTTPClient(c *http.Client) FetcherOption {
	return func(f *Fetcher) { f.client = c }
}

// WithCacheSize sets the LRU capacity.
func WithCacheSize(n int) FetcherOption {
	return func(f *Fetcher) { f.cache = newLRU(n) }
}

// WithFetchTimeout overrides the per-fetch timeout.
func WithFetchTimeout(d time.Duration) FetcherOption {
	return func(f *Fetcher) { f.timeout = d }
}

// NewFetcher constructs a Fetcher with sensible defaults.
func NewFetcher(opts ...FetcherOption) *Fetcher {
	f := &Fetcher{
		cache:    newLRU(256),
		timeout:  DefaultFetchTimeout,
		ttlFloor: MinProfileTTL,
		maxBytes: 1 << 20,
	}
	for _, opt := range opts {
		opt(f)
	}
	if f.client == nil {
		f.client = &http.Client{Timeout: f.timeout}
	}
	// Force no-redirect policy regardless of provided client.
	prev := f.client.CheckRedirect
	f.client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if prev != nil {
			_ = prev(req, via) // honor side effects (logging) but override decision
		}
		return http.ErrUseLastResponse
	}
	return f
}

// Get returns a cached profile or fetches one fresh. Errors are typed
// (ErrProfileUnreachable, ErrProfileMalformed, ErrInvalidProfileURL) so
// callers can map them to UCP error codes.
func (f *Fetcher) Get(ctx context.Context, profileURL string) (*wire.Profile, error) {
	if err := validateProfileURL(profileURL); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidProfileURL, err)
	}
	if p, ok := f.cache.get(profileURL); ok {
		return p, nil
	}
	p, err := f.fetch(ctx, profileURL)
	if err != nil {
		return nil, err
	}
	f.cache.put(profileURL, p, f.ttlFloor)
	return p, nil
}

func (f *Fetcher) fetch(ctx context.Context, url string) (*wire.Profile, error) {
	reqCtx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", ErrInvalidProfileURL, err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProfileUnreachable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return nil, fmt.Errorf("%w: refusing redirect (status %d)", ErrProfileUnreachable, resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: status %d", ErrProfileUnreachable, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: read body: %v", ErrProfileUnreachable, err)
	}

	var p wire.Profile
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("%w: decode: %v", ErrProfileMalformed, err)
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProfileMalformed, err)
	}
	return &p, nil
}

// Sentinel errors mapped to UCP error codes by the response layer.
var (
	ErrInvalidProfileURL  = errors.New("invalid_profile_url")
	ErrProfileUnreachable = errors.New("profile_unreachable")
	ErrProfileMalformed   = errors.New("profile_malformed")
)

// --- LRU cache implementation -------------------------------------------

type lruEntry struct {
	url     string
	profile *wire.Profile
	expires time.Time
}

type lruCache struct {
	mu    sync.Mutex
	cap   int
	ll    *list.List
	index map[string]*list.Element
}

func newLRU(capacity int) *lruCache {
	if capacity <= 0 {
		capacity = 256
	}
	return &lruCache{
		cap:   capacity,
		ll:    list.New(),
		index: make(map[string]*list.Element, capacity),
	}
}

func (c *lruCache) get(url string) (*wire.Profile, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.index[url]
	if !ok {
		return nil, false
	}
	entry := el.Value.(*lruEntry)
	if time.Now().After(entry.expires) {
		c.ll.Remove(el)
		delete(c.index, url)
		return nil, false
	}
	c.ll.MoveToFront(el)
	return entry.profile, true
}

func (c *lruCache) put(url string, p *wire.Profile, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.index[url]; ok {
		entry := el.Value.(*lruEntry)
		entry.profile = p
		entry.expires = time.Now().Add(ttl)
		c.ll.MoveToFront(el)
		return
	}
	el := c.ll.PushFront(&lruEntry{url: url, profile: p, expires: time.Now().Add(ttl)})
	c.index[url] = el
	for c.ll.Len() > c.cap {
		oldest := c.ll.Back()
		if oldest == nil {
			return
		}
		c.ll.Remove(oldest)
		delete(c.index, oldest.Value.(*lruEntry).url)
	}
}
