// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package shopify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// httpClient is a reusable HTTP client scoped to a single merchant's Shopify
// store. It handles:
//   - Correct header injection (Storefront vs Admin token)
//   - Exponential backoff retries on 429 / 503 responses
//   - Structured error parsing for both REST and GraphQL responses
//   - Request/response logging via slog
type httpClient struct {
	cfg        MerchantConfig
	underlying *http.Client
	logger     *slog.Logger
	// scheme is the URL scheme every Shopify API call is built with. Always
	// https: Shopify serves both the Storefront and Admin APIs over TLS
	// only. It is a field rather than a literal so the mock server in tests
	// (httptest.NewTLSServer, likewise https) shares the same code path.
	scheme string
}

// defaultScheme is what every Shopify API URL is built with. Left unset,
// the fmt.Sprintf("%s://%s/…") templates below produced "://shop.example/…"
// — a URL with no scheme, which url.Parse rejects, so every Storefront and
// Admin call failed at request construction.
const defaultScheme = "https"

func newHTTPClient(cfg MerchantConfig, logger *slog.Logger) *httpClient {
	return &httpClient{
		cfg:    cfg,
		logger: logger,
		scheme: defaultScheme,
		underlying: &http.Client{
			Timeout: cfg.RequestTimeout,
		},
	}
}

// ─── Storefront GraphQL ───────────────────────────────────────────────────────

type gqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type gqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []GQLError      `json:"errors,omitempty"`
}

// StorefrontGQL executes a Shopify Storefront API GraphQL request.
// It handles retries and returns a parsed gqlResponse on success.
func (c *httpClient) StorefrontGQL(ctx context.Context, query string, variables map[string]any, out any) error {
	url := fmt.Sprintf("%s://%s/api/%s/graphql.json",
		c.scheme, c.cfg.ShopDomain, c.cfg.StorefrontAPIVersion)

	body, err := json.Marshal(gqlRequest{Query: query, Variables: variables})
	if err != nil {
		return fmt.Errorf("shopify: marshal gql request: %w", err)
	}

	return c.doWithRetry(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Shopify-Storefront-Access-Token", c.cfg.StorefrontAccessToken)
		return req, nil
	}, func(respBody []byte, statusCode int) error {
		var gqlResp gqlResponse
		if err := json.Unmarshal(respBody, &gqlResp); err != nil {
			return fmt.Errorf("shopify: decode gql response: %w", err)
		}
		if len(gqlResp.Errors) > 0 {
			return &APIError{StatusCode: statusCode, GQLErrors: gqlResp.Errors}
		}
		if out != nil {
			return json.Unmarshal(gqlResp.Data, out)
		}
		return nil
	})
}

// ─── Admin REST ───────────────────────────────────────────────────────────────

// AdminGET executes an Admin REST API GET request and decodes the response.
func (c *httpClient) AdminGET(ctx context.Context, path string, out any) error {
	url := fmt.Sprintf("%s://%s/admin/api/%s%s",
		c.scheme, c.cfg.ShopDomain, c.cfg.AdminAPIVersion, path)

	return c.doWithRetry(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("X-Shopify-Access-Token", c.cfg.AdminAccessToken)
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	}, func(respBody []byte, _ int) error {
		return json.Unmarshal(respBody, out)
	})
}

// AdminPOST executes an Admin REST API POST request.
func (c *httpClient) AdminPOST(ctx context.Context, path string, reqBody any, out any) error {
	return c.adminWrite(ctx, http.MethodPost, path, reqBody, out)
}

// AdminPUT executes an Admin REST API PUT request.
func (c *httpClient) AdminPUT(ctx context.Context, path string, reqBody any, out any) error {
	return c.adminWrite(ctx, http.MethodPut, path, reqBody, out)
}

// AdminDELETE executes an Admin REST API DELETE request.
func (c *httpClient) AdminDELETE(ctx context.Context, path string) error {
	url := fmt.Sprintf("%s://%s/admin/api/%s%s",
		c.scheme, c.cfg.ShopDomain, c.cfg.AdminAPIVersion, path)

	return c.doWithRetry(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("X-Shopify-Access-Token", c.cfg.AdminAccessToken)
		return req, nil
	}, func(_ []byte, _ int) error { return nil })
}

func (c *httpClient) adminWrite(ctx context.Context, method, path string, reqBody any, out any) error {
	url := fmt.Sprintf("%s://%s/admin/api/%s%s",
		c.scheme, c.cfg.ShopDomain, c.cfg.AdminAPIVersion, path)

	encoded, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("shopify: marshal admin request: %w", err)
	}

	return c.doWithRetry(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(encoded))
		if err != nil {
			return nil, err
		}
		req.Header.Set("X-Shopify-Access-Token", c.cfg.AdminAccessToken)
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	}, func(respBody []byte, _ int) error {
		if out == nil || len(respBody) == 0 {
			return nil
		}
		return json.Unmarshal(respBody, out)
	})
}

// ─── Retry loop ───────────────────────────────────────────────────────────────

type buildReqFn func() (*http.Request, error)
type handleRespFn func(body []byte, statusCode int) error

func (c *httpClient) doWithRetry(ctx context.Context, buildReq buildReqFn, handle handleRespFn) error {
	tracer := otel.Tracer("github.com/sarvexlabs/sarvex.cp/shopify")
	var lastErr error
	backoff := 250 * time.Millisecond

	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
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
			return fmt.Errorf("shopify: build request: %w", err)
		}

		// Create a span for this HTTP call attempt
		callCtx, span := tracer.Start(ctx, "shopify.http_call",
			trace.WithAttributes(
				attribute.String("http.method", req.Method),
				attribute.String("http.url", req.URL.String()),
				attribute.String("gateway.platform", "shopify"),
				attribute.Int("http.attempt", attempt+1),
			),
		)

		resp, err := c.underlying.Do(req)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			span.End()
			lastErr = fmt.Errorf("shopify: http transport error: %w", err)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			span.End()
			lastErr = fmt.Errorf("shopify: read response body: %w", err)
			continue
		}

		span.SetAttributes(
			attribute.Int("http.status_code", resp.StatusCode),
			attribute.Int("http.response_content_length", len(body)),
		)

		c.logger.DebugContext(callCtx, "shopify api call",
			"merchant_id", c.cfg.MerchantID,
			"method", req.Method,
			"url", req.URL.String(),
			"status", resp.StatusCode,
		)

		// On rate limit, respect Retry-After header before retrying.
		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
			span.SetStatus(codes.Error, "rate limited")
			span.SetAttributes(
				attribute.Int64("http.retry_after_ms", retryAfter.Milliseconds()),
			)
			span.End()

			c.logger.WarnContext(callCtx, "shopify rate limited",
				"merchant_id", c.cfg.MerchantID,
				"retry_after_ms", retryAfter.Milliseconds(),
			)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryAfter):
			}
			lastErr = ErrRateLimited
			continue
		}

		if err := classifyHTTPError(resp.StatusCode, string(body)); err != nil {
			// Only retry transient errors.
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

	return fmt.Errorf("shopify: all %d attempts failed, last error: %w", c.cfg.MaxRetries+1, lastErr)
}

func parseRetryAfter(header string) time.Duration {
	if header == "" {
		return 1 * time.Second
	}
	if secs, err := strconv.Atoi(header); err == nil {
		return time.Duration(secs) * time.Second
	}
	return 1 * time.Second
}
