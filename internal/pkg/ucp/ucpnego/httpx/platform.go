// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package httpx

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/wire"
)

// PlatformClient is the platform side of UCP negotiation. It advertises
// the platform profile URL on every outbound request via the UCP-Agent
// header, and exposes helpers for parsing the negotiated capability set
// from the response body.
//
// PlatformClient is safe for concurrent use.
type PlatformClient struct {
	profileURL string
	http       *http.Client

	// optional: validate spec/schema URLs against namespace authority
	// before sending requests. Defaults to on; switch off only for
	// integration testing against non-conforming endpoints.
	validateNamespaces bool

	// optional: pre-loaded profile, used to enable client-side
	// pre-negotiation hints (logs, metrics). Not required.
	ownProfile *wire.Profile
}

// PlatformOption configures a PlatformClient.
type PlatformOption func(*PlatformClient)

// WithPlatformHTTPClient overrides the inner http.Client used for
// outbound requests.
func WithPlatformHTTPClient(c *http.Client) PlatformOption {
	return func(p *PlatformClient) { p.http = c }
}

// WithOwnProfile attaches the platform's own profile so the client can
// validate intersection assumptions locally before sending.
func WithOwnProfile(p *wire.Profile) PlatformOption {
	return func(c *PlatformClient) { c.ownProfile = p }
}

// NewPlatformClient constructs a PlatformClient. The profileURL is the
// publicly fetchable URL of the platform's own profile, which will be
// advertised on every request.
func NewPlatformClient(profileURL string, opts ...PlatformOption) (*PlatformClient, error) {
	if err := validateProfileURL(profileURL); err != nil {
		return nil, fmt.Errorf("httpx: platform profile URL: %w", err)
	}
	c := &PlatformClient{
		profileURL:         profileURL,
		http:               http.DefaultClient,
		validateNamespaces: true,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Do attaches the UCP-Agent header to req and dispatches it through the
// underlying client. Caller-supplied UCP-Agent values are overwritten.
func (c *PlatformClient) Do(req *http.Request) (*http.Response, error) {
	agent, err := FormatUCPAgent(c.profileURL)
	if err != nil {
		return nil, err
	}
	req.Header.Set("UCP-Agent", agent)
	return c.http.Do(req)
}

// ResponseEnvelope is the minimal shape of a UCP response, used to read
// the negotiated capability set the business returned.
type ResponseEnvelope struct {
	UCP struct {
		Version         wire.Version                     `json:"version"`
		Status          string                           `json:"status,omitempty"`
		Capabilities    map[string][]wire.Capability     `json:"capabilities,omitempty"`
		PaymentHandlers map[string][]wire.PaymentHandler `json:"payment_handlers,omitempty"`
	} `json:"ucp"`
	Messages    []Message `json:"messages,omitempty"`
	ContinueURL string    `json:"continue_url,omitempty"`
}

// ErrNegotiationFailed reports that a 200 OK response carried
// ucp.status = "error" (capabilities_incompatible).
var ErrNegotiationFailed = errors.New("ucp: capabilities incompatible")
