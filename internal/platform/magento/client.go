// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package magento

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// httpClient is scoped to a single merchant's Magento store.
// It manages admin token caching and all REST API calls.
type httpClient struct {
	cfg        MerchantConfig
	underlying *http.Client
	logger     *slog.Logger

	// Token caching
	mu          sync.RWMutex
	token       string
	tokenExpiry time.Time
}

func newHTTPClient(cfg MerchantConfig, logger *slog.Logger) *httpClient {
	return &httpClient{
		cfg:    cfg,
		logger: logger,
		underlying: &http.Client{
			Timeout: cfg.RequestTimeout,
		},
	}
}

// ─── URL builders ─────────────────────────────────────────────────────────────

func (c *httpClient) restURL(path string) string {
	if c.cfg.APIOverrideBase != "" {
		return fmt.Sprintf("%s/rest/%s/V1%s", c.cfg.APIOverrideBase, c.cfg.StoreCode, path)
	}
	return fmt.Sprintf("%s/rest/%s/V1%s", c.cfg.BaseURL, c.cfg.StoreCode, path)
}

// ─── Token management ────────────────────────────────────────────────────────

// ensureToken refreshes the admin token if it's missing or expired.
func (c *httpClient) ensureToken(ctx context.Context) error {
	c.mu.RLock()
	if c.token != "" && time.Now().Before(c.tokenExpiry.Add(-c.cfg.TokenTTLBuffer)) {
		c.mu.RUnlock()
		return nil
	}
	c.mu.RUnlock()

	// Token is missing or expired; refresh it.
	return c.refreshToken(ctx)
}

// refreshToken obtains a new admin token via POST /rest/all/V1/integration/admin/token
func (c *httpClient) refreshToken(ctx context.Context) error {
	url := c.cfg.BaseURL + "/rest/all/V1/integration/admin/token"
	if c.cfg.APIOverrideBase != "" {
		url = c.cfg.APIOverrideBase + "/rest/all/V1/integration/admin/token"
	}

	payload := map[string]string{
		"username": c.cfg.AdminUser,
		"password": c.cfg.AdminPassword,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("magento: marshal token request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("magento: create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.underlying.Do(req)
	if err != nil {
		return fmt.Errorf("magento: token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("magento: read token response: %w", err)
	}

	if err := classifyHTTPError(resp.StatusCode, string(body)); err != nil {
		return fmt.Errorf("%w: %v", ErrTokenRefresh, err)
	}

	// Magento returns a bare quoted string, not a JSON object
	var token string
	if err := json.Unmarshal(body, &token); err != nil {
		return fmt.Errorf("magento: unmarshal token response: %w", err)
	}

	if token == "" {
		return fmt.Errorf("%w: empty token in response", ErrTokenRefresh)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = token
	c.tokenExpiry = time.Now().Add(c.cfg.TokenTTL)

	c.logger.DebugContext(ctx, "magento admin token refreshed",
		"merchant_id", c.cfg.MerchantID,
		"expiry", c.tokenExpiry,
	)

	return nil
}

// ─── Public request methods ───────────────────────────────────────────────────

// GET executes an authenticated GET and decodes the response.
func (c *httpClient) GET(ctx context.Context, path string, out any) error {
	return c.doWithRetry(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.restURL(path), nil)
		if err != nil {
			return nil, err
		}
		if err := c.setAuthHeaders(ctx, req); err != nil {
			return nil, err
		}
		return req, nil
	}, jsonDecodeHandler(out))
}

// POST executes an authenticated POST.
func (c *httpClient) POST(ctx context.Context, path string, body any, out any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("magento: marshal request: %w", err)
	}
	return c.doWithRetry(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.restURL(path), bytes.NewReader(encoded))
		if err != nil {
			return nil, err
		}
		if err := c.setAuthHeaders(ctx, req); err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	}, jsonDecodeHandler(out))
}

// POSTNoRetry is POST with a single attempt, for creating things that a replay
// would duplicate (a cart, an order).
func (c *httpClient) POSTNoRetry(ctx context.Context, path string, body any, out any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("magento: marshal request: %w", err)
	}
	return c.doWithRetryN(ctx, 0, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.restURL(path), bytes.NewReader(encoded))
		if err != nil {
			return nil, err
		}
		if err := c.setAuthHeaders(ctx, req); err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	}, jsonDecodeHandler(out))
}

// PUT executes an authenticated PUT.
func (c *httpClient) PUT(ctx context.Context, path string, body any, out any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("magento: marshal request: %w", err)
	}
	return c.doWithRetry(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.restURL(path), bytes.NewReader(encoded))
		if err != nil {
			return nil, err
		}
		if err := c.setAuthHeaders(ctx, req); err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	}, jsonDecodeHandler(out))
}

