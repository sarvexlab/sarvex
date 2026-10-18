// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package httpx

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/ucpnego/negotiate"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

// BusinessNegotiator is the transport-neutral business side of UCP.
//
// Construct one at startup with the business's own profile and a
// ProfileSource. Then plug it into your HTTP framework of choice — this
// package ships a net/http adapter (Middleware); the echox subpackage
// provides an Echo v4 adapter.
//
// The negotiator is safe for concurrent use.
type BusinessNegotiator struct {
	own         *wire.Profile
	source      ProfileSource
	continueURL func(*http.Request) string

	minimumPlatformVersion wire.Version
}

// BusinessOption configures a BusinessNegotiator.
type BusinessOption func(*BusinessNegotiator)

// WithContinueURL provides a function that returns the continue_url for
// failed negotiations. The function receives the underlying *http.Request
// (Echo and other frameworks expose this) so it can resolve
// context-appropriate URLs.
func WithContinueURL(fn func(*http.Request) string) BusinessOption {
	return func(b *BusinessNegotiator) { b.continueURL = fn }
}

// NewBusinessNegotiator constructs the negotiator. The own profile must
// validate before this returns.
func NewBusinessNegotiator(own *wire.Profile, source ProfileSource, opts ...BusinessOption) (*BusinessNegotiator, error) {
	if own == nil {
		return nil, errors.New("httpx: own profile required")
	}
	if err := own.Validate(); err != nil {
		return nil, fmt.Errorf("httpx: own profile invalid: %w", err)
	}
	if source == nil {
		return nil, errors.New("httpx: profile source required")
	}
	b := &BusinessNegotiator{own: own, source: source}
	for _, opt := range opts {
		opt(b)
	}
	return b, nil
}

// OwnProfile returns the business's own profile. Useful for adapters
// that need to serve /.well-known/ucp from the same negotiator instance.
func (b *BusinessNegotiator) OwnProfile() *wire.Profile { return b.own }

// Negotiate runs the full handshake against an incoming *http.Request.
// On success the returned Result is non-empty and FailureOutcome is nil.
// On failure FailureOutcome describes what response to write.
//
// This is the transport-neutral entry point — Echo, Chi, Fiber, and the
// stdlib adapter all call this and translate FailureOutcome to their
// native response API.
func (b *BusinessNegotiator) Negotiate(r *http.Request) (negotiate.Result, *FailureOutcome) {
	result, nerr := b.run(r)
	if nerr == nil {
		return result, nil
	}
	return result, &FailureOutcome{
		Code:            nerr.code,
		HTTPStatus:      httpStatusFor(nerr.code),
		Content:         nerr.content,
		ContinueURL:     b.continueFor(r),
		ProtocolVersion: b.own.UCP.Version,
	}
}

// FailureOutcome carries everything an adapter needs to write a
// negotiation failure response in its framework-native way. Adapters
// translate this to either a DiscoveryError body (for 4xx codes) or a
// NegotiationFailure body (for capabilities_incompatible on 200 OK).
type FailureOutcome struct {
	Code            ErrorCode
	HTTPStatus      int
	Content         string
	ContinueURL     string
	ProtocolVersion wire.Version
}

// IsNegotiationFailure reports whether this is a capabilities_incompatible
// outcome — these get HTTP 200 with ucp.status = "error" rather than 4xx.
func (f *FailureOutcome) IsNegotiationFailure() bool {
	return f.Code == CodeCapabilitiesIncompatible
}

// DiscoveryBody returns the JSON-serializable body for a 4xx discovery
// or version error.
func (f *FailureOutcome) DiscoveryBody() DiscoveryError {
	return DiscoveryError{
		Code:        f.Code,
		Content:     f.Content,
		ContinueURL: f.ContinueURL,
	}
}

