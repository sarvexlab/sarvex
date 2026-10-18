// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package client is a typed HTTP client for the control plane's
// console/operator REST API, scoped to exactly the endpoints cmd/sarvex
// drives.
//
// It calls only routes already registered in
// internal/planes/control/api/handler.go — the control-plane surface is
// exactly sarvex-v1-control-plane.yaml (CLAUDE.md §3), so this package
// invents nothing. Request and response shapes are declared here rather
// than imported from the control plane's domain package: this is an HTTP
// client and must not depend on a plane's Go types.
//
// Authentication is a dev bearer token minted in-process
// (internal/pkg/ucp/agent.DevBearerToken), accepted only when the control
// plane runs with KINDE_ISSUER_URL unset. merchant_id travels in the token's
// claims and is resolved server-side — never as a path or query parameter
// (CLAUDE.md §2.4).
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/agent"
)

// Client talks to one control plane as one principal. Get a merchant-scoped
// client from AsMerchant once the merchant id is known.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewOperator returns a client authenticated as Sarvex staff. It carries no
// merchant_id, which is what /operator/* requires and what every
// merchant-scoped route rejects — so this client can create a merchant and
// little else.
func NewOperator(baseURL string, timeout time.Duration) (*Client, error) {
	token, err := agent.DevBearerToken(agent.RoleOperator, "")
	if err != nil {
		return nil, err
	}
	return newClient(baseURL, token, timeout), nil
}

// NewMerchant returns a client authenticated as the owner of merchantID.
func NewMerchant(baseURL, merchantID string, timeout time.Duration) (*Client, error) {
	token, err := agent.DevBearerToken(agent.RoleOwner, merchantID)
	if err != nil {
		return nil, err
	}
	return newClient(baseURL, token, timeout), nil
}

// AsMerchant re-scopes an operator client to a merchant, keeping the same
// base URL and timeout — the step that follows creating a merchant.
func (c *Client) AsMerchant(merchantID string) (*Client, error) {
	token, err := agent.DevBearerToken(agent.RoleOwner, merchantID)
	if err != nil {
		return nil, err
	}
	return &Client{baseURL: c.baseURL, token: token, http: c.http}, nil
}

func newClient(baseURL, token string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: timeout},
	}
}

// ── Wire types ───────────────────────────────────────────────────────────

// Account is the merchant record POST /operator/merchants returns.
type Account struct {
	MerchantID    string        `json:"merchant_id"`
	Name          string        `json:"name"`
	Timezone      string        `json:"timezone"`
	CountryCode   string        `json:"country_code"`
	BusinessFacts BusinessFacts `json:"business_facts"`
}

// BusinessFacts are the merchant-level facts collected at onboarding.
type BusinessFacts struct {
	LegalEntityName    string   `json:"legal_entity_name"`
	SupportEmail       string   `json:"support_email"`
	SupportPhone       string   `json:"support_phone"`
	FulfillmentRegions []string `json:"fulfillment_regions"`
}

// Store is the store record POST /stores returns.
type Store struct {
	StoreID     string `json:"store_id"`
	MerchantID  string `json:"merchant_id"`
	DisplayName string `json:"display_name"`
	Platform    string `json:"platform"`
	ConnectorID string `json:"connector_id,omitempty"`
}

// StoreProfile is the business identity and fulfillment configuration bound
// into a store's published UCP profile.
type StoreProfile struct {
	LegalName          string   `json:"legal_name"`
	SupportEmail       string   `json:"support_email"`
	Domain             string   `json:"domain"`
	ReturnPolicyURL    string   `json:"return_policy_url"`
	FulfillmentRegions []string `json:"fulfillment_regions"`
}

// EnabledHandler is one payment handler's enablement state. Parameters holds
// the handler's public parameters only — secrets never travel on this API
// (sarvex-v1-control-plane.yaml payment_config_update).
type EnabledHandler struct {
	HandlerID  string            `json:"handler_id"`
	Enabled    bool              `json:"enabled"`
	Parameters map[string]string `json:"parameters,omitempty"`
}

// Pairing is what POST /stores/{id}/connectors/pairing returns.
type Pairing struct {
	ConnectorID string `json:"connector_id"`
	PairingCode string `json:"pairing_code"`
	StoreID     string `json:"store_id"`
}

// Activation is the store's own UCP lifecycle: draft → connector_linked →
// profile_published → verified → active.
type Activation struct {
	StoreID          string              `json:"store_id"`
	UCPState         string              `json:"ucp_state"`
	Capabilities     []string            `json:"capabilities"`
	ProfileURL       string              `json:"profile_url,omitempty"`
	LastVerification *VerificationReport `json:"last_verification,omitempty"`
	Steps            []ActivationStep    `json:"steps"`
}

// ActivationStep is one stage of the lifecycle and why the store has not
// cleared it yet.
type ActivationStep struct {
	State    string `json:"state"`
	Reached  bool   `json:"reached"`
	Blocking string `json:"blocking,omitempty"`
}

// VerificationReport is a verification run's outcome. A failing run is a
// successful request that found problems, not an error.
type VerificationReport struct {
	Passed   bool          `json:"passed"`
	State    string        `json:"ucp_state"`
	Checks   []CheckResult `json:"checks"`
	RanAtRaw string        `json:"ran_at"`
}

// CheckResult is one probe's outcome within a verification run.
type CheckResult struct {
	CheckID string `json:"check_id"`
	Passed  bool   `json:"passed"`
	Detail  string `json:"detail,omitempty"`
}

