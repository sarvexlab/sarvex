// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/eventbus"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/safego"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/negotiation"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/ucpsig"
)

// TelemetryRecordedEvent is emitted for every agent request that reaches
// the serving plane's UCP routes, carrying the outcome of signature
// verification and capability negotiation alongside status/latency. It is
// intentionally distinct from RequestLogEvent ("serving.log.requested"):
// RequestLogEvent predates this and is left untouched, and this event's
// consumer (the endpoint-health evaluator) needs signature/negotiation
// outcome fields RequestLogEvent doesn't carry.
//
// EventVersion/EventID/OccurredAt_/Surface/AgentPlatform are the versioned
// checkout lifecycle contract's common envelope (lifecycle_contract.go,
// R2) — added here even though this event predates that contract, since
// R1 names serving.request.telemetry_recorded as one of its seven
// channels. checkout_id is deliberately absent per R2 ("absent on
// telemetry_recorded"). SignatureResult keeps its pre-existing four-value
// domain (valid/invalid/missing/not_applicable) rather than R5's
// three-value (valid/invalid/absent): the endpoint-health evaluator
// (internal/planes/control/endpointhealth/window.go) already branches on
// "not_applicable" specifically to exclude routes that never carry a
// signature (e.g. GET /.well-known/ucp) from being counted as a signature
// breach — collapsing that into the same "absent" value as a route that
// *should* have been signed and wasn't would misclassify every
// unsigned-by-design request as a breach. The four-value domain is a
// strict superset of R5's three (valid/invalid still mean exactly what
// R5's spec says); see the PR description for this deliberate deviation.
type TelemetryRecordedEvent struct {
	EventVersion      string    `json:"event_version"`
	EventID           string    `json:"event_id"`
	StoreID_          string    `json:"store_id"`
	MerchantID_       string    `json:"merchant_id"`
	Surface           Surface   `json:"surface"`
	AgentPlatform     *string   `json:"agent_platform"`
	RouteTemplate     string    `json:"route_template"`
	Method            string    `json:"method"`
	StatusCode        int       `json:"status_code"`
	LatencyMs         int64     `json:"latency_ms"`
	SignatureResult   string    `json:"signature_result"`   // valid | invalid | missing | not_applicable
	NegotiationResult string    `json:"negotiation_result"` // succeeded | failed | not_applicable
	Synthetic         bool      `json:"synthetic"`
	RequestID         string    `json:"request_id"`
	OccurredAt_       time.Time `json:"occurred_at"`
	Payload_          []byte    `json:"-"`
}

func (t *TelemetryRecordedEvent) EventType() string     { return ucp.EventTelemetryRecorded }
func (t *TelemetryRecordedEvent) StoreID() string       { return t.StoreID_ }
func (t *TelemetryRecordedEvent) MerchantID() string    { return t.MerchantID_ }
func (t *TelemetryRecordedEvent) OccurredAt() time.Time { return t.OccurredAt_ }

func (t *TelemetryRecordedEvent) Payload() []byte {
	if t.Payload_ == nil {
		body, _ := json.Marshal(map[string]any{
			"event_version":      t.EventVersion,
			"event_id":           t.EventID,
			"store_id":           t.StoreID_,
			"merchant_id":        t.MerchantID_,
			"surface":            t.Surface,
			"agent_platform":     t.AgentPlatform,
			"route_template":     t.RouteTemplate,
			"method":             t.Method,
			"status_code":        t.StatusCode,
			"latency_ms":         t.LatencyMs,
			"signature_result":   t.SignatureResult,
			"negotiation_result": t.NegotiationResult,
			"synthetic":          t.Synthetic,
			"request_id":         t.RequestID,
			"occurred_at":        t.OccurredAt_,
		})
		t.Payload_ = body
	}
	return t.Payload_
}

// WithTelemetry records a TelemetryRecordedEvent for every request. It must
// be the outermost middleware in the pipeline (registered first) so it
// observes the outcome even when signature verification or negotiation
// short-circuits the chain before later middleware (including
// WithRequestLogging) ever runs.
func WithTelemetry(pub eventbus.EventBus, log *slog.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			err := next(c)
			latency := time.Since(start)

			instance, ok := negotiation.GetStoreProfile(c)
			if !ok {
				// Tenant middleware never ran (route without :storeID, or
				// resolution itself failed before reaching this point) —
				// nothing to attribute the event to.
				return err
			}

			agentPlatform := agentPlatformFromHeader(c.Request().Header)
			var agentPlatformPtr *string
			if agentPlatform != "" {
				agentPlatformPtr = &agentPlatform
			}

			ev := &TelemetryRecordedEvent{
				EventVersion:  eventContractVersion,
				EventID:       newEventID(),
				StoreID_:      instance.StoreID(),
				MerchantID_:   instance.Configuration().MerchantID,
				Surface:       deriveSurface(agentPlatform),
				AgentPlatform: agentPlatformPtr,
				RouteTemplate: c.Path(),
				Method:        c.Request().Method,
				StatusCode:    statusFrom(c, err),
				LatencyMs:     latency.Milliseconds(),
				RequestID:     c.Response().Header().Get(echo.HeaderXRequestID),
				OccurredAt_:   start,
				Synthetic:     c.Request().Header.Get(ucp.ProbeHeader) != "",
			}

			if result, ok := ucpsig.ResultFrom(c); ok {
				ev.SignatureResult = result
			} else {
				ev.SignatureResult = "not_applicable"
			}
			if outcome, ok := negotiation.GetOutcome(c); ok {
				ev.NegotiationResult = outcome
			} else {
				ev.NegotiationResult = "not_applicable"
			}

			safego.Go("emit-telemetry", func() {
				ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
				defer cancel()
				if pubErr := pub.Publish(ctx, ev); pubErr != nil {
					lifecyclePublishFailures.Add(1)
					log.Debug("telemetry publish failed", "error", pubErr, "request_id", ev.RequestID)
				}
			})

			return err
		}
	}
}

// statusFrom resolves the HTTP status actually returned to the caller. When
// a middleware short-circuits by returning an *echo.HTTPError without
// itself writing to the response (e.g. ucpsig.toHTTPError), c.Response().Status
// isn't set yet at this point in the chain (Echo's global error handler
// runs later, after every middleware unwinds) — the error's own Code is the
// authoritative status in that case. Handlers/middleware that write the
// response directly and return nil (e.g. negotiation's error paths) are
// covered by the c.Response().Status fallback.
func statusFrom(c echo.Context, err error) int {
	var httpErr *echo.HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.Code
	}
	if status := c.Response().Status; status != 0 {
		return status
	}
	return http.StatusOK
}