// NegotiationBody returns the JSON-serializable body for a 200 OK
// capabilities_incompatible response.
func (f *FailureOutcome) NegotiationBody() NegotiationFailure {
	return NegotiationFailure{
		UCP: FailureUCP{
			Version:      f.ProtocolVersion,
			Status:       "error",
			Capabilities: map[string][]wire.Capability{},
		},
		Messages: []Message{{
			Type:     "error",
			Code:     string(f.Code),
			Content:  f.Content,
			Severity: "unrecoverable",
		}},
		ContinueURL: f.ContinueURL,
	}
}

func (b *BusinessNegotiator) continueFor(r *http.Request) string {
	if b.continueURL == nil {
		return ""
	}
	return b.continueURL(r)
}

// run does the actual negotiation work and returns a typed error on failure.
func (b *BusinessNegotiator) run(r *http.Request) (negotiate.Result, *negotiationError) {
	header := r.Header.Get("UCP-Agent")
	agent, err := ParseUCPAgent(header)
	if err != nil {
		return negotiate.Result{}, &negotiationError{code: CodeInvalidProfileURL, content: err.Error()}
	}

	platform, err := b.source.Get(r.Context(), agent.ProfileURL)
	if err != nil {
		code, ok := MapError(err)
		if !ok {
			code = CodeProfileUnreachable
		}
		return negotiate.Result{}, &negotiationError{code: code, content: err.Error()}
	}

	if b.minimumPlatformVersion != "" &&
		platform.UCP.Version.Compare(b.minimumPlatformVersion) < 0 {
		return negotiate.Result{}, &negotiationError{
			code:    CodeVersionUnsupported,
			content: fmt.Sprintf("platform protocol version %s older than minimum %s", platform.UCP.Version, b.minimumPlatformVersion),
		}
	}

	result := negotiate.Intersect(b.own, platform)
	if result.IsEmpty() {
		return result, &negotiationError{
			code:    CodeCapabilitiesIncompatible,
			content: "no mutually-supported capabilities",
		}
	}
	return result, nil
}

type negotiationError struct {
	code    ErrorCode
	content string
}

// --- net/http adapter ----------------------------------------------------

// Middleware returns net/http middleware that runs negotiation on every
// request and attaches the Result to the request context on success.
// On failure it writes the appropriate response and short-circuits.
//
// Echo users: prefer github.com/sarvex/ucpnego/httpx/echox.Middleware.
func (b *BusinessNegotiator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result, failure := b.Negotiate(r)
		if failure != nil {
			b.writeFailure(w, failure)
			return
		}
		next.ServeHTTP(w, r.WithContext(withResult(r.Context(), result)))
	})
}

func (b *BusinessNegotiator) writeFailure(w http.ResponseWriter, f *FailureOutcome) {
	if f.IsNegotiationFailure() {
		WriteNegotiationFailure(w, f.ProtocolVersion, f.Content, f.ContinueURL)
		return
	}
	WriteDiscoveryError(w, f.Code, f.Content, f.ContinueURL)
}

// --- Context plumbing ----------------------------------------------------

type ctxKey struct{}

// ContextKey is the key used in framework-native context bags (echo.Context,
// fiber.Ctx, etc.) to store and retrieve the negotiation Result. Adapters
// use this string so a single key works across the stdlib context and
// framework-specific bags.
const ContextKey = "ucp.negotiation.result"

// FromContext returns the negotiation result attached by Middleware
// (net/http) or echox.Middleware. The bool is false if no negotiation
// ran on this request.
func FromContext(ctx context.Context) (negotiate.Result, bool) {
	r, ok := ctx.Value(ctxKey{}).(negotiate.Result)
	return r, ok
}

// MustFromContext is FromContext but panics if no result is present.
// Use only in handlers guaranteed to run behind the middleware.
func MustFromContext(ctx context.Context) negotiate.Result {
	r, ok := FromContext(ctx)
	if !ok {
		panic("httpx: negotiation result missing from context")
	}
	return r
}

// WithResult attaches a Result to a context. Exported for adapter packages.
func WithResult(ctx context.Context, r negotiate.Result) context.Context {
	return withResult(ctx, r)
}

func withResult(ctx context.Context, r negotiate.Result) context.Context {
	return context.WithValue(ctx, ctxKey{}, r)
}