// Journey is the merchant-facing engagement lifecycle: prospect →
// connected → synced → ready → live. Independent of Activation.
type Journey struct {
	State          string     `json:"state"`
	Stage          string     `json:"stage"`
	StageName      string     `json:"stage_name"`
	OpenTaskCounts TaskCounts `json:"open_task_counts"`
}

// TaskCounts summarises a store's outstanding remediation work.
type TaskCounts struct {
	Total    int `json:"total"`
	Blocking int `json:"blocking"`
}

// Task is one remediation item from GET /tasks.
type Task struct {
	TaskID   string `json:"task_id"`
	StoreID  string `json:"store_id"`
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Blocking bool   `json:"blocking"`
}

// Readiness is a store's catalog readiness score.
type Readiness struct {
	Score          float64 `json:"score"`
	ProductCount   int     `json:"product_count"`
	ReadyCount     int     `json:"ready_count"`
	BlockingIssues int     `json:"blocking_issues"`
}

// listEnvelope is Sarvex's own list envelope (CLAUDE.md §3). Note the
// serving plane's UCP endpoints deliberately do *not* use it.
type listEnvelope[T any] struct {
	Data []T `json:"data"`
	Page struct {
		Cursor  string `json:"cursor"`
		HasMore bool   `json:"has_more"`
	} `json:"page"`
}

// ── Operations ───────────────────────────────────────────────────────────

// CreateMerchant creates a merchant account and invites its owner. Requires
// an operator-scoped client.
func (c *Client) CreateMerchant(ctx context.Context, name, ownerEmail string, facts BusinessFacts) (*Account, error) {
	body := map[string]any{"name": name, "owner_email": ownerEmail, "business_facts": facts}
	var out Account
	if err := c.do(ctx, http.MethodPost, "/operator/merchants", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateStore creates a store. platform must be woocommerce or magento —
// the control plane rejects anything else.
func (c *Client) CreateStore(ctx context.Context, displayName, platform string) (*Store, error) {
	body := map[string]any{"display_name": displayName, "platform": platform}
	var out Store
	if err := c.do(ctx, http.MethodPost, "/stores", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListStores lists the merchant's stores.
func (c *Client) ListStores(ctx context.Context) ([]Store, error) {
	var out listEnvelope[Store]
	if err := c.do(ctx, http.MethodGet, "/stores", nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// SetStoreProfile replaces a store's business identity and fulfillment
// configuration.
func (c *Client) SetStoreProfile(ctx context.Context, storeID string, p StoreProfile) error {
	return c.do(ctx, http.MethodPut, "/stores/"+storeID+"/profile", p, nil)
}

// SetPaymentConfig replaces which payment handlers a store has enabled.
func (c *Client) SetPaymentConfig(ctx context.Context, storeID string, handlers []EnabledHandler) error {
	body := map[string]any{"handlers": handlers}
	return c.do(ctx, http.MethodPut, "/stores/"+storeID+"/payment-config", body, nil)
}

// BeginPairing mints a connector and pairing code for a store. In dev mode
// the control plane's plugin simulator completes the pairing and triggers
// the first catalog import on its own, within a few seconds.
func (c *Client) BeginPairing(ctx context.Context, storeID, platform string) (*Pairing, error) {
	body := map[string]any{"platform": platform}
	var out Pairing
	if err := c.do(ctx, http.MethodPost, "/stores/"+storeID+"/connectors/pairing", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TriggerAudit re-scores a store's catalog, raising remediation tasks for
// whatever is not agent-ready.
func (c *Client) TriggerAudit(ctx context.Context, storeID string) error {
	return c.do(ctx, http.MethodPost, "/stores/"+storeID+"/catalog/audits", map[string]any{}, nil)
}

// GetActivation reads a store's UCP activation lifecycle state.
func (c *Client) GetActivation(ctx context.Context, storeID string) (*Activation, error) {
	var out Activation
	if err := c.do(ctx, http.MethodGet, "/stores/"+storeID+"/activation", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// VerifyActivation runs the store-level probe suite and applies the result.
// A failing verification returns a report with Passed false and no error —
// only a transport or authorization failure is an error here.
func (c *Client) VerifyActivation(ctx context.Context, storeID string) (*VerificationReport, error) {
	var out VerificationReport
	if err := c.do(ctx, http.MethodPost, "/stores/"+storeID+"/activation/verify", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetJourney reads a store's engagement journey state.
func (c *Client) GetJourney(ctx context.Context, storeID string) (*Journey, error) {
	var out Journey
	if err := c.do(ctx, http.MethodGet, "/stores/"+storeID+"/journey", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListTasks lists the merchant's open remediation tasks.
func (c *Client) ListTasks(ctx context.Context) ([]Task, error) {
	var out listEnvelope[Task]
	if err := c.do(ctx, http.MethodGet, "/tasks", nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// ResolveTask marks one remediation task resolved.
func (c *Client) ResolveTask(ctx context.Context, taskID string) error {
	return c.do(ctx, http.MethodPost, "/tasks/"+taskID+"/resolve", map[string]any{}, nil)
}

// ── Transport ────────────────────────────────────────────────────────────

// StatusError carries a non-2xx response so callers can branch on the code
// without parsing the message.
type StatusError struct {
	Method string
	Path   string
	Code   int
	Body   string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s %s: status %d: %s", e.Method, e.Path, e.Code, e.Body)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal %s %s body: %w", method, path, err)
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build %s %s: %w", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read %s %s response: %w", method, path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &StatusError{Method: method, Path: path, Code: resp.StatusCode, Body: strings.TrimSpace(string(respBody))}
	}

	if out == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode %s %s response: %w", method, path, err)
	}
	return nil
}
