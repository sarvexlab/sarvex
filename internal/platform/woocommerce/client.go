// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package woocommerce

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// httpClient is scoped to a single merchant's WooCommerce store.
// All REST API v3 calls are authenticated with HTTP Basic auth
// (Consumer Key + Consumer Secret base64-encoded).
type httpClient struct {
	cfg        MerchantConfig
	underlying *http.Client
	logger     *slog.Logger
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

// restURLForConfig builds the REST API URL for path under cfg's store base
// (or override base, for testing). Shared by httpClient and the store-API
// pull client so both build URLs identically without duplicating logic.
func restURLForConfig(cfg MerchantConfig, path string) string {
	if cfg.APIOverrideBase != "" {
		return fmt.Sprintf("%s/wp-json/wc/%s%s", cfg.APIOverrideBase, cfg.APIVersion, path)
	}
	return fmt.Sprintf("%s/wp-json/wc/%s%s", cfg.StoreBaseURL, cfg.APIVersion, path)
}

// setAuthHeadersForConfig sets HTTP Basic auth (Consumer Key + Consumer
// Secret) on req per cfg. Shared by httpClient and the store-API pull client.
func setAuthHeadersForConfig(req *http.Request, cfg MerchantConfig) {
	auth := base64.StdEncoding.EncodeToString([]byte(cfg.ConsumerKey + ":" + cfg.ConsumerSecret))
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Accept", "application/json")
}

func (c *httpClient) restURL(path string) string {
	return restURLForConfig(c.cfg, path)
}

// ─── Public request methods ───────────────────────────────────────────────────

// GET executes an authenticated GET and decodes the response.
func (c *httpClient) GET(ctx context.Context, path string, out any) error {
	return c.doWithRetry(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.restURL(path), nil)
		if err != nil {
			return nil, err
		}
		c.setAuthHeaders(req)
		return req, nil
	}, jsonDecodeHandler(out))
}

// POST executes an authenticated POST.
func (c *httpClient) POST(ctx context.Context, path string, body any, out any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("woocommerce: marshal request: %w", err)
	}
	return c.doWithRetry(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.restURL(path), bytes.NewReader(encoded))
		if err != nil {
			return nil, err
		}
		c.setAuthHeaders(req)
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	}, jsonDecodeHandler(out))
}

// PUT executes an authenticated PUT.
func (c *httpClient) PUT(ctx context.Context, path string, body any, out any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("woocommerce: marshal request: %w", err)
	}
	return c.doWithRetry(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.restURL(path), bytes.NewReader(encoded))
		if err != nil {
			return nil, err
		}
		c.setAuthHeaders(req)
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	}, jsonDecodeHandler(out))
}

// POSTNoRetry executes an authenticated POST without retries (for order creation).
func (c *httpClient) POSTNoRetry(ctx context.Context, path string, body any, out any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("woocommerce: marshal request: %w", err)
	}
	return c.doWithRetryEx(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.restURL(path), bytes.NewReader(encoded))
		if err != nil {
			return nil, err
		}
		c.setAuthHeaders(req)
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	}, jsonDecodeHandler(out), true)
}

// PUTNoRetry executes an authenticated PUT without retries (for order status updates).
func (c *httpClient) PUTNoRetry(ctx context.Context, path string, body any, out any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("woocommerce: marshal request: %w", err)
	}
	return c.doWithRetryEx(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.restURL(path), bytes.NewReader(encoded))
		if err != nil {
			return nil, err
		}
		c.setAuthHeaders(req)
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	}, jsonDecodeHandler(out), true)
}

// DELETE executes an authenticated DELETE.
func (c *httpClient) DELETE(ctx context.Context, path string) error {
	return c.doWithRetry(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.restURL(path), nil)
		if err != nil {
			return nil, err
		}
		c.setAuthHeaders(req)
		return req, nil
	}, func(_ []byte, _ int) error { return nil })
}

// ─── Authentication ───────────────────────────────────────────────────────────

func (c *httpClient) setAuthHeaders(req *http.Request) {
	setAuthHeadersForConfig(req, c.cfg)
}

// ─── Retry loop ───────────────────────────────────────────────────────────────

type buildReqFn func() (*http.Request, error)
type handleRespFn func(body []byte, statusCode int) error

// doWithRetry executes an HTTP request with retry logic (except when noRetry=true).
// Mutations like CheckoutCreate/CheckoutComplete should use noRetry=true to prevent
// accidental duplicates on retried responses without an idempotency key in the backend.
// Catalog operations (search, lookup) use noRetry=false (the default).
func (c *httpClient) doWithRetry(ctx context.Context, buildReq buildReqFn, handle handleRespFn) error {
	return c.doWithRetryEx(ctx, buildReq, handle, false)
}

// doWithRetryEx is the internal version that accepts a noRetry flag.
func (c *httpClient) doWithRetryEx(ctx context.Context, buildReq buildReqFn, handle handleRespFn, noRetry bool) error {
	tracer := otel.Tracer("github.com/sarvexlab/sarvex/woocommerce")
	var lastErr error
	backoff := 250 * time.Millisecond

	maxAttempts := 1
	if !noRetry {
		maxAttempts = c.cfg.MaxRetries + 1
	}

	for attempt := range maxAttempts {
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
			return fmt.Errorf("woocommerce: build request: %w", err)
		}

		// Create a span for this HTTP call attempt
		callCtx, span := tracer.Start(ctx, "woocommerce.http_call",
			trace.WithAttributes(
				attribute.String("http.method", req.Method),
				attribute.String("http.url", req.URL.String()),
				attribute.String("gateway.platform", "woocommerce"),
				attribute.Int("http.attempt", attempt+1),
			),
		)

		resp, err := c.underlying.Do(req)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			span.End()
			lastErr = fmt.Errorf("woocommerce: http transport error: %w", err)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			span.End()
			lastErr = fmt.Errorf("woocommerce: read response body: %w", err)
			continue
		}

		span.SetAttributes(
			attribute.Int("http.status_code", resp.StatusCode),
			attribute.Int("http.response_content_length", len(body)),
		)

		c.logger.DebugContext(callCtx, "woocommerce api call",
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

	return fmt.Errorf("woocommerce: all %d attempts failed, last error: %w", c.cfg.MaxRetries+1, lastErr)
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

// priceToCents converts a price string like "29.99" to cents (2999).
func priceToCents(priceStr string) int64 {
	var price float64
	_, _ = fmt.Sscanf(priceStr, "%f", &price)
	// Round to the nearest cent rather than truncate: float parsing makes a
	// value like "4.39" land at 438.9999…, and int64(x*100) would then drop a
	// cent (438 instead of 439), desyncing a discounted total from its discount
	// entry. math.Round makes the round-trip exact for money.
	return int64(math.Round(price * 100))
}