// DELETE executes an authenticated DELETE.
func (c *httpClient) DELETE(ctx context.Context, path string) error {
	return c.doWithRetry(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.restURL(path), nil)
		if err != nil {
			return nil, err
		}
		if err := c.setAuthHeaders(ctx, req); err != nil {
			return nil, err
		}
		return req, nil
	}, func(_ []byte, _ int) error { return nil })
}

// ─── Authentication ───────────────────────────────────────────────────────────

func (c *httpClient) setAuthHeaders(ctx context.Context, req *http.Request) error {
	if err := c.ensureToken(ctx); err != nil {
		return err
	}
	c.mu.RLock()
	token := c.token
	c.mu.RUnlock()
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	return nil
}

// ─── Retry loop ───────────────────────────────────────────────────────────────

type buildReqFn func() (*http.Request, error)
type handleRespFn func(body []byte, statusCode int) error

func (c *httpClient) doWithRetry(ctx context.Context, buildReq buildReqFn, handle handleRespFn) error {
	return c.doWithRetryN(ctx, c.cfg.MaxRetries, buildReq, handle)
}

// doWithRetryN is doWithRetry with an explicit retry budget; 0 sends exactly
// one attempt, for writes that are not idempotent at the store (placing an
// order) and so must never be replayed after a lost response.
func (c *httpClient) doWithRetryN(ctx context.Context, maxRetries int, buildReq buildReqFn, handle handleRespFn) error {
	tracer := otel.Tracer("github.com/sarvexlabs/sarvex.cp/magento")
	var lastErr error
	backoff := 250 * time.Millisecond

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
				backoff *= 2
			}
		}

		req, err := buildReq()
		if err != nil {
			return fmt.Errorf("magento: build request: %w", err)
		}

		// Create a span for this HTTP call attempt
		callCtx, span := tracer.Start(ctx, "magento.http_call",
			trace.WithAttributes(
				attribute.String("http.method", req.Method),
				attribute.String("http.url", req.URL.String()),
				attribute.String("gateway.platform", "magento"),
				attribute.Int("http.attempt", attempt+1),
			),
		)

		resp, err := c.underlying.Do(req)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			span.End()
			lastErr = fmt.Errorf("magento: http transport error: %w", err)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			span.End()
			lastErr = fmt.Errorf("magento: read response body: %w", err)
			continue
		}

		span.SetAttributes(
			attribute.Int("http.status_code", resp.StatusCode),
			attribute.Int("http.response_content_length", len(body)),
		)

		c.logger.DebugContext(callCtx, "magento api call",
			"merchant_id", c.cfg.MerchantID,
			"method", req.Method,
			"url", req.URL.String(),
			"status", resp.StatusCode,
		)

		if err := classifyHTTPError(resp.StatusCode, string(body)); err != nil {
			if apiErr, ok := err.(*APIError); ok && apiErr.IsRetryable() {
				span.SetStatus(codes.Error, apiErr.Error())
				span.RecordError(apiErr)
				span.End()
				lastErr = err
				continue
			}
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			span.End()
			return err
		}

		span.SetStatus(codes.Ok, "")
		span.End()
		return handle(body, resp.StatusCode)
	}

	if maxRetries == 0 && lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("magento: all %d attempts failed, last error: %w", maxRetries+1, lastErr)
}

// doOnce performs exactly one authenticated HTTP attempt against path — no
// retry, no backoff, no circuit breaking. It still shares this client's
// cached admin token (ensureToken/setAuthHeaders), so repeated pull calls
// for the same store don't re-authenticate on every attempt.
//
// This is used only by the connector's store-API pull path (connector.go's
// pullCall), which delegates all retry/circuit-breaking to resilience.Guard
// so a failing store's failure budget is counted once per logical call, not
// once per HTTP client retry nested inside a guard retry. The checkout/cart
// path continues to use GET/POST/PUT/DELETE above, which retry internally
// via doWithRetry.
func (c *httpClient) doOnce(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("magento: marshal request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.restURL(path), reader)
	if err != nil {
		return fmt.Errorf("magento: build request: %w", err)
	}
	if err := c.setAuthHeaders(ctx, req); err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.underlying.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", errTransport, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%w: read response body: %w", errTransport, err)
	}

	if err := classifyHTTPError(resp.StatusCode, string(respBody)); err != nil {
		return err
	}
	if out != nil && len(respBody) > 0 {
		return json.Unmarshal(respBody, out)
	}
	return nil
}

// jsonDecodeHandler returns a handleRespFn that JSON-decodes into out.
func jsonDecodeHandler(out any) handleRespFn {
	return func(body []byte, _ int) error {
		if out == nil || len(body) == 0 {
			return nil
		}
		return json.Unmarshal(body, out)
	}
}
