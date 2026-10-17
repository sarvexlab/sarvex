// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	specall "github.com/sarvexlab/sarvex/internal/spec/all"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"

	"github.com/google/uuid"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/types"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wireadapt"
)

// webhookMaxAttempts/webhookRetryDelay bound a delivery's retry loop: enough
// attempts, spaced closely enough, that a merchant's transient 5xx or a
// dropped connection resolves within the same order flow rather than being
// silently lost — order.md requires failed webhook deliveries be retried.
const (
	webhookMaxAttempts = 5
	webhookRetryDelay  = 300 * time.Millisecond
)

// WebhookDeliverer delivers order-event webhooks to the platform's
// advertised webhook_url (order.md "Order Event Webhook"): the full,
// current-state order entity — the same shape GET /orders/{id} returns,
// never an incremental delta — with the Standard Webhooks Webhook-Id/
// Webhook-Timestamp headers. A nil *WebhookDeliverer is safe to call
// through (every method no-ops), so callers that construct a
// CheckoutService/OrderService without one — every existing test — need no
// change.
type WebhookDeliverer struct {
	client *http.Client
	logger *slog.Logger
}

// NewWebhookDeliverer constructs a WebhookDeliverer.
func NewWebhookDeliverer(logger *slog.Logger) *WebhookDeliverer {
	if logger == nil {
		logger = slog.Default()
	}
	return &WebhookDeliverer{
		client: &http.Client{Timeout: 10 * time.Second},
		logger: logger,
	}
}

// DeliverOrderEvent asynchronously POSTs order to webhookURL as the full
// current-state order entity, retrying on failure while preserving the same
// Webhook-Id/Webhook-Timestamp and body across attempts — a retry must be
// recognizable as the same event, not a new one (order.md). A no-op when d
// is nil, webhookURL is empty (the platform advertised no order webhook), or
// order is nil.
//
// Fire-and-forget by design: delivery (including retries) must never block
// or fail the request that triggered it (checkout complete, an order PUT) —
// the order is already real at the merchant regardless of whether the
// platform's webhook receiver is currently reachable.
//
// specVersion is the UCP generation the platform negotiated for the session
// (empty means the oldest served one): the webhook runs outside any negotiation, so the
// order is rendered in the generation the platform last spoke.
func (d *WebhookDeliverer) DeliverOrderEvent(ctx context.Context, webhookURL string, order *model.Order, specVersion string) {
	if d == nil || webhookURL == "" || order == nil {
		return
	}
	body, err := wireCodecs.EncodeResponse(orderWireVersion(specVersion), wireadapt.Order, order, nil)
	if err != nil {
		d.logger.Error("webhook: failed to marshal order payload", "error", err, "order_id", order.ID)
		return
	}
	webhookID := uuid.New().String()
	timestamp := time.Now().Unix()
	// Detached from ctx deliberately: delivery (with retries) must outlive
	// the HTTP request that triggered it, and must not be cancelled just
	// because that request's own context ends when its response is sent.
	go d.deliverWithRetry(webhookURL, body, webhookID, timestamp)
}

func (d *WebhookDeliverer) deliverWithRetry(url string, body []byte, webhookID string, timestamp int64) {
	for attempt := 1; attempt <= webhookMaxAttempts; attempt++ {
		if d.attempt(url, body, webhookID, timestamp) {
			return
		}
		if attempt < webhookMaxAttempts {
			time.Sleep(webhookRetryDelay)
		}
	}
	d.logger.Warn("webhook: delivery exhausted retries", "url", url, "webhook_id", webhookID)
}

// attempt makes one delivery attempt, returning whether it succeeded (2xx).
func (d *WebhookDeliverer) attempt(url string, body []byte, webhookID string, timestamp int64) bool {
	// context.Background(), not a caller-supplied ctx: deliverWithRetry
	// already runs detached from the triggering request (see
	// DeliverOrderEvent's doc comment) — a request-scoped context would get
	// cancelled the moment that request's own response is sent, killing
	// delivery (and every retry) right when it's meant to keep going. The
	// client's own Timeout bounds each attempt instead.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		d.logger.Warn("webhook: failed to build request", "error", err, "url", url)
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	// Standard Webhooks required headers (order.md "Required Headers"): a
	// stable event identifier and its unix-seconds timestamp, both preserved
	// verbatim across retries by the caller passing the same values back in.
	req.Header.Set("Webhook-Id", webhookID)
	req.Header.Set("Webhook-Timestamp", strconv.FormatInt(timestamp, 10))

	resp, err := d.client.Do(req)
	if err != nil {
		d.logger.Warn("webhook: delivery attempt failed", "error", err, "url", url, "webhook_id", webhookID)
		return false
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			d.logger.Debug("webhook: failed to close response body", "error", cerr, "url", url)
		}
	}()
	_, _ = io.Copy(io.Discard, resp.Body)

	ok := resp.StatusCode >= 200 && resp.StatusCode < 300
	if !ok {
		d.logger.Warn("webhook: delivery rejected", "status", resp.StatusCode, "url", url, "webhook_id", webhookID)
	}
	return ok
}

// wireCodecs are the served spec generations' wire codecs.
var wireCodecs = specall.Default().Codecs()

// orderWireVersion maps a session's recorded generation to one an order can be
// rendered in, defaulting to the oldest served generation for an empty or
// unknown value — what a session from before generations were recorded was
// served in.
func orderWireVersion(specVersion string) types.Version {
	if v := types.Version(specVersion); wireCodecs.Supported(v) {
		return v
	}
	return wireCodecs.Baseline()
}
