// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package negotiation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

// Config holds NegotiationService configuration.
type Config struct {
	// PlatformProfileCacheTTL is used when no HTTP cache headers are present.
	PlatformProfileCacheTTL time.Duration // default: 5m

	// NegotiationCacheTTL is the max TTL for a cached NegotiationResult.
	// Effective TTL = min(remainingPlatformProfileTTL, NegotiationCacheTTL).
	NegotiationCacheTTL time.Duration // default: 5m

	// HTTPTimeout for fetching platform profiles.
	HTTPTimeout time.Duration // default: 5s

	// SkipNamespaceValidation disables spec-origin checking (for testing only).
	SkipNamespaceValidation bool

	// ServableVersions are the UCP protocol versions this negotiation
	// service will actually select as the response version — see
	// SelectVersion and WithServableVersions. Left empty, negotiatedVersion
	// falls back to the original single-version gate (inst.Version() is
	// the only candidate), so existing callers that never set this are
	// unaffected.
	ServableVersions []wire.Version

	// CheckoutServableVersions restricts NegotiateCheckout to a narrower
	// (or different) set than ServableVersions — see NegotiateCheckout's
	// doc comment. Left nil, NegotiateCheckout falls back to
	// ServableVersions, i.e. no restriction beyond what Negotiate itself
	// allows.
	CheckoutServableVersions []wire.Version
}

// NegotiationService is a stateless UCP negotiation engine.
type NegotiationService struct {
	cache      NegotiationCache
	httpClient *http.Client
	config     Config
	provider   ucp.StoreProviderFunc
}

// Option configures a NegotiationService.
type Option func(*NegotiationService)

// WithHTTPClient sets the HTTP client used for profile fetches.
func WithHTTPClient(client *http.Client) Option {
	return func(s *NegotiationService) { s.httpClient = client }
}

func WithStoreProvider(provider ucp.StoreProviderFunc) Option {
	return func(s *NegotiationService) { s.provider = provider }
}

// WithSkipNamespaceValidation disables namespace binding validation (for testing only).
func WithSkipNamespaceValidation(skip bool) Option {
	return func(s *NegotiationService) { s.config.SkipNamespaceValidation = skip }
}

// WithServableVersions sets the candidate set negotiatedVersion selects
// from — see SelectVersion. Production wiring passes ServableVersions()
// (the generations that serve the catalog); tests that want to prove multi-version selection can
// pass their own set instead of relying on the single-version fallback.
func WithServableVersions(versions []wire.Version) Option {
	return func(s *NegotiationService) { s.config.ServableVersions = versions }
}

// WithCheckoutServableVersions sets the candidate set NegotiateCheckout
// selects from, independent of WithServableVersions. Production wiring
// passes CheckoutServableVersions() (the generations that serve checkout — see its doc
// comment on why checkout may trail ServableVersions while a new
// generation's checkout support is built).
func WithCheckoutServableVersions(versions []wire.Version) Option {
	return func(s *NegotiationService) { s.config.CheckoutServableVersions = versions }
}

