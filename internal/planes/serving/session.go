// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"encoding/json"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

// CheckoutSession represents a UCP checkout session, owned by exactly one
// store. LineItems/Buyer/Fulfillment/Payment mirror the reconciled ucp.dev
// Checkout capability's wire shape (v20260408.Checkout) directly — the
// session is the spec resource, not a Sarvex-private cart representation,
// so CheckoutCreate/Update can bind the HTTP request straight into it and
// buildCheckoutCreateRequest/buildCheckoutUpdateRequest (api/handler.go)
// pass these fields to the connector near-verbatim.
type CheckoutSession struct {
	SessionID         string                       `json:"session_id"`
	StoreID           string                       `json:"store_id"`
	MerchantID        string                       `json:"merchant_id"`                   // denormalized for HMAC/JWT scope resolution
	BackendCheckoutID string                       `json:"backend_checkout_id,omitempty"` // connector-assigned ID, threaded across Create→Update→Complete
	State             SessionState                 `json:"state"`
	LineItems         []base.LineItemCreateRequest `json:"line_items"`
	Buyer             *model.BuyerWithConsent      `json:"buyer,omitempty"`
	Fulfillment       *base.FulfillmentRequest     `json:"fulfillment,omitempty"`
	Payment           *model.Payment               `json:"payment,omitempty"`
	// Discounts carries the buyer-submitted discount codes (PUT-replaces the
	// prior set), forwarded to the connector so the merchant can apply them.
	Discounts *model.DiscountsObject `json:"discounts,omitempty"`
	// WebhookURL is the platform's advertised order-event webhook endpoint
	// (order.md), read from the negotiated platform profile's
	// dev.ucp.shopping.order capability config at Create/Update and carried
	// on the session so Complete and later order PUTs can still reach it —
	// SimulateShipping/OrderService.Update address a session that never goes
	// through negotiation itself.
	WebhookURL string `json:"webhook_url,omitempty"`
	// SpecVersion is the UCP spec generation the platform last negotiated for
	// this session (e.g. "2026-08-25"); empty means 2026-04-08. Later calls
	// negotiate afresh, but the order webhook (delivered from Complete and order
	// PUTs, which run outside any negotiation) is rendered in this generation.
	SpecVersion string `json:"spec_version,omitempty"`
	// BuyerConsentWire is the request's buyer.consent exactly as the platform
	// sent it, in that generation's own shape. The canonical buyer holds only
	// what the merchant can persist; this keeps the richer detail a newer
	// generation's consent carries (segments, links, the platform's wording) so
	// the response can echo it (see wireadapt.EncodeResponse).
	BuyerConsentWire   json.RawMessage       `json:"buyer_consent_wire,omitempty"`
	FinalizedLineItems []FinalizedLineItem   `json:"finalized_line_items,omitempty"`
	Totals             *SessionTotals        `json:"totals,omitempty"`
	IdempotencyKey     string                `json:"idempotency_key"`
	BodyHash           string                `json:"body_hash,omitempty"` // SHA256 hash of normalized request body for idempotency conflict detection
	CreatedAt          time.Time             `json:"created_at"`
	UpdatedAt          time.Time             `json:"updated_at"`
	FreshnessCheck     *FreshnessCheckResult `json:"freshness_check,omitempty"`
	Errors             []SessionError        `json:"errors,omitempty"`
	// Surface is the checkout lifecycle event contract's Surface value
	// (lifecycle_contract.go), resolved once at Create from the request's
	// UCP-Agent header and persisted so every later lifecycle event for
	// this session (update/escalated/failed/completed/abandoned) reports
	// the same surface without re-deriving it.
	Surface string `json:"surface,omitempty"`
	// AbandonedNotifiedAt is set the one time the abandonment sweep
	// (abandonment_sweep.go) emits serving.checkout.abandoned for this
	// session — the sweep's own idempotency mark (R4: at most once per
	// session), independent of and never reset by any other write.
	AbandonedNotifiedAt *time.Time `json:"abandoned_notified_at,omitempty"`
	// LastKnownCart is the most recent connector-priced cart snapshot
	// (set after every successful Create/Update, mirroring
	// cartSnapshotFromSession but with real totals instead of the
	// zero-priced, line-items-only snapshot that function builds).
	// checkout.failed and checkout.abandoned prefer this over a freshly
	// built zero-priced snapshot when it's present (see
	// lastKnownOrLineItemsOnlySnapshot in lifecycle_contract.go), so the
	// Lost Revenue Ledger can price a failure/abandonment from the last
	// price the store actually confirmed instead of defaulting to unknown
	// whenever a session had gotten at least one successful Create/Update
	// in before things went wrong. Zero value (never priced yet) means
	// exactly that: no cart total was ever confirmed for this session
	// (e.g. CONNECTOR_CREATE_FAILED, which fails before any connector
	// response exists).
	LastKnownCart CartSnapshot `json:"last_known_cart,omitempty"`
	// LastResponse is the connector's most recent CheckoutResponse (Create/
	// Update/Complete), with ID already rewritten to SessionID — the exact
	// spec-shaped body (id/line_items/status/currency/totals/links, see
	// base.CheckoutResponse) the agent last received. CheckoutGet returns
	// this verbatim rather than the session's own bookkeeping shape, which
	// carries none of those fields under those names.
	LastResponse *base.CheckoutResponse `json:"last_response,omitempty"`
	// CompleteIdempotencyKey/CompleteBodyHash dedup Complete the same way
	// IdempotencyKey/BodyHash dedup Create — a separate pair because
	// Complete is a second, later idempotent operation against the same
	// session, with its own key and its own request body (the payment
	// credential), not a replacement for Create's.
	CompleteIdempotencyKey string `json:"complete_idempotency_key,omitempty"`
	CompleteBodyHash       string `json:"complete_body_hash,omitempty"`
	// UpdateIdempotencyKey/UpdateBodyHash dedup Update the same way the Create
	// and Complete pairs do — the last update's key and body hash, so a
	// replayed PUT with the same key returns the prior response and one with a
	// different body conflicts (409).
	UpdateIdempotencyKey string `json:"update_idempotency_key,omitempty"`
	UpdateBodyHash       string `json:"update_body_hash,omitempty"`
	// OrderID is the placed order's ID (base.CheckoutResponse.Order.ID),
	// set once at Complete. Orders/:orderId (order.go/order_contract.go)
	// look sessions up by this via SessionStore.GetByOrderID — a completed
	// checkout session doubles as Sarvex's record of the order the spec's
	// Order resource represents, rather than a second, separately-owned
	// copy of the same data.
	OrderID string `json:"order_id,omitempty"`
	// Adjustments and FulfillmentEvents are the only fields a completed
	// order's PUT can change (order_contract.go's OrderUpdateRequest) —
	// everything else about a placed order (line items, totals,
	// fulfillment expectations) is fixed at Complete.
	Adjustments       []model.Adjustment       `json:"adjustments,omitempty"`
	FulfillmentEvents []model.FulfillmentEvent `json:"fulfillment_events,omitempty"`
	// CompleteAttempts logs every attempt to Complete this session,
	// including handler responses and local status. Parallel to Order.MutationAttempts
	// on the sync plane for cross-plane audit trail.
	CompleteAttempts []CompleteAttempt `json:"complete_attempts,omitempty"`
}

