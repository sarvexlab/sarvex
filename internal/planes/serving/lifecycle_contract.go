// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"crypto/rand"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"

	"github.com/oklog/ulid"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/safego"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/ucpnego/httpx"
)

// This file holds the shared building blocks for the versioned checkout
// lifecycle event contract (sarvex-v1-syncing-plane-async.yaml,
// event_version "1.0"): the common envelope fields every event in the
// contract carries, the Surface/FailureCode closed enums, the cart
// snapshot shape, and a panic-safe fire-and-forget publish helper. The
// events themselves live in checkout_lifecycle_events.go (checkout
// transitions) and abandonment_sweep.go (the abandonment sweep);
// telemetry.go's TelemetryRecordedEvent shares the envelope fields and
// FailureCode is referenced by checkout_lifecycle_events.go's
// CheckoutFailedEvent.

// eventContractVersion is event_version on every event in this contract.
// Bump it (and keep old readers tolerant of the old value) the first time
// a breaking payload change is needed; nothing in this codebase branches
// on it today.
const eventContractVersion = "1.0"

// Surface is the closed enum identifying which agent surface drove a
// checkout or request. Best-effort, derived from the UCP-Agent header's
// profile-URL host (agentPlatformFromHeader) — Sarvex has no stronger
// signal than that host today, so anything not recognizably Gemini or
// AI Mode is classified as best it can be rather than guessed.
//
// SurfaceOpenAIChatGPT and SurfaceOtherAgent were added for the Channel
// Dashboard's multi-surface requirement (R8/R9, sarvex-v1-backend-
// architecture channel dashboard task). They extend this closed enum
// without a schema migration — every consumer keys aggregates by the
// Surface string value, so a new member is additive. SurfaceOpenAIChatGPT
// is defined but deliberately left unreachable by deriveSurface today:
// this repo has no evidence any ChatGPT/ACP traffic can reach the serving
// plane yet (no route, no negotiation profile for it), and no header
// signal exists to distinguish it from any other non-Google agent even if
// it did — see deriveSurface's doc comment. Populating it now would be a
// guess, not a classification; the member exists so the day that signal
// does exist, no migration is required (verified by
// TestSurfaceEnumExtension_NoMigrationRequired).
type Surface string

const (
	SurfaceGoogleAIMode  Surface = "google_ai_mode"
	SurfaceGoogleGemini  Surface = "google_gemini"
	SurfaceOpenAIChatGPT Surface = "openai_chatgpt"
	SurfaceOtherAgent    Surface = "other_agent"
	SurfaceUnknown       Surface = "unknown"
)

// deriveSurface maps a best-effort agent-platform host (as produced by
// agentPlatformFromHeader) to the Surface enum. This is a narrow, literal
// hostname match, not a fabricated classifier: Sarvex has no documented
// signal today that distinguishes Google's AI Mode surface from Gemini
// beyond the UCP-Agent profile URL's host, so anything that doesn't
// plainly say "gemini" falls back to the generic Google surface.
//
// A non-empty host that isn't recognizably Google is classified
// SurfaceOtherAgent, not SurfaceUnknown — the request did carry a UCP-Agent
// header identifying *some* calling platform, so "we saw an agent but
// can't name it" is a materially different, more honest state than "no
// signal at all" (empty header, SurfaceUnknown). SurfaceOpenAIChatGPT is
// never returned here: there is no hostname or other signal in this
// codebase today that distinguishes ChatGPT/ACP traffic from any other
// non-Google agent, so it would be a guess rather than a classification —
// it falls into SurfaceOtherAgent like every other unrecognized host until
// a real signal exists.
func deriveSurface(agentPlatform string) Surface {
	host := strings.ToLower(agentPlatform)
	switch {
	case host == "":
		return SurfaceUnknown
	case strings.Contains(host, "gemini"):
		return SurfaceGoogleGemini
	case strings.Contains(host, "google"):
		return SurfaceGoogleAIMode
	default:
		return SurfaceOtherAgent
	}
}

