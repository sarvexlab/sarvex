// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"

	"github.com/google/uuid"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	pkgucp "github.com/sarvexlab/sarvex/internal/pkg/ucp"
	cataloguev1 "github.com/sarvexlab/sarvex/internal/pkg/ucp/catalogue/v1"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

// Checkout-service errors. The HTTP handler (api package) maps these to
// status codes; ErrSessionNotFound/ErrSessionExpired (session_store.go)
// are reused directly by the same mapping.
var (
	ErrLineItemsRequired = errors.New("serving: line_items is required and must not be empty")
	// ErrCheckoutNotReady means Create hasn't yet produced a backend
	// checkout for this session — Update/Complete have nothing to act on.
	ErrCheckoutNotReady = errors.New("serving: checkout has no backend session yet — create must succeed first")
	// ErrIdempotentReplayIncomplete means a session exists for this
	// idempotency key but the original attempt never reached a confirmed
	// backend checkout (e.g. it crashed between session-create and the
	// connector call) — nothing safe to replay, so retry needs a fresh key.
	ErrIdempotentReplayIncomplete = errors.New("serving: previous attempt failed before a backend checkout was created — use a new idempotency key to retry")
	ErrFreshnessGuardFailed       = errors.New("serving: catalog cache is stale, refusing to complete checkout against possibly-outdated pricing")
	// ErrConnectorUnavailable wraps any failure reaching or resolving the
	// merchant backend connector (architecture §5's one live-call exception).
	ErrConnectorUnavailable = errors.New("serving: store connector unavailable")
	// ErrUnknownPaymentHandler means Complete was submitted with a payment
	// handler ID the store's profile does not advertise — the agent is
	// meant to pick from FetchProfile's payment_handlers, never invent one.
	ErrUnknownPaymentHandler = errors.New("serving: payment handler not advertised by this store")
	// ErrCheckoutNotModifiable means Update/Complete/Cancel was called on a
	// session already in a terminal state (SessionState.Terminal()) other
	// than the specific case that operation itself treats as idempotent —
	// Cancel replays success for an already-cancelled session,
	// Complete replays success for a matching idempotency key; every other
	// state/operation combination is a genuine conflict.
	ErrCheckoutNotModifiable = errors.New("serving: checkout session cannot be modified in its current state")
)

// CheckoutService owns the checkout session lifecycle: session persistence,
// the live connector calls architecture §5 permits (verify inventory/
// pricing at create, live shipping/tax at update, capture+order at
// complete), and the order-intent handoff to the sync plane. The HTTP
// handler's job is limited to binding the request, calling this service,
// and translating the result/error to a response.
//
// Persistence ordering matters here: a connector call that succeeds has
// already changed real state at the merchant (a checkout exists, an order
// was placed) — the connector's success is the source of truth, and this
// service is written so that Sarvex's own bookkeeping failing afterward
// never contradicts or hides that from the agent. See persistSessionWithRetry
// and Complete's ordering (order-intent emitted, and success reported,
// independent of whether the final session-store write succeeds).
type CheckoutService struct {
	sessions SessionStore
	cache    ServingCache
	registry *base.Registry
	events   eventbus.EventBus
	logger   *slog.Logger
	// webhooks delivers the order-event webhook on a successful Complete
	// (order.md). A nil *WebhookDeliverer no-ops (see its own doc comment),
	// so omitting it — every pre-existing caller of NewCheckoutService — is
	// unchanged behavior, not a missing feature.
	webhooks *WebhookDeliverer
}

// NewCheckoutService constructs a CheckoutService. webhooks is optional
// (trailing, variadic) — pass one to enable order-event webhook delivery on
// Complete; omit it for behavior identical to before webhooks existed.
func NewCheckoutService(sessions SessionStore, cache ServingCache, registry *base.Registry, events eventbus.EventBus, logger *slog.Logger, webhooks ...*WebhookDeliverer) *CheckoutService {
	if logger == nil {
		logger = slog.Default()
	}
	var wh *WebhookDeliverer
	if len(webhooks) > 0 {
		wh = webhooks[0]
	}
	return &CheckoutService{sessions: sessions, cache: cache, registry: registry, events: events, logger: logger, webhooks: wh}
}

// resolveConnector is the serving plane's only route to a store connector.
//
// The connector comes back wrapped in blockPullConnector (pull_guard.go), so
// the live checkout calls architecture §5 permits pass through while the
// store-API pull methods the sync plane owns fail loudly. Wrapping here
// rather than at the registry keeps the guard on every connector the serving
// plane can reach, since this is the single place it gets one.
func (s *CheckoutService) resolveConnector(instance pkgucp.StoreProvider) (base.Connector, error) {
	connector, err := s.registry.Get(instance)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConnectorUnavailable, err)
	}
	return blockPullConnector{Connector: connector, storeID: instance.Configuration().StoreID}, nil
}