// CompleteAttempt records a single attempt to Complete a checkout session.
type CompleteAttempt struct {
	AttemptedAt     time.Time              `json:"attempted_at"`
	CompletedAt     *time.Time             `json:"completed_at,omitempty"`
	IdempotencyKey  string                 `json:"idempotency_key"`
	HandlerResponse map[string]interface{} `json:"handler_response"`
	LocalStatus     string                 `json:"local_status"` // "committed_pending_handoff", "completed", "failed", etc.
	ErrorMessage    string                 `json:"error_message,omitempty"`
}

// SessionState represents the state of a checkout session.
type SessionState string

const (
	// SessionStateIncomplete — connector created, awaiting buyer info (fulfillment/address).
	SessionStateIncomplete SessionState = "incomplete"
	// SessionStateReadyForComplete — buyer info collected, shipping/tax resolved, ready to capture.
	SessionStateReadyForComplete SessionState = "ready_for_complete"
	// SessionStateCommittedPendingHandoff — order created in store, sync plane handoff pending.
	SessionStateCommittedPendingHandoff SessionState = "committed_pending_handoff"
	// SessionStateCompleted — order fully confirmed.
	SessionStateCompleted SessionState = "completed"
	// SessionStateFailed — checkout failed (freshness guard, stock, payment, etc).
	SessionStateFailed SessionState = "failed"
	// SessionStateCancelled — session cancelled.
	SessionStateCancelled SessionState = "cancelled"
)

// Terminal reports whether a session in this state will never transition
// further on its own — the abandonment sweep (abandonment_sweep.go) only
// ever considers non-terminal sessions abandonment candidates.
// CommittedPendingHandoff counts as terminal here: the buyer already
// completed the checkout (an order exists at the merchant), it is just
// Sarvex's own sync-plane handoff that is pending.
func (s SessionState) Terminal() bool {
	switch s {
	case SessionStateCommittedPendingHandoff, SessionStateCompleted, SessionStateFailed, SessionStateCancelled:
		return true
	default:
		return false
	}
}

// FinalizedLineItem is a finalized line item with pricing details.
type FinalizedLineItem struct {
	SKU           string `json:"sku"`
	Title         string `json:"title"`
	Quantity      int    `json:"quantity"`
	UnitPrice     Money  `json:"unit_price"`
	LineTotal     Money  `json:"line_total"`
	FreshnessInfo string `json:"freshness_info,omitempty"` // e.g., "price updated 2m ago"
}

// SessionTotals holds order totals.
type SessionTotals struct {
	Subtotal Money `json:"subtotal"`
	Shipping Money `json:"shipping"`
	Tax      Money `json:"tax"`
	Total    Money `json:"total"`
}

// FreshnessCheckResult documents the freshness guard result at complete time.
type FreshnessCheckResult struct {
	Passed      bool             `json:"passed"`
	CheckedAt   time.Time        `json:"checked_at"`
	CacheAge    time.Duration    `json:"cache_age_ms"`
	GuardWindow time.Duration    `json:"guard_window_ms"`
	Issues      []FreshnessIssue `json:"issues,omitempty"`
}

// FreshnessIssue documents a freshness problem that caused guard failure.
type FreshnessIssue struct {
	SKU     string `json:"sku"`
	Type    string `json:"type"` // "price_drift", "stock_mismatch", "unavailable"
	Current Money  `json:"current,omitempty"`
	Served  Money  `json:"served,omitempty"`
}

// SessionError documents a checkout error.
type SessionError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}