// NewNegotiationService creates a NegotiationService with sensible defaults.
func NewNegotiationService(opts ...Option) *NegotiationService {
	s := &NegotiationService{
		config: Config{
			PlatformProfileCacheTTL: 5 * time.Minute,
			NegotiationCacheTTL:     5 * time.Minute,
			HTTPTimeout:             5 * time.Second,
		},
		cache: NewMemoryCache(),
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.httpClient == nil {
		s.httpClient = &http.Client{Timeout: s.config.HTTPTimeout}
	}
	return s
}

// ParsePlatformProfileURI parses the UCP-Agent header and extracts the profile URI.
// Returns ("", nil) if the header is absent — callers should treat this as a non-UCP request.
// Returns an error if the header is present but malformed.
func (s *NegotiationService) ParsePlatformProfileURI(r *http.Request) (string, error) {
	header := r.Header.Get("UCP-Agent")
	if header == "" {
		return "", nil
	}

	// RFC 8941 dictionary: look for profile="..."
	const prefix = `profile="`
	_, after, ok := strings.Cut(header, prefix)
	if !ok {
		return "", &ucp.ParseError{Header: header, Reason: "missing profile parameter"}
	}
	rest := after
	before, _, ok0 := strings.Cut(rest, `"`)
	if !ok0 {
		return "", &ucp.ParseError{Header: header, Reason: "unterminated quoted string"}
	}
	uri := before
	if uri == "" {
		return "", &ucp.ParseError{Header: header, Reason: "empty profile URI"}
	}
	return uri, nil
}

// ParseHeaderVersion extracts the UCP-Agent header's version= member (RFC
// 8941 dictionary), or "" if the header is absent or carries no version=
// member. Unlike ParsePlatformProfileURI, an absent header or member is
// never an error here — version= has no spec-mandated required status the
// way profile= does; it is only consulted by negotiate's
// isDereferenceableURL fallback, itself an exception path.
func (s *NegotiationService) ParseHeaderVersion(r *http.Request) string {
	header := r.Header.Get("UCP-Agent")
	if header == "" {
		return ""
	}
	const prefix = `version="`
	_, after, ok := strings.Cut(header, prefix)
	if !ok {
		return ""
	}
	version, _, ok := strings.Cut(after, `"`)
	if !ok {
		return ""
	}
	return version
}

// FetchPlatformProfile fetches and caches the platform's UCP profile.
// Returns ProfileUnreachableError for network/non-2xx failures and ProfileMalformedError
// for invalid JSON.
func (s *NegotiationService) FetchPlatformProfile(ctx context.Context, profileURI string) (*wire.Profile, error) {
	if cached, ok := s.cache.GetPlatformProfile(profileURI); ok {
		return cached, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, profileURI, nil)
	if err != nil {
		return nil, &ucp.ProfileUnreachableError{Cause: fmt.Sprintf("building profile request: %v", err)}
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, &ucp.ProfileUnreachableError{Cause: fmt.Sprintf("fetching platform profile: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &ucp.ProfileUnreachableError{Cause: fmt.Sprintf("platform profile returned status %d", resp.StatusCode)}
	}

	var profile wire.Profile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, &ucp.ProfileMalformedError{Cause: fmt.Sprintf("decoding platform profile: %v", err)}
	}

	ttl := parseCacheTTL(resp.Header, s.config.PlatformProfileCacheTTL)
	s.cache.SetPlatformProfile(profileURI, &profile, ttl)
	return &profile, nil
}

// ValidateNamespaceBinding verifies that each capability's spec URL origin matches
// the origin implied by the capability's reverse-domain name prefix.
func (s *NegotiationService) ValidateNamespaceBinding(profile *wire.Profile) error {
	if s.config.SkipNamespaceValidation {
		return nil
	}

	var mismatches []string
	for capName, caps := range profile.UCP.Capabilities {
		expectedOrigin := capabilityOrigin(capName)
		for _, cap := range caps {
			if cap.Spec == "" {
				continue
			}
			u, err := url.Parse(cap.Spec)
			if err != nil {
				mismatches = append(mismatches, fmt.Sprintf("%s: invalid spec URL %q", capName, cap.Spec))
				continue
			}
			actualOrigin := u.Scheme + "://" + u.Host
			if actualOrigin != expectedOrigin {
				mismatches = append(mismatches, fmt.Sprintf(
					"%s: expected origin %s, got %s", capName, expectedOrigin, actualOrigin,
				))
			}
		}
	}
	if len(mismatches) > 0 {
		return &ucp.NamespaceValidationError{Mismatches: mismatches}
	}
	return nil
}

// negotiatedVersion determines and validates the protocol version this
// negotiation will report and respond in, selecting from
// Config.ServableVersions. See negotiatedVersionFor for the selection rule.
func (s *NegotiationService) negotiatedVersion(inst ucp.StoreProvider, platformProfile *wire.Profile) (wire.Version, error) {
	return s.negotiatedVersionFor(inst, platformProfile, s.config.ServableVersions)
}

// negotiatedVersionFor is negotiatedVersion generalized over an explicit
// servable set, so Negotiate and NegotiateCheckout can each select from
// their own set (Config.ServableVersions vs Config.CheckoutServableVersions)
// through the same rule.
//
// Default (servable empty): preserves the original single-version gate
// exactly — inst.Version() is the only candidate, and the negotiated
// version is always the platform's own declared version, which must not
// be newer than inst.Version(). No behavior change for any caller that
// doesn't opt into ServableVersions.
//
// Configured (the real multi-version production path — see
// WithServableVersions/ServableVersions in version.go): the negotiated
// version is the highest of servable that does not exceed the platform's
// declared version (SelectVersion) — "find the highest version we support
// that the platform also supports," instead of comparing a single
// hardcoded pair. Sarvex can grow to support any number of generations
// without this method changing again — only the servable set passed in
// (in production, the spec registry's) grows.
func (s *NegotiationService) negotiatedVersionFor(inst ucp.StoreProvider, platformProfile *wire.Profile, servable []wire.Version) (wire.Version, error) {
	platformVer := platformProfile.UCP.Version

	if len(servable) == 0 {
		businessVer := wire.Version(inst.Version())
		if platformVer > businessVer {
			return "", &ucp.VersionUnsupportedError{
				PlatformVersion: string(platformVer),
				BusinessVersion: inst.Version(),
			}
		}
		return platformVer, nil
	}

	chosen, ok := SelectVersion(servable, platformVer)
	if !ok {
		return "", &ucp.VersionUnsupportedError{
			PlatformVersion: string(platformVer),
			BusinessVersion: inst.Version(),
		}
	}
	return chosen, nil
}

// ValidateVersion returns an error if no mutual protocol version exists
// between inst and platformProfile, selecting from Config.ServableVersions
// — see negotiatedVersionFor for the selection rule. Exported for callers
// that only need the pass/fail check; Negotiate calls negotiatedVersionFor
// directly since it also needs the selected version to stamp into the
// response.
func (s *NegotiationService) ValidateVersion(inst ucp.StoreProvider, platformProfile *wire.Profile) error {
	_, err := s.negotiatedVersion(inst, platformProfile)
	return err
}

// NegotiationEvent is a domain event produced by Negotiate that the caller
// should publish. Keeping event creation inside the service and publication
// outside makes the core logic testable without an event bus.
type NegotiationEvent interface {
	eventbus.DomainEvent
}

// Negotiate orchestrates the full UCP negotiation flow for a single request,
// selecting from Config.ServableVersions. It returns the negotiation
// result, the fetched platform profile, a slice of pending domain events
// (never nil), and any error. The caller is responsible for publishing the
// returned events.
//
// headerVersion is the UCP-Agent header's own version= member (optional,
// variadic so every existing caller compiles unchanged) — see negotiate's
// isDereferenceableURL fallback for the one case it's used.
func (s *NegotiationService) Negotiate(ctx context.Context, inst ucp.StoreProvider, platformProfileURI string, headerVersion ...string) (*NegotiationResult, *wire.Profile, []NegotiationEvent, error) {
	return s.negotiate(ctx, inst, platformProfileURI, "default", s.config.ServableVersions, firstOf(headerVersion))
}

// NegotiateCheckout is Negotiate restricted to Config.CheckoutServableVersions
// instead of Config.ServableVersions. Checkout is the one capability domain
// allowed to trail ServableVersions: its response-building for a new
// generation (the wireadapt codec) may land after catalog's, and unlike
// catalog reads, checkout has real side effects (a live merchant call, payment credentials, order
// creation), so getting its generation wrong is not something a follow-up
// response can quietly correct (sarvex-v1-architecture.md §D R3, R1).
// Falls back to Config.ServableVersions when CheckoutServableVersions is
// unset, so a caller that never configures it sees Negotiate's behavior
// exactly — this is an additional restriction, not a separate feature.
func (s *NegotiationService) NegotiateCheckout(ctx context.Context, inst ucp.StoreProvider, platformProfileURI string, headerVersion ...string) (*NegotiationResult, *wire.Profile, []NegotiationEvent, error) {
	servable := s.config.CheckoutServableVersions
	if servable == nil {
		servable = s.config.ServableVersions
	}
	return s.negotiate(ctx, inst, platformProfileURI, "checkout", servable, firstOf(headerVersion))
}

// firstOf returns vs[0], or "" if vs is empty — the accessor for
// Negotiate/NegotiateCheckout's variadic headerVersion parameter.
func firstOf(vs []string) string {
	if len(vs) == 0 {
		return ""
	}
	return vs[0]
}

// negotiate is Negotiate/NegotiateCheckout's shared implementation.
// opTag distinguishes their cache entries (CacheKey alone is instance ×
// platform profile URI, with no notion of which servable set produced the
// cached result — two operations negotiating the same store against the
// same platform profile must not share a cache entry when they select
// from different servable sets, or the second to run would silently reuse
// the first's version decision).
func (s *NegotiationService) negotiate(ctx context.Context, inst ucp.StoreProvider, platformProfileURI, opTag string, servable []wire.Version, headerVersion string) (*NegotiationResult, *wire.Profile, []NegotiationEvent, error) {
	merchantID := inst.Configuration().MerchantID
	storeID := inst.StoreID()

	// 1. Composite cache key: merchant × instance × platform profile URI × operation
	key := CacheKey(merchantID, storeID, platformProfileURI) + ":" + opTag

	// 2. Fetch platform profile — unless the platform didn't supply a
	// dereferenceable profile reference at all (no scheme/host, e.g. the
	// literal "..." placeholder), in which case there is nothing to fetch:
	// fall back to negotiating on the UCP-Agent header's own version=
	// member alone. This is narrower than "profile fetch failed" — a
	// syntactically-absolute-but-unreachable URL (DNS failure, connection
	// refused, timeout, non-2xx) still goes through the normal fetch below
	// and still reports profile_unreachable exactly as before; only a
	// profile reference that could never be dereferenced takes this path.
	// See negotiateVersionOnly's doc comment for what this is for and what
	// it deliberately does not validate.
	if !isDereferenceableURL(platformProfileURI) && headerVersion != "" {
		result, events, err := s.negotiateVersionOnly(inst, headerVersion, platformProfileURI, servable)
		return result, nil, events, err
	}

	platformProfile, err := s.FetchPlatformProfile(ctx, platformProfileURI)
	if err != nil {
		return nil, nil, nil, err
	}

	// 3. Return cached result if available (no new events — already published)
	if cached, ok := s.cache.GetNegotiation(key); ok {
		return cached, platformProfile, nil, nil
	}

	// 4. Validate namespace binding; wrap as ProfileMalformedError per spec.
	if err := s.ValidateNamespaceBinding(platformProfile); err != nil {
		var nsErr *ucp.NamespaceValidationError
		if errors.As(err, &nsErr) {
			return nil, platformProfile, nil, &ucp.ProfileMalformedError{Cause: err.Error()}
		}
		return nil, platformProfile, nil, err
	}

	// 5. Build business profile and the requested/declared capability sets
	// (CapabilitySets) up front — before version validation, not after —
	// so every failure path from here on can report what the platform
	// asked for and what the business declares, not just the failure
	// reason string. This is a pure, no-I/O read of the already-loaded
	// inst, so moving it ahead of ValidateVersion changes no observable
	// behavior or error ordering (see the Agent Behavior Insights emission
	// gap writeup for why this data didn't exist before).
	businessProfile := ucp.BusinessProfileFromStoreProfile(inst)
	sets := CapabilitySets{
		RequestedCapabilities:    capabilityNames(platformProfile.UCP.Capabilities),
		DeclaredCapabilities:     capabilityNames(businessProfile.UCP.Capabilities),
		RequestedPaymentHandlers: paymentHandlerNames(platformProfile.UCP.PaymentHandlers),
		DeclaredPaymentHandlers:  paymentHandlerNames(businessProfile.UCP.PaymentHandlers),
	}

	// 6. Validate version and select the protocol version to respond in.
	protocolVersion, err := s.negotiatedVersionFor(inst, platformProfile, servable)
	if err != nil {
		ev := NewNegotiationFailedEvent(merchantID, storeID, platformProfileURI, "version_unsupported", sets)
		return nil, platformProfile, []NegotiationEvent{ev}, err
	}

	// 7. Intersect capabilities and payment handlers
	caps := IntersectCapabilities(businessProfile.UCP.Capabilities, platformProfile.UCP.Capabilities)

	// 8. Empty intersection means no compatible capabilities — not a cache-worthy result.
	if len(caps) == 0 {
		ev := NewNegotiationFailedEvent(merchantID, storeID, platformProfileURI, "capabilities_incompatible", sets)
		return nil, platformProfile, []NegotiationEvent{ev}, &ucp.CapabilitiesIncompatibleError{BusinessVersion: inst.Version()}
	}

	handlers := IntersectPaymentHandlers(businessProfile.UCP.PaymentHandlers, platformProfile.UCP.PaymentHandlers)

	// 9. Assemble result
	capRefs := make(map[string][]CapabilityRef, len(caps))
	for name, capList := range caps {
		refs := make([]CapabilityRef, len(capList))
		for i, c := range capList {
			refs[i] = CapabilityRef{Version: string(c.Version)}
		}
		capRefs[name] = refs
	}

	handlerRefs := make(map[string][]HandlerRef, len(handlers))
	for name, handlerList := range handlers {
		refs := make([]HandlerRef, len(handlerList))
		for i, h := range handlerList {
			refs[i] = HandlerRef{ID: h.ID, Version: string(h.Version)}
		}
		handlerRefs[name] = refs
	}

	result := &NegotiationResult{
		Version:         string(protocolVersion),
		Capabilities:    capRefs,
		PaymentHandlers: handlerRefs,
	}

	// 10. Store in cache bounded by remaining platform profile TTL
	profileExpiry := s.cache.GetPlatformProfileExpiry(platformProfileURI)
	negotiationTTL := s.config.NegotiationCacheTTL
	if profileExpiry > 0 && profileExpiry < negotiationTTL {
		negotiationTTL = profileExpiry
	}
	s.cache.SetNegotiation(key, result, negotiationTTL)

	// 11. Return a succeeded event for the caller to publish
	activeCapabilities := make([]string, 0, len(result.Capabilities))
	for capName := range result.Capabilities {
		activeCapabilities = append(activeCapabilities, capName)
	}
	sort.Strings(activeCapabilities)
	sets.ActiveCapabilities = activeCapabilities
	sets.ActivePaymentHandlers = paymentHandlerNames(handlers)
	ev := NewNegotiationSucceededEvent(merchantID, storeID, platformProfileURI, sets)

	return result, platformProfile, []NegotiationEvent{ev}, nil
}

// isDereferenceableURL reports whether raw parses as an absolute URL with a
// scheme and host — i.e. something FetchPlatformProfile could actually
// issue a request against. A syntactically-absolute-but-unreachable URL
// (wrong host, connection refused, ...) still returns true here; that
// belongs to FetchPlatformProfile's own fetch failure (profile_unreachable),
// not this check. Empty/malformed input, a bare path, or a placeholder
// value like "..." (RFC 3986 permits it as a relative reference — it parses
// without error, just with no scheme or host) returns false.
func isDereferenceableURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return u.Scheme != "" && u.Host != ""
}

// negotiateVersionOnly is negotiate's fallback for a UCP-Agent header whose
// profile= member is not a dereferenceable URL but does carry a version=
// member (see negotiate's isDereferenceableURL check). This exists for the
// official UCP conformance suite's ProtocolTest.test_version_negotiation,
// which deliberately sends UCP-Agent: profile="..."; version="<value>" to
// exercise version compatibility in isolation from profile fetching — every
// other conformance test supplies a real profile URL served by a local mock
// agent. Real platforms (Google's UCP-powered shopping agent) always send a
// genuine profile URL, so in production this path should never be taken;
// FetchPlatformProfile and everything downstream of it in negotiate runs
// exactly as before whenever the profile reference is dereferenceable.
//
// With no platform profile to fetch, there is nothing to run
// ValidateNamespaceBinding or capability/payment-handler intersection
// against, so this negotiates purely on whether headerVersion is servable
// and reports the business's own full capability/payment-handler set
// verbatim: a claim about what the business itself supports, never a claim
// about what the platform supports or was told to intersect against. The
// result is never cached (there is no fetched profile / cache TTL to bound
// it by, and the key space here is a placeholder profile URI, not a real
// one) — every call recomputes.
//
// headerVersion is checked in two steps, not negotiatedVersionFor's single
// ceiling rule applied directly:
//
//  1. versionIsKnown: headerVersion must not be newer than the newest
//     generation Sarvex has registered anywhere (s.config.ServableVersions,
//     the full cross-capability registry — the spec registry in production,
//     the same set discovery's own ucp.version is drawn from). This is what
//     rejects an obviously-bogus future date like "2099-01-01" outright —
//     under negotiatedVersionFor's ceiling rule alone it would be accepted
//     as "compatible" (every real servable version is older than it), which
//     is exactly backwards; the conformance suite's own
//     test_version_negotiation asserts that exact case must be rejected
//     (422).
//  2. negotiatedVersionFor, exactly as the fetched-profile path uses it,
//     picks the highest version in the narrower servable set (e.g.
//     CheckoutServableVersions, which can legitimately lag the full
//     registry while a newer generation's checkout rendering is built, even
//     where discovery/catalog have moved to it, see
//     capabilityVersionEntries in profile_renderer.go) that does not exceed
//     headerVersion. This is what lets a platform whose header names
//     Sarvex's newest known generation still get served at whatever older
//     version this specific operation can actually render — the same
//     "declare the ceiling, serve the best mutually supported version"
//     behavior negotiatedVersionFor already provides fetched-profile
//     callers.
func (s *NegotiationService) negotiateVersionOnly(
	inst ucp.StoreProvider,
	headerVersion string,
	platformProfileURI string,
	servable []wire.Version,
) (*NegotiationResult, []NegotiationEvent, error) {
	merchantID := inst.Configuration().MerchantID
	storeID := inst.StoreID()
	businessProfile := ucp.BusinessProfileFromStoreProfile(inst)
	sets := CapabilitySets{
		DeclaredCapabilities:    capabilityNames(businessProfile.UCP.Capabilities),
		DeclaredPaymentHandlers: paymentHandlerNames(businessProfile.UCP.PaymentHandlers),
	}

	versionUnsupported := func() (*NegotiationResult, []NegotiationEvent, error) {
		ev := NewNegotiationFailedEvent(merchantID, storeID, platformProfileURI, "version_unsupported", sets)
		return nil, []NegotiationEvent{ev}, &ucp.VersionUnsupportedError{
			PlatformVersion: headerVersion,
			BusinessVersion: inst.Version(),
		}
	}

	if !versionIsKnown(headerVersion, s.config.ServableVersions, inst) {
		return versionUnsupported()
	}

	synthetic := &wire.Profile{UCP: wire.UCP{Version: wire.Version(headerVersion)}}
	protocolVersion, err := s.negotiatedVersionFor(inst, synthetic, servable)
	if err != nil {
		return versionUnsupported()
	}

	capRefs := make(map[string][]CapabilityRef, len(businessProfile.UCP.Capabilities))
	for name, capList := range businessProfile.UCP.Capabilities {
		refs := make([]CapabilityRef, len(capList))
		for i, c := range capList {
			refs[i] = CapabilityRef{Version: string(c.Version)}
		}
		capRefs[name] = refs
	}
	handlerRefs := make(map[string][]HandlerRef, len(businessProfile.UCP.PaymentHandlers))
	for name, handlerList := range businessProfile.UCP.PaymentHandlers {
		refs := make([]HandlerRef, len(handlerList))
		for i, h := range handlerList {
			refs[i] = HandlerRef{ID: h.ID, Version: string(h.Version)}
		}
		handlerRefs[name] = refs
	}

	result := &NegotiationResult{
		Version:         string(protocolVersion),
		Capabilities:    capRefs,
		PaymentHandlers: handlerRefs,
	}

	sets.ActiveCapabilities = sets.DeclaredCapabilities
	sets.ActivePaymentHandlers = sets.DeclaredPaymentHandlers
	ev := NewNegotiationSucceededEvent(merchantID, storeID, platformProfileURI, sets)

	return result, []NegotiationEvent{ev}, nil
}

// versionIsKnown reports whether headerVersion does not exceed the newest
// generation in allServable (falling back to inst.Version() — the same
// single-candidate default negotiatedVersionFor uses when the caller never
// configured ServableVersions). See negotiateVersionOnly's doc comment for
// why this full-registry ceiling check runs before, and separately from,
// negotiatedVersionFor's own per-operation ceiling selection.
func versionIsKnown(headerVersion string, allServable []wire.Version, inst ucp.StoreProvider) bool {
	known := allServable
	if len(known) == 0 {
		known = []wire.Version{wire.Version(inst.Version())}
	}
	newest := known[0]
	for _, v := range known[1:] {
		if v > newest {
			newest = v
		}
	}
	return wire.Version(headerVersion) <= newest
}

// PublishNegotiationEvents publishes the events returned by Negotiate.
// Errors are logged but never returned — event publishing is fire-and-forget.
func (s *NegotiationService) PublishNegotiationEvents(ctx context.Context, publisher eventbus.EventBus, events []NegotiationEvent) {
	if publisher == nil {
		return
	}
	for _, ev := range events {
		_ = publisher.Publish(ctx, ev)
	}
}

// capabilityNames returns the sorted capability names (map keys) declared
// in a profile's capability set — the CapabilitySets requested/declared
// fields, e.g. R8's capability-gap computation, key on these names, not on
// the version-specific wire.Capability values.
func capabilityNames(m map[string][]wire.Capability) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// paymentHandlerNames returns the sorted payment-handler names (map keys)
// declared in a profile's payment-handler set.
func paymentHandlerNames(m map[string][]wire.PaymentHandler) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// capabilityOrigin derives the expected HTTPS origin from a reverse-domain capability name.
// "dev.ucp.shopping.checkout" -> "https://ucp.dev"
// "com.example.payment"       -> "https://example.com"
func capabilityOrigin(capName string) string {
	parts := strings.SplitN(capName, ".", 3)
	if len(parts) < 2 {
		return ""
	}
	return "https://" + parts[1] + "." + parts[0]
}

// parseCacheTTL derives a cache TTL from HTTP response headers.
// Falls back to the provided default if no usable headers are present.
func parseCacheTTL(header http.Header, fallback time.Duration) time.Duration {
	if cc := header.Get("Cache-Control"); cc != "" {
		for directive := range strings.SplitSeq(cc, ",") {
			directive = strings.TrimSpace(directive)
			if after, ok := strings.CutPrefix(directive, "max-age="); ok {
				if n, err := strconv.Atoi(after); err == nil && n > 0 {
					return time.Duration(n) * time.Second
				}
			}
		}
	}
	if expires := header.Get("Expires"); expires != "" {
		if t, err := http.ParseTime(expires); err == nil {
			if remaining := time.Until(t); remaining > 0 {
				return remaining
			}
		}
	}
	return fallback
}

// isVersionUnsupported reports whether err is a VersionUnsupportedError.
func isVersionUnsupported(err error) bool {
	var e *ucp.VersionUnsupportedError
	return errors.As(err, &e)
}