// persistSessionWithRetry saves session, retrying a bounded number of times
// with a short backoff on failure. Used at the points where session-store
// state must track a connector operation that has already happened for
// real at the merchant — losing that write silently would leave Sarvex's
// own record permanently out of sync with (or blind to) what the merchant
// backend actually did.
func persistSessionWithRetry(ctx context.Context, store SessionStore, session *CheckoutSession, log *slog.Logger, op string) error {
	const maxAttempts = 3
	var err error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err = store.Update(ctx, session); err == nil {
			return nil
		}
		if attempt < maxAttempts {
			log.Warn("session persistence failed, retrying", "op", op, "attempt", attempt, "session_id", session.SessionID, "error", err)
			select {
			case <-time.After(time.Duration(attempt) * 50 * time.Millisecond):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return fmt.Errorf("persist session after connector confirmed %q: %w", op, err)
}

// ─── Create ─────────────────────────────────────────────────────────────────

// CreateCheckoutInput is the caller-supplied portion of a new checkout session.
type CreateCheckoutInput struct {
	LineItems      []base.LineItemCreateRequest
	Buyer          *model.BuyerWithConsent
	Fulfillment    *base.FulfillmentRequest
	Payment        *model.Payment
	Discounts      *model.DiscountsObject
	IdempotencyKey string
	BodyHash       string // SHA256 hash of normalized request body for idempotency conflict detection
	// WebhookURL is the platform's advertised order-event webhook endpoint,
	// read from the negotiated platform profile (order.md). Empty when the
	// platform advertised no dev.ucp.shopping.order capability config.
	WebhookURL string
	// Surface and AgentPlatform are best-effort, console-facing metadata
	// only — carried through to the session_initiated lifecycle event
	// (checkout_lifecycle_events.go), never used for any checkout decision.
	// AgentPlatform is empty when the caller has no UCP-Agent header.
	Surface       string
	AgentPlatform string
	// SpecVersion is the negotiated UCP generation of this request, and
	// BuyerConsentWire the request's buyer.consent in that generation's shape
	// (see CheckoutSession).
	SpecVersion      string
	BuyerConsentWire json.RawMessage
}

// Create creates a new checkout session: persists the session, then makes
// the one live call to the merchant backend the serving plane is allowed
// (architecture §5) to make, to verify inventory/pricing.
//
// The returned response's ID is always Sarvex's session ID, never the
// backend's own checkout/order ID — every subsequent call (Get/Update/
// Complete) addresses the session by that ID via the URL path, so
// returning the backend ID here would make every follow-up call
// unroutable.
func (s *CheckoutService) Create(ctx context.Context, instance pkgucp.StoreProvider, storeID string, in CreateCheckoutInput) (*base.CheckoutResponse, error) {
	if len(in.LineItems) == 0 {
		return nil, ErrLineItemsRequired
	}

	if in.IdempotencyKey != "" {
		resp, found, err := s.replayIdempotentCreate(ctx, instance, storeID, in.IdempotencyKey, in.BodyHash)
		if err != nil {
			return nil, err
		}
		if found {
			return resp, nil
		}
	}

	sessionID := uuid.New().String()
	now := time.Now().UTC()
	session := &CheckoutSession{
		SessionID:   sessionID,
		StoreID:     storeID,
		MerchantID:  sessionMerchantID(instance, storeID),
		State:       SessionStateIncomplete,
		LineItems:   in.LineItems,
		Buyer:       in.Buyer,
		Fulfillment: in.Fulfillment,
		Payment:     sanitizePayment(in.Payment),
		Discounts:   in.Discounts,
		WebhookURL:  in.WebhookURL,
		SpecVersion: in.SpecVersion,

		BuyerConsentWire: in.BuyerConsentWire,
		IdempotencyKey:   in.IdempotencyKey,
		BodyHash:         in.BodyHash,
		CreatedAt:        now,
		UpdatedAt:        now,
		// Surface is resolved once here and persisted so every later
		// lifecycle event for this session (update/escalated/failed/
		// completed/abandoned) reports the same surface — see
		// lifecycle_contract.go's deriveSurface.
		Surface: string(deriveSurface(in.AgentPlatform)),
	}

	// Persist before calling the connector so a connector failure still has
	// a session (marked failed) to report against, and so a retried
	// request with the same idempotency key can be dedup'd instead of
	// creating a second backend checkout.
	if err := s.sessions.Create(ctx, session); err != nil {
		return nil, fmt.Errorf("create checkout session: %w", err)
	}

	connector, err := s.resolveConnector(instance)
	if err != nil {
		return nil, err
	}

	connResp, err := connector.CheckoutCreate(ctx, storeID, buildCheckoutCreateRequest(session))
	if err != nil {
		failed := *session
		failed.State = SessionStateFailed
		failed.Errors = append(append([]SessionError{}, session.Errors...), SessionError{
			Code: "CONNECTOR_CREATE_FAILED", Message: "store rejected checkout creation",
		})
		if uerr := s.sessions.Update(ctx, &failed); uerr != nil {
			s.logger.Error("failed to persist checkout-create failure", "error", uerr, "session_id", sessionID, "store_id", storeID)
		}
		// Log connector error with session context at Warn level for visibility
		s.logger.Warn("connector checkout create failed", "session_id", sessionID, "store_id", storeID, "error", err)
		s.emitFailed(ctx, &failed, "CONNECTOR_CREATE_FAILED", "store rejected checkout creation", "create", nil, "")
		// A business rejection (out of stock, unknown item) is the buyer's to
		// resolve and maps to a 4xx — surface it as-is rather than as the
		// transport-failure "store unavailable".
		var rejected *base.CheckoutRejectedError
		if errors.As(err, &rejected) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %v", ErrConnectorUnavailable, err)
	}

	// The connector has now genuinely created a checkout at the merchant —
	// from here on, losing the session_id -> backend_checkout_id mapping
	// would orphan it (there is no cancel endpoint yet — architecture G1),
	// so this write is retried rather than given up on the first failure.
	session.BackendCheckoutID = connResp.ID
	session.LastKnownCart = cartSnapshotFromTotals(connResp.Totals, session.LineItems)
	session.UpdatedAt = time.Now().UTC()

	// The wire ID is always the session ID (see this method's doc comment);
	// rewrite before caching so LastResponse — what CheckoutGet returns
	// verbatim — matches what this call itself returns.
	connResp.ID = session.SessionID
	if connResp.Payment == nil {
		connResp.Payment = echoablePayment(session.Payment)
	}
	echoBuyer(connResp, session.Buyer)
	applySelectedDestinationID(connResp, session.Fulfillment)
	session.LastResponse = connResp

	if err := persistSessionWithRetry(ctx, s.sessions, session, s.logger, "checkout create"); err != nil {
		s.logger.Error("orphaned backend checkout: could not persist backend_checkout_id",
			"error", err, "store_id", storeID, "session_id", sessionID, "backend_checkout_id", connResp.ID)
		return nil, fmt.Errorf("persist session: %w", err)
	}

	s.emitSessionInitiated(ctx, session, connResp, in.Surface, in.AgentPlatform)
	s.emitInitiatedV1(session)

	return connResp, nil
}

// replayIdempotentCreate looks up a prior attempt by idempotency key and,
// if it reached a confirmed backend checkout, replays the connector's
// current state so a retried request gets the same response instead of a
// second backend checkout. found is false only when no session exists yet
// for this key (a genuinely new request). If the body hash differs from the
// existing session's hash, returns ErrIdempotencyKeyConflict to indicate that
// the same idempotency key was reused with a different request body.
func (s *CheckoutService) replayIdempotentCreate(ctx context.Context, instance pkgucp.StoreProvider, storeID, idempotencyKey string, bodyHash string) (resp *base.CheckoutResponse, found bool, err error) {
	existing, err := s.sessions.GetByIdempotencyKey(ctx, storeID, idempotencyKey)
	if errors.Is(err, ErrSessionNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("check idempotency key: %w", err)
	}

	// Check for idempotency key reuse with a different request body.
	// If both have body hashes, compare those; otherwise fall back to checking
	// if line items changed (backward compatibility with sessions created before body hash support).
	if bodyHash != "" && existing.BodyHash != "" && bodyHash != existing.BodyHash {
		return nil, false, ErrIdempotencyKeyConflict
	}

	if existing.BackendCheckoutID == "" {
		return nil, false, ErrIdempotentReplayIncomplete
	}

	connector, err := s.resolveConnector(instance)
	if err != nil {
		return nil, false, err
	}
	connResp, err := connector.CheckoutGet(ctx, storeID, existing.BackendCheckoutID)
	if err != nil {
		return nil, false, fmt.Errorf("%w: idempotent replay CheckoutGet: %v", ErrConnectorUnavailable, err)
	}
	connResp.ID = existing.SessionID
	// Echo payment the same way the original Create did, so a replayed create
	// returns a byte-identical response (idempotency_test compares the two).
	if connResp.Payment == nil {
		connResp.Payment = echoablePayment(existing.Payment)
	}
	echoBuyer(connResp, existing.Buyer)
	return connResp, true, nil
}

// ─── Get ────────────────────────────────────────────────────────────────────

// Get retrieves a checkout session by Sarvex session ID.
func (s *CheckoutService) Get(ctx context.Context, storeID, sessionID string) (*CheckoutSession, error) {
	return s.sessions.Get(ctx, storeID, sessionID)
}

// GetResponse retrieves a checkout session and projects it to the same
// spec-shaped body Create/Update/Complete return (base.CheckoutResponse) —
// the wire contract for GET /checkout-sessions/{id}. A session that has
// never completed a connector call (Create failed before LastResponse was
// set) has nothing spec-shaped to return; ErrCheckoutNotReady signals that
// the same way it already does for Update/Complete on such a session.
func (s *CheckoutService) GetResponse(ctx context.Context, storeID, sessionID string) (*base.CheckoutResponse, error) {
	session, err := s.sessions.Get(ctx, storeID, sessionID)
	if err != nil {
		return nil, err
	}
	if session.LastResponse == nil {
		return nil, ErrCheckoutNotReady
	}
	return session.LastResponse, nil
}

// ─── Update ─────────────────────────────────────────────────────────────────

// UpdateCheckoutInput carries the full-replacement fields a checkout PUT
// may supply. A zero-value field (nil / empty slice) leaves the
// corresponding session field unchanged.
type UpdateCheckoutInput struct {
	LineItems      []base.LineItemCreateRequest
	Buyer          *model.BuyerWithConsent
	Fulfillment    *base.FulfillmentRequest
	Payment        *model.Payment
	Discounts      *model.DiscountsObject
	WebhookURL     string
	IdempotencyKey string
	BodyHash       string
	// SpecVersion and BuyerConsentWire: see CreateCheckoutInput.
	SpecVersion      string
	BuyerConsentWire json.RawMessage
}

// Update replaces a checkout session's line items/buyer/fulfillment/
// payment (PUT semantics) and makes the live call to fetch shipping
// options and recompute tax (architecture §5).
func (s *CheckoutService) Update(ctx context.Context, instance pkgucp.StoreProvider, storeID, sessionID string, in UpdateCheckoutInput) (*base.CheckoutResponse, error) {
	session, err := s.sessions.Get(ctx, storeID, sessionID)
	if err != nil {
		return nil, err
	}
	if session.BackendCheckoutID == "" {
		return nil, ErrCheckoutNotReady
	}
	// A cancelled, completed, or already-failed session has nothing left
	// to update — PUT is full-replacement, not a resumable draft.
	if session.State.Terminal() {
		return nil, ErrCheckoutNotModifiable
	}

	// Idempotent replay: the same Idempotency-Key with the same body returns
	// the prior response without re-calling the connector; the same key with a
	// different body is a conflict (409). A new key (or none) falls through.
	if in.IdempotencyKey != "" && session.UpdateIdempotencyKey == in.IdempotencyKey {
		if session.UpdateBodyHash != in.BodyHash {
			return nil, ErrIdempotencyKeyConflict
		}
		if session.LastResponse != nil {
			return session.LastResponse, nil
		}
	}

	// Work on a copy: the session store may hand back a live, shared
	// object (the in-memory store does), so mutating it in place would let
	// a concurrent reader (or a later failed request) observe fields the
	// merchant never actually confirmed. Only persist once the connector
	// has agreed to the new state.
	updated := *session
	if len(in.LineItems) > 0 {
		updated.LineItems = in.LineItems
	}
	if in.Buyer != nil {
		// The buyer object is PUT-replace, so its consent goes with it: an
		// update that states no consent clears the platform's earlier detail
		// as surely as it clears the merchant's.
		updated.Buyer = in.Buyer
		updated.BuyerConsentWire = in.BuyerConsentWire
	}
	if in.Fulfillment != nil {
		updated.Fulfillment = in.Fulfillment
	}
	if in.Payment != nil {
		updated.Payment = sanitizePayment(in.Payment)
	}
	if in.SpecVersion != "" {
		updated.SpecVersion = in.SpecVersion
	}
	// Discounts are PUT-replace: a non-nil object (including one with an empty
	// Codes slice, which clears previously applied codes) replaces the prior
	// set. Nil leaves it unchanged.
	if in.Discounts != nil {
		updated.Discounts = in.Discounts
	}
	// Refresh only when this request actually carried one — negotiation
	// derives it from the UCP-Agent header, so a stray call without one must
	// not erase a webhook URL a prior request already recorded.
	if in.WebhookURL != "" {
		updated.WebhookURL = in.WebhookURL
	}

	connector, err := s.resolveConnector(instance)
	if err != nil {
		return nil, err
	}

	connResp, err := connector.CheckoutUpdate(ctx, storeID, updated.BackendCheckoutID, buildCheckoutUpdateRequest(&updated))
	if err != nil {
		// Unlike Create/Complete, this branch doesn't persist a failed
		// session state today (the session stays usable for a retry) — not
		// something this change is altering. The console still needs to
		// see the attempt, so it gets an event even though the session
		// store doesn't.
		// Log connector error with session context at Warn level for visibility
		s.logger.Warn("connector checkout update failed", "session_id", sessionID, "store_id", storeID, "error", err)
		s.emitFailed(ctx, &updated, "CONNECTOR_UPDATE_FAILED", "store rejected checkout update", "update", nil, "")
		var rejected *base.CheckoutRejectedError
		if errors.As(err, &rejected) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %v", ErrConnectorUnavailable, err)
	}

	updated.State = SessionStateReadyForComplete
	updated.LastKnownCart = cartSnapshotFromTotals(connResp.Totals, updated.LineItems)
	updated.UpdatedAt = time.Now().UTC()
	updated.UpdateIdempotencyKey = in.IdempotencyKey
	updated.UpdateBodyHash = in.BodyHash

	connResp.ID = updated.SessionID
	if connResp.Payment == nil {
		connResp.Payment = echoablePayment(updated.Payment)
	}
	echoBuyer(connResp, updated.Buyer)
	applySelectedDestinationID(connResp, updated.Fulfillment)
	updated.LastResponse = connResp

	if err := persistSessionWithRetry(ctx, s.sessions, &updated, s.logger, "checkout update"); err != nil {
		s.logger.Error("checkout updated at merchant but session persistence failed — session may show stale buyer/fulfillment/line_items until the next successful write",
			"error", err, "store_id", storeID, "session_id", sessionID, "backend_checkout_id", updated.BackendCheckoutID)
		return nil, fmt.Errorf("persist session: %w", err)
	}

	s.emitCartUpdated(ctx, &updated, connResp)
	s.emitUpdatedV1(&updated)

	return connResp, nil
}

// ─── Cancel ─────────────────────────────────────────────────────────────────

// Cancel cancels a checkout session (architecture G1). Repeated cancellation
// of an already-cancelled session is idempotent success — the spec allows
// either that or a client error (checkout_lifecycle_test.py's
// test_repeated_cancel); every other terminal state (completed, committed
// pending handoff, failed) is a genuine conflict, since an order may
// already exist at the merchant.
func (s *CheckoutService) Cancel(ctx context.Context, storeID, sessionID string) (*base.CheckoutResponse, error) {
	session, err := s.sessions.Get(ctx, storeID, sessionID)
	if err != nil {
		return nil, err
	}
	if session.State == SessionStateCancelled {
		return session.LastResponse, nil
	}
	if session.BackendCheckoutID == "" {
		return nil, ErrCheckoutNotReady
	}
	if session.State.Terminal() {
		return nil, ErrCheckoutNotModifiable
	}

	cancelled := *session
	cancelled.State = SessionStateCancelled
	cancelled.UpdatedAt = time.Now().UTC()
	if cancelled.LastResponse != nil {
		resp := *cancelled.LastResponse
		resp.Status = base.CheckoutStatusCanceled
		cancelled.LastResponse = &resp
	}

	if err := s.sessions.Update(ctx, &cancelled); err != nil {
		return nil, fmt.Errorf("persist checkout cancellation: %w", err)
	}

	return cancelled.LastResponse, nil
}

// ─── Complete ───────────────────────────────────────────────────────────────

// CheckoutCompleteResult is either an escalation (buyer must complete an
// out-of-band action before retrying complete) or a placed order — the two
// are mutually exclusive per the spec's requires_escalation flow.
type CheckoutCompleteResult struct {
	SessionID  string
	State      SessionState
	Escalation *CheckoutEscalation
	Order      *CompletedOrder
	// Response is the full spec-shaped checkout the connector returned for
	// this completion (status completed or requires_escalation, with totals,
	// links, fulfillment, and the order confirmation). The HTTP handler
	// returns this verbatim — the checkout capability's complete endpoint
	// responds with the Checkout resource, not a bespoke envelope.
	Response *base.CheckoutResponse
}

// CheckoutEscalation carries the buyer-facing continuation the agent needs
// when the connector reports requires_escalation (e.g. 3DS).
type CheckoutEscalation struct {
	Status      base.CheckoutStatus
	ContinueURL string
	Messages    []model.Message
}

// CompletedOrder is the placed order the agent gets back on success.
type CompletedOrder struct {
	ID        string
	Status    base.CheckoutStatus
	Total     int64
	Currency  string
	Permalink string
}

// Complete finalizes a checkout: enforces the freshness guard, submits the
// opaque payment credential to the connector, and — once the connector
// confirms an order was placed — emits the order-intent event and reports
// success to the agent.
//
// From the moment the connector confirms the order, the connector's answer
// is the source of truth, not Sarvex's own session bookkeeping: the
// order-intent event is built from data already in hand and emitted
// independent of whether the final session-store write succeeds, and the
// method still returns a successful CheckoutCompleteResult even if that
// write fails — telling the agent "error" after a real order was placed
// would be worse than a session record that's briefly stale. See
// persistSessionWithRetry for the same ordering principle applied to
// Create/Update, where a connector-confirmed change has nowhere else to be
// recorded and so is retried instead.
func (s *CheckoutService) Complete(ctx context.Context, instance pkgucp.StoreProvider, storeID, sessionID string, payment *model.Payment, idempotencyKey string) (*CheckoutCompleteResult, error) {
	session, err := s.sessions.Get(ctx, storeID, sessionID)
	if err != nil {
		return nil, err
	}
	if session.BackendCheckoutID == "" {
		return nil, ErrCheckoutNotReady
	}

	// Idempotent replay: the same key with the same payment payload returns
	// the prior outcome instead of calling the connector (and the
	// merchant's payment processor) a second time. Checked before the
	// terminal-state guard below — a completed/cancelled session replaying
	// its own completion key is success, not a conflict; the same key with
	// a different payload is (ErrIdempotencyKeyConflict, matching Create's
	// rule); any other terminal state, or no matching key at all, falls
	// through to the guard.
	bodyHash := completeBodyHash(payment)
	if idempotencyKey != "" && session.CompleteIdempotencyKey == idempotencyKey {
		if session.CompleteBodyHash != bodyHash {
			return nil, ErrIdempotencyKeyConflict
		}
		return completeResultFromSession(session), nil
	}
	if session.State.Terminal() {
		return nil, ErrCheckoutNotModifiable
	}

	checkTime := time.Now().UTC()
	freshness, err := s.cache.Watermark(ctx, storeID)
	if err != nil {
		return nil, fmt.Errorf("check freshness: %w", err)
	}

	// IsStale alone: it already means "a signalled catalog change has gone
	// un-rebuilt past the window" (see CacheWatermark). The extra Age check
	// beside it was redundant with the old rule and actively wrong under the
	// new one — Age is now how long the snapshot has been *behind*, which is
	// zero for a store that is level with its source however long ago it was
	// last built. Together they failed a sale for every merchant whose catalog
	// had simply been steady for five minutes.
	freshnessResult := &FreshnessCheckResult{CheckedAt: checkTime, CacheAge: freshness.Age, GuardWindow: freshnessWindow}
	if freshness.IsStale {
		freshnessResult.Passed = false
		freshnessResult.Issues = append(freshnessResult.Issues, FreshnessIssue{Type: "cache_stale"})

		failed := *session
		failed.State = SessionStateFailed
		failed.FreshnessCheck = freshnessResult
		failed.Errors = append(append([]SessionError{}, session.Errors...), SessionError{
			Code:    "FRESHNESS_GUARD_FAILED",
			Message: fmt.Sprintf("cache age %dms exceeds %dms threshold", freshness.Age.Milliseconds(), (5 * time.Minute).Milliseconds()),
		})
		if uerr := s.sessions.Update(ctx, &failed); uerr != nil {
			s.logger.Error("failed to persist freshness-guard failure", "error", uerr, "session_id", sessionID, "store_id", storeID)
		}
		s.logger.Warn("freshness guard check failed", "session_id", sessionID, "store_id", storeID)
		s.emitFailed(ctx, &failed, "FRESHNESS_GUARD_FAILED", failed.Errors[len(failed.Errors)-1].Message, "complete_freshness_guard", nil, "")
		return nil, ErrFreshnessGuardFailed
	}
	freshnessResult.Passed = true

	// The spec lets an agent pick any handler it likes, but only the ones
	// this store's own profile (FetchProfile's payment_handlers) advertised
	// — a stray or fabricated handler_id must never reach the connector, let
	// alone place an order.
	if handlerID, ok := unadvertisedPaymentHandler(instance, payment); !ok {
		s.logger.Warn("checkout complete rejected: payment handler not advertised by this store",
			"session_id", sessionID, "store_id", storeID, "handler_id", handlerID)
		return nil, fmt.Errorf("%w: %q", ErrUnknownPaymentHandler, handlerID)
	}

	connector, err := s.resolveConnector(instance)
	if err != nil {
		return nil, err
	}

	s.emitPaymentSubmitted(ctx, session)

	// Live call: capture funds + create order synchronously (architecture
	// §5, sequence diagram step 3). Payment credential is passed through
	// as an opaque token — never stored (invariant 7). IdempotencyKey is
	// passed to the payment handler (Stripe, etc.) for exactly-once charge.
	checkoutResp, err := connector.CheckoutComplete(ctx, storeID, session.BackendCheckoutID, &base.CompleteCheckoutRequest{
		Payment:        payment,
		Buyer:          session.Buyer,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		failed := *session
		failed.State = SessionStateFailed
		failed.Errors = append(append([]SessionError{}, session.Errors...), SessionError{
			Code: "ORDER_CREATION_FAILED", Message: "failed to create order in your store",
		})
		if uerr := s.sessions.Update(ctx, &failed); uerr != nil {
			s.logger.Error("failed to persist checkout-complete failure", "error", uerr, "session_id", sessionID, "store_id", storeID)
		}
		// Log order creation failure at Error level for immediate visibility
		s.logger.Error("order creation failed at merchant backend", "session_id", sessionID, "store_id", storeID, "error", err)
		s.emitFailed(ctx, &failed, "ORDER_CREATION_FAILED", "failed to create order in your store", "complete", nil, "")
		var rejected *base.CheckoutRejectedError
		if errors.As(err, &rejected) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %v", ErrConnectorUnavailable, err)
	}

	// 3DS / soft-decline: buyer must complete an action off-band. The
	// session stays ready_for_complete so the agent can retry complete
	// after the buyer returns — nothing was committed, nothing to persist.
	if checkoutResp.Status == base.CheckoutStatusRequiresEscalation {
		s.emitEscalated(ctx, session, checkoutResp)
		return &CheckoutCompleteResult{
			SessionID: session.SessionID,
			State:     session.State,
			Escalation: &CheckoutEscalation{
				Status:      checkoutResp.Status,
				ContinueURL: checkoutResp.ContinueURL,
				Messages:    checkoutResp.Messages,
			},
			Response: checkoutResp,
		}, nil
	}

	// From here, the merchant has genuinely placed an order.
	order := buildCompletedOrder(checkoutResp)

	if err := EmitOrderIntent(ctx, s.events, buildOrderIntent(session, checkoutResp, order, checkTime), s.logger); err != nil {
		s.logger.Error("failed to emit order-intent — order handoff to the sync plane may be delayed", "error", err, "session_id", session.SessionID)
		// Best-effort: the order is committed at the merchant either way.
	}
	s.emitCompletedV1(session)

	committed := *session
	committed.State = SessionStateCommittedPendingHandoff
	committed.FreshnessCheck = freshnessResult
	committed.UpdatedAt = checkTime
	committed.OrderID = order.ID
	committed.CompleteIdempotencyKey = idempotencyKey
	committed.CompleteBodyHash = bodyHash
	checkoutResp.ID = committed.SessionID
	committed.LastResponse = checkoutResp
	if err := persistSessionWithRetry(ctx, s.sessions, &committed, s.logger, "checkout complete"); err != nil {
		s.logger.Error("order placed at merchant but session persistence failed — session will show stale state until the next successful write",
			"error", err, "store_id", storeID, "session_id", sessionID, "order_id", order.ID)
		// Do not fail the response: the order is real regardless of
		// whether our own bookkeeping succeeded.
	}

	// The merchant has genuinely placed an order — notify the platform's
	// order-event webhook, if it advertised one (order.md: MUST send the
	// "Order created" event with the fully populated order entity). Async
	// and best-effort, like order-intent above: never blocks or fails this
	// response.
	s.webhooks.DeliverOrderEvent(ctx, committed.WebhookURL, buildOrder(&committed), committed.SpecVersion)

	return &CheckoutCompleteResult{
		SessionID: session.SessionID,
		State:     SessionStateCommittedPendingHandoff,
		Order:     order,
		Response:  checkoutResp,
	}, nil
}

// completeBodyHash hashes the completion request's varying input (the
// payment credential) the same way computeCheckoutCreateBodyHash hashes
// Create's body — a nil payment (no credential supplied) still hashes to a
// stable value so two such calls compare equal.
func completeBodyHash(payment *model.Payment) string {
	data, err := json.Marshal(payment)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

// completeResultFromSession rebuilds Complete's result from a session whose
// CompleteIdempotencyKey already matched — the idempotent-replay path, so
// no connector call and no new events.
func completeResultFromSession(session *CheckoutSession) *CheckoutCompleteResult {
	result := &CheckoutCompleteResult{SessionID: session.SessionID, State: session.State, Response: session.LastResponse}
	if session.LastResponse == nil {
		return result
	}
	if session.LastResponse.Status == base.CheckoutStatusRequiresEscalation {
		result.Escalation = &CheckoutEscalation{
			Status:      session.LastResponse.Status,
			ContinueURL: session.LastResponse.ContinueURL,
			Messages:    session.LastResponse.Messages,
		}
		return result
	}
	result.Order = buildCompletedOrder(session.LastResponse)
	return result
}

// unadvertisedPaymentHandler reports whether every instrument in payment
// names a handler the store advertises, returning the first offending
// handler ID when one does not.
//
// Every instrument, not only the one marked Selected: at completion the
// payment block is an instruction to charge rather than a menu, and Complete
// forwards the block whole to the connector (see the CheckoutComplete call
// below), so a handler the merchant never enabled must not ride along on an
// entry the caller simply left unmarked. An instrument carrying an empty
// handler_id names nothing and fails the same rule.
//
// A nil payment, or one with no instruments, passes: refusing it would be a
// new rejection unrelated to this check, and such a request already fails
// downstream at the connector.
func unadvertisedPaymentHandler(instance pkgucp.StoreProvider, payment *model.Payment) (string, bool) {
	if payment == nil {
		return "", true
	}
	for _, instrument := range payment.Instruments {
		if !advertisesPaymentHandler(instance, instrument.HandlerID) {
			return instrument.HandlerID, false
		}
	}
	return "", true
}

// advertisesPaymentHandler reports whether handlerID appears anywhere in
// the store's own advertised payment handlers — the same
// UCP.PaymentHandlers block FetchProfile's /.well-known/ucp document
// renders (profile_renderer.go), so an agent has no way to discover a
// handler this check would then reject.
func advertisesPaymentHandler(instance pkgucp.StoreProvider, handlerID string) bool {
	// Conformance-only allowance: a few official-suite tests hardcode the
	// mock handler id when completing, and it cannot be advertised in the
	// profile (its id is not a valid reverse-domain profile key). Accept it at
	// completion under the same env gate `sarvex conform serve` sets; nothing
	// sets it in production. See cataloguev1.ConformanceMockHandlerEnvVar.
	if handlerID == cataloguev1.MockPaymentHandlerID && os.Getenv(cataloguev1.ConformanceMockHandlerEnvVar) != "" {
		return true
	}
	for _, handlers := range instance.PaymentHandlers() {
		for _, h := range handlers {
			if h.ID == handlerID {
				return true
			}
		}
	}
	return false
}

// ─── Mapping helpers ────────────────────────────────────────────────────────
// Moved out of the HTTP handler: these translate between the session's
// spec-shaped state and the connector's request/event shapes, none of
// which is an HTTP transport concern.

// buildCheckoutCreateRequest maps the session to a connector
// CreateCheckoutRequest. Only line items are typically sent at create
// time; buyer/fulfillment/payment are usually added in the subsequent PUT,
// but are passed through if present.
func buildCheckoutCreateRequest(session *CheckoutSession) *base.CreateCheckoutRequest {
	return &base.CreateCheckoutRequest{
		LineItems:   session.LineItems,
		Buyer:       session.Buyer,
		Fulfillment: session.Fulfillment,
		Payment:     session.Payment,
		Discounts:   session.Discounts,
	}
}

// buildCheckoutUpdateRequest maps the full session state to a connector
// UpdateCheckoutRequest (PUT, full-replacement semantics per spec).
func buildCheckoutUpdateRequest(session *CheckoutSession) *base.UpdateCheckoutRequest {
	req := &base.UpdateCheckoutRequest{
		Buyer:       session.Buyer,
		Fulfillment: session.Fulfillment,
		Payment:     session.Payment,
		Discounts:   session.Discounts,
	}
	if len(session.LineItems) > 0 {
		req.LineItems = make([]base.LineItemUpdateRequest, len(session.LineItems))
		for i, item := range session.LineItems {
			req.LineItems[i] = base.LineItemUpdateRequest{Item: item.Item, Quantity: item.Quantity}
		}
	}
	return req
}

// buildCompletedOrder extracts the agent-facing order summary from the
// connector's completion response. Totals come from the connector's
// response, not the request — the agent's line items carry no price
// (architecture §5: checkout is the one path that trusts the merchant's
// live pricing over anything cached/sent).
func buildCompletedOrder(checkoutResp *base.CheckoutResponse) *CompletedOrder {
	var total int64
	for _, t := range checkoutResp.Totals {
		if t.Type == model.TotalTypeTotal {
			total = int64(t.Amount)
			break
		}
	}

	order := &CompletedOrder{
		Status:    checkoutResp.Status,
		Total:     total,
		Currency:  "USD",
		Permalink: checkoutResp.ContinueURL,
	}
	if checkoutResp.Order != nil {
		order.ID = checkoutResp.Order.ID
		if checkoutResp.Order.PermalinkURL != "" {
			order.Permalink = checkoutResp.Order.PermalinkURL
		}
	}
	return order
}

// applySelectedDestinationID stamps the agent's chosen destination id onto the
// response's fulfillment, so the id the agent submitted round-trips back to it.
// The merchant backend persists only the destination address, not the id the
// agent gave it, so the serving plane — which does hold the request — restores
// it. In the single-destination checkout model every echoed destination is the
// selected one, so they all take the id.
func applySelectedDestinationID(resp *base.CheckoutResponse, fulfillment *base.FulfillmentRequest) {
	if resp == nil || resp.Fulfillment == nil || fulfillment == nil || fulfillment.SelectedDestinationID == "" {
		return
	}
	id := fulfillment.SelectedDestinationID
	for i := range resp.Fulfillment.Methods {
		m := &resp.Fulfillment.Methods[i]
		if len(m.Destinations) == 0 {
			continue
		}
		m.SelectedDestinationID = &id
		for j := range m.Destinations {
			if m.Destinations[j].Shipping != nil {
				m.Destinations[j].Shipping.ID = id
			}
		}
	}
}

// echoBuyer fills the response's buyer from the session's when the connector
// left it unset: a backend that can only record the buyer when the order is
// placed (Magento guest checkout) has nothing to read back before then, but the
// agent still expects to see the buyer it submitted.
func echoBuyer(resp *base.CheckoutResponse, buyer *model.BuyerWithConsent) {
	if resp.Buyer == nil && buyer != nil {
		resp.Buyer = buyer
	}
}

// echoablePayment returns a copy of the buyer-submitted payment suitable to
// reflect on a checkout response: the instruments are preserved (an agent
// reads them back and re-sends them on the next update) but any credential is
// dropped — a checkout response must never reflect a payment credential
// (architecture invariant 8). A nil payment yields nil so the field stays
// absent when the buyer has submitted nothing.
func echoablePayment(p *model.Payment) *base.ResponsePayment {
	if p == nil {
		return nil
	}
	out := &base.ResponsePayment{Instruments: make([]model.SelectedPaymentInstrument, 0, len(p.Instruments))}
	for _, inst := range p.Instruments {
		inst.Credential = nil
		out.Instruments = append(out.Instruments, inst)
	}
	return out
}

// buildOrderIntent builds the serving.checkout.order_intent event payload
// for the sync plane's OrderIntentHandler. IdempotencyKey is carried through
// for audit trail linking and replay diagnostics.
func buildOrderIntent(session *CheckoutSession, checkoutResp *base.CheckoutResponse, order *CompletedOrder, at time.Time) *OrderIntentEvent {
	lineItems := make([]LineItem, len(session.LineItems))
	for i, item := range session.LineItems {
		lineItems[i] = LineItem{SKU: item.Item.ID, Quantity: item.Quantity}
	}
	return &OrderIntentEvent{
		CheckoutSessionID: session.SessionID,
		OrderID:           order.ID,
		StoreOrderRef:     order.ID,
		OrderStatus:       string(checkoutResp.Status),
		IdempotencyKey:    session.CompleteIdempotencyKey,
		StoreID_:          session.StoreID,
		MerchantID_:       session.MerchantID,
		LineItems:         lineItems,
		Total:             Money{AmountMinor: int(order.Total), Currency: order.Currency},
		EmittedAt_:        at,
	}
}

// sessionMerchantID is the merchant the session belongs to, as its lifecycle
// events must carry it: the control plane attributes every checkout event by
// merchant_id, so using the store id there files a checkout under the wrong
// tenant for any merchant whose store id differs from its own id. It falls back
// to the store id only when the store's profile carries no merchant yet.
func sessionMerchantID(instance pkgucp.StoreProvider, storeID string) string {
	if id := instance.Configuration().MerchantID; id != "" {
		return id
	}
	return storeID
}

// sanitizePayment keeps only what may be stored and forwarded from a payment
// object on create/update: which handler and instrument the buyer chose, and an
// opaque token credential. Anything else a credential carries — a card number,
// CVC, cryptogram, or an unrecognised credential kept verbatim — is dropped.
//
// Payment binds to the full spec type on these endpoints, which faithfully
// models raw card fields so it can decode them; without this, a platform
// sending a PAN here would have it persisted in the session store and handed to
// the connector, breaking the payment-credential boundary (sarvex-v1-architecture.md A10.8) that
// checkout-complete enforces structurally (OpaquePaymentCredential).
func sanitizePayment(p *model.Payment) *model.Payment {
	if p == nil {
		return nil
	}
	out := &model.Payment{Instruments: make([]model.SelectedPaymentInstrument, len(p.Instruments))}
	for i, inst := range p.Instruments {
		if inst.Credential != nil && inst.Credential.Token == nil {
			inst.Credential = nil
		}
		out.Instruments[i] = inst
	}
	return out
}

// ConsentWire returns the buyer.consent the platform sent for this session, in
// the shape of the generation it negotiated — the detail a newer generation's
// response echoes. Nil when there is none or the session is unknown.
func (s *CheckoutService) ConsentWire(ctx context.Context, storeID, sessionID string) json.RawMessage {
	session, err := s.sessions.Get(ctx, storeID, sessionID)
	if err != nil {
		return nil
	}
	return session.BuyerConsentWire
}