// agentPlatformFromHeader best-effort identifies the calling agent
// platform from the UCP-Agent header's profile URL host (e.g.
// "google.com"). Mirrors api.agentPlatformFromRequest exactly, duplicated
// here rather than imported: api already imports serving, so the reverse
// import would cycle. Returns "" when the header is absent or
// unparseable — this is best-effort metadata only, never a request
// decision, so a parse failure here must never surface as an error.
func agentPlatformFromHeader(h http.Header) string {
	header := h.Get("UCP-Agent")
	if header == "" {
		return ""
	}
	parsed, err := httpx.ParseUCPAgent(header)
	if err != nil {
		return ""
	}
	u, err := url.Parse(parsed.ProfileURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// FailureCode is the closed enum for checkout_lifecycle_events.go's
// CheckoutFailedEvent.Failure.Code — every SessionError.Code
// checkout_service.go can currently produce, and nothing else. Adding a
// new checkout_service.go failure code without adding it here is a bug:
// it would serialize as an empty/unrecognized code on the wire.
type FailureCode string

const (
	// FailureCodeConnectorCreateFailed — the store rejected checkout
	// creation (stock, pricing, or a store-side error at CheckoutCreate).
	FailureCodeConnectorCreateFailed FailureCode = "CONNECTOR_CREATE_FAILED"
	// FailureCodeConnectorUpdateFailed — the store rejected a cart/buyer/
	// fulfillment update (shipping/tax resolution failed at the store).
	FailureCodeConnectorUpdateFailed FailureCode = "CONNECTOR_UPDATE_FAILED"
	// FailureCodeFreshnessGuardFailed — the pre-complete freshness guard
	// refused to complete against a stale catalog cache.
	FailureCodeFreshnessGuardFailed FailureCode = "FRESHNESS_GUARD_FAILED"
	// FailureCodeOrderCreationFailed — the connector's CheckoutComplete
	// call failed to place the order at the merchant.
	FailureCodeOrderCreationFailed FailureCode = "ORDER_CREATION_FAILED"
)

// FailureStage is the closed enum for CheckoutFailedEvent.Failure.Stage
// (R3's full six-value contract stage set). Only session_create,
// cart_update, payment_submission, and confirmation are reachable by any
// failure code checkout_service.go can currently produce (see the
// FailureCode block above and stageForFailureCode); negotiation and
// escalation are defined for a complete closed enum but not yet reachable
// — negotiation failures never reach checkout_service.go's SessionError
// path (they're rejected earlier, in ucpnego), and nothing currently
// classifies a failure during an escalation follow-up as "failed" rather
// than a fresh attempt.
type FailureStage string

const (
	FailureStageNegotiation       FailureStage = "negotiation"
	FailureStageSessionCreate     FailureStage = "session_create"
	FailureStageCartUpdate        FailureStage = "cart_update"
	FailureStagePaymentSubmission FailureStage = "payment_submission"
	FailureStageConfirmation      FailureStage = "confirmation"
	FailureStageEscalation        FailureStage = "escalation"
)

// stageForFailureCode maps each reachable FailureCode to its
// FailureStage. A code with no entry here would be a bug (see FailureCode
// block's doc comment) — stageForFailureCode intentionally has no
// default/fallback branch so that case would return "" rather than a
// silently-wrong guess.
func stageForFailureCode(code FailureCode) FailureStage {
	switch code {
	case FailureCodeConnectorCreateFailed:
		return FailureStageSessionCreate
	case FailureCodeConnectorUpdateFailed:
		return FailureStageCartUpdate
	case FailureCodeFreshnessGuardFailed:
		return FailureStageConfirmation
	case FailureCodeOrderCreationFailed:
		return FailureStagePaymentSubmission
	default:
		return ""
	}
}

// CartLineItemSnapshot is one line item in a CartSnapshot.
type CartLineItemSnapshot struct {
	SKU            string `json:"sku"`
	Quantity       int    `json:"quantity"`
	UnitPriceMinor int64  `json:"unit_price_minor"`
}

// CartSnapshot is the cart state carried on checkout.failed and
// checkout.abandoned events (R3, R4). total_minor is 0 when no live
// connector pricing was ever resolved for this session — checkout
// sessions carry no price of their own (base.ItemRef is id-only; price is
// only known once a connector response has been seen). This is an honest
// "unknown", not a fabricated 0 price: see lastKnownOrFreshSnapshot for
// the one case (a failure/abandonment following an earlier successful
// Create/Update) where a real, connector-confirmed total is available and
// used instead of 0.
type CartSnapshot struct {
	Currency   string                 `json:"currency"`
	TotalMinor int64                  `json:"total_minor"`
	LineItems  []CartLineItemSnapshot `json:"line_items"`
}

// cartSnapshotFromSession builds a CartSnapshot from a session's own line
// items only (sku/quantity, total_minor 0) — the fallback
// lastKnownOrFreshSnapshot uses when no connector-confirmed price was ever
// recorded for this session (session.LastKnownCart's zero value).
func cartSnapshotFromSession(session *CheckoutSession) CartSnapshot {
	items := make([]CartLineItemSnapshot, len(session.LineItems))
	for i, li := range session.LineItems {
		items[i] = CartLineItemSnapshot{SKU: li.Item.ID, Quantity: li.Quantity}
	}
	return CartSnapshot{Currency: "USD", LineItems: items}
}

// cartSnapshotFromTotals builds a CartSnapshot with a real, connector-
// confirmed total — used at the two points a fresh connector response is
// in hand (Create/Update success, checkout_service.go) to populate
// session.LastKnownCart. Mirrors buildCompletedOrder's total-extraction
// loop (checkout_service.go), including its "USD" fallback, since
// model.Total carries no per-total currency field today.
func cartSnapshotFromTotals(totals model.Totals, lineItems []base.LineItemCreateRequest) CartSnapshot {
	var total int64
	for _, t := range totals {
		if t.Type == model.TotalTypeTotal {
			total = int64(t.Amount)
			break
		}
	}
	items := make([]CartLineItemSnapshot, len(lineItems))
	for i, li := range lineItems {
		items[i] = CartLineItemSnapshot{SKU: li.Item.ID, Quantity: li.Quantity}
	}
	return CartSnapshot{Currency: "USD", TotalMinor: total, LineItems: items}
}

// lastKnownOrFreshSnapshot returns session.LastKnownCart when it carries a
// real, connector-confirmed total (set at a prior successful Create/Update
// — see checkout_service.go), so a failure/abandonment later in the same
// session can still report its true revenue impact instead of defaulting
// to unknown. Falls back to cartSnapshotFromSession's zero-priced,
// line-items-only snapshot when no price was ever confirmed (e.g.
// CONNECTOR_CREATE_FAILED, which fails before any connector response
// exists) — never a guess, just the two honest states R8 (Lost Revenue
// Ledger requirements) distinguishes: known or unknown.
func lastKnownOrFreshSnapshot(session *CheckoutSession) CartSnapshot {
	if session.LastKnownCart.TotalMinor != 0 {
		return session.LastKnownCart
	}
	return cartSnapshotFromSession(session)
}

// newEventID mints a fresh ULID prefixed "evt_" (R2) — lexicographically
// sortable by emission time, unlike the pre-existing lifecycle events'
// plain uuid.New().String() event_id.
func newEventID() string {
	id := ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader)
	return "evt_" + id.String()
}

// handlerIDFromPayment returns the handler_id of the session's selected
// payment instrument, or "" when none is set — checkout.failed's
// failure.handler_id (R3) is nullable for exactly this reason: most
// failures (create/update/freshness-guard) happen before any payment
// instrument is selected at all.
func handlerIDFromPayment(payment *model.Payment) string {
	if payment == nil {
		return ""
	}
	for _, inst := range payment.Instruments {
		if inst.Selected {
			return inst.HandlerID
		}
	}
	if len(payment.Instruments) > 0 {
		return payment.Instruments[0].HandlerID
	}
	return ""
}

// lifecyclePublishFailures counts publish failures for the versioned
// lifecycle contract (R6: "publish errors are ... counted (metric)").
// This repo has no metrics library wired into the serving plane today
// (no prometheus import anywhere under internal/planes/serving); a
// package-level atomic counter is the minimal, dependency-free way to
// satisfy "counted" without introducing a new instrumentation dependency
// as a side effect of this change. LifecyclePublishFailureCount exposes
// it for tests and for a future real metrics exporter to read.
var lifecyclePublishFailures atomic.Int64

// LifecyclePublishFailureCount returns the number of failed publishes to
// the versioned checkout lifecycle event contract since process start.
func LifecyclePublishFailureCount() int64 {
	return lifecyclePublishFailures.Load()
}

// publishLifecycleEventV1 is the fire-and-forget publish path for every
// event in the versioned contract (R6): it runs off the request
// goroutine via safego.Go (whose recover() covers ev.Payload()'s
// marshaling too, since Publish calls it internally — a panic there can
// never reach the caller), bounds the publish attempt with a short
// timeout so a wedged bus can't leak goroutines, and never returns
// anything the caller could propagate into the HTTP response.
func publishLifecycleEventV1(pub eventbus.EventBus, ev eventbus.DomainEvent, log *slog.Logger, sessionOrRequestID string) {
	safego.Go("emit-checkout-lifecycle-v1", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := pub.Publish(ctx, ev); err != nil {
			lifecyclePublishFailures.Add(1)
			log.Error("failed to publish checkout lifecycle event", "error", err, "event_type", ev.EventType(), "id", sessionOrRequestID)
		}
	})
}
