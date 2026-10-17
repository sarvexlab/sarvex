// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlab/sarvex/internal/pkg/middlewares"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/ucpnego/httpx"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
	"github.com/sarvexlab/sarvex/internal/planes/serving"
)

// checkoutSurface identifies the checkout surface for the session-projection
// console (checkout_lifecycle_events.go). There is exactly one: UCP
// checkout — a constant rather than a derived value until a second surface
// exists.
const checkoutSurface = "ucp_checkout"

// agentPlatformFromRequest best-effort identifies the calling agent
// platform from the UCP-Agent header's profile URL host (e.g.
// "google.com"). Returns "" when the header is absent or unparseable —
// this is console-facing metadata only, never a checkout decision input,
// so a parse failure here must never surface as a request error.
func agentPlatformFromRequest(r *http.Request) string {
	header := r.Header.Get("UCP-Agent")
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

// This file holds the checkout endpoints' wire contract only — request
// bodies, response envelopes, and the domain-error -> HTTP-status mapping.
// No orchestration lives here; handler.go calls into these purely to bind
// a request or shape a response.

// CheckoutCreateRequest is the request body for creating a checkout session,
// matching the reconciled ucp.dev Checkout capability's create-time fields
// (v20260408.Checkout: line_items is required; buyer/fulfillment/payment are
// optional at create and are normally supplied by the subsequent PUT). The
// idempotency key travels in the Idempotency-Key header, not the body, per
// the reconciled ucp.dev REST transport — which is also how the agent-side
// client sends it (internal/pkg/ucp/agent.Agent.CreateCheckout).
type CheckoutCreateRequest struct {
	// CartID converts a cart into this checkout (cart.json's "Checkout with
	// Cart"): line items, buyer and discounts the request does not itself
	// carry are taken from the cart.
	CartID    string                       `json:"cart_id,omitempty"`
	LineItems []base.LineItemCreateRequest `json:"line_items"`
	Buyer     *model.BuyerWithConsent      `json:"buyer,omitempty"`
	// Currency is captured only so it participates in the idempotency body
	// hash — reusing an Idempotency-Key with a changed currency is a conflict.
	// Sarvex does not otherwise honor a buyer-asserted currency: the merchant
	// determines it (see the canonical Checkout.currency doc).
	Currency    string                  `json:"currency,omitempty"`
	Fulfillment *wireFulfillmentRequest `json:"fulfillment,omitempty"`
	Payment     *model.Payment          `json:"payment,omitempty"`
	Discounts   *model.DiscountsObject  `json:"discounts,omitempty"`
}

// CheckoutUpdateRequest is the request body for updating a checkout session
// (PUT, full-replacement semantics per spec).
type CheckoutUpdateRequest struct {
	LineItems   []base.LineItemCreateRequest `json:"line_items,omitempty"`
	Buyer       *model.BuyerWithConsent      `json:"buyer,omitempty"`
	Fulfillment *wireFulfillmentRequest      `json:"fulfillment,omitempty"`
	Payment     *model.Payment               `json:"payment,omitempty"`
	Discounts   *model.DiscountsObject       `json:"discounts,omitempty"`
}

// wireFulfillmentRequest models the checkout capability's `fulfillment`
// request object as it actually arrives on the wire (ucp.dev shopping
// fulfillment): a list of methods, each carrying candidate destinations, a
// selected destination id, and groups whose selected_option_id names the
// chosen shipping option. The connector layer consumes a flattened
// base.FulfillmentRequest (one destination address + one selected method id),
// so toBase() reduces this to that. Binding the request straight into
// base.FulfillmentRequest — as this handler used to — silently dropped every
// selection, because the wire shape and that flat shape share no field names.
type wireFulfillmentRequest struct {
	Methods []wireFulfillmentMethod `json:"methods,omitempty"`
}

type wireFulfillmentMethod struct {
	ID                    string                       `json:"id,omitempty"`
	Type                  string                       `json:"type,omitempty"`
	Destinations          []wireFulfillmentDestination `json:"destinations,omitempty"`
	SelectedDestinationID string                       `json:"selected_destination_id,omitempty"`
	Groups                []wireFulfillmentGroup       `json:"groups,omitempty"`
}

// wireFulfillmentDestination is a candidate destination: an id plus the
// canonical postal-address fields (promoted from the embedded PostalAddress).
type wireFulfillmentDestination struct {
	ID   string `json:"id,omitempty"`
	Type string `json:"type,omitempty"`
	model.PostalAddress
}

type wireFulfillmentGroup struct {
	ID               string `json:"id,omitempty"`
	SelectedOptionID string `json:"selected_option_id,omitempty"`
}

// toBase flattens the first method's selection into the connector-facing
// base.FulfillmentRequest: the address of the destination named by
// selected_destination_id (falling back to the first destination), and the
// selected shipping option id from the first group that carries one. A pickup
// method is passed through by type and destination id instead. Returns
// nil when there is nothing to act on, so a caller can pass it straight
// through as the optional fulfillment input.
func (w *wireFulfillmentRequest) toBase() *base.FulfillmentRequest {
	if w == nil || len(w.Methods) == 0 {
		return nil
	}
	m := w.Methods[0]
	out := &base.FulfillmentRequest{
		MethodType:            m.Type,
		SelectedMethodID:      selectedOptionID(m.Groups),
		SelectedDestinationID: m.SelectedDestinationID,
	}

	// A pickup selection names one of the business's own retail locations by
	// id (the agent does not supply an address for it), so there is no postal
	// address to extract; the destination id and method type are the selection.
	if m.Type == string(model.FulfillmentMethodTypePickup) {
		if out.SelectedDestinationID == "" && out.SelectedMethodID == "" {
			return nil
		}
		return out
	}

	if len(m.Destinations) > 0 {
		chosen := m.Destinations[0]
		for _, d := range m.Destinations {
			if m.SelectedDestinationID != "" && d.ID == m.SelectedDestinationID {
				chosen = d
				break
			}
		}
		addr := chosen.PostalAddress
		out.SelectedDestination = &addr
	}

	if out.SelectedDestination == nil && out.SelectedMethodID == "" {
		return nil
	}
	return out
}

// selectedOptionID returns the first non-empty selected_option_id across the
// groups — the shipping option the agent has chosen, if any.
func selectedOptionID(groups []wireFulfillmentGroup) string {
	for _, g := range groups {
		if g.SelectedOptionID != "" {
			return g.SelectedOptionID
		}
	}
	return ""
}

// OpaquePaymentCredential wraps only the handler reference and opaque credential
// identifier, never exposing raw credential data (e.g., card numbers, CVCs).
// This type enforces the architectural invariant: "The checkout schema has no
// field able to hold a raw credential. Sarvex sees opaque references + handler_id only."
type OpaquePaymentCredential struct {
	handlerID             string // opaque handler identifier (e.g., "com.stripe")
	opaqueCredentialToken string // opaque reference from the handler (e.g., tokenized credential)
}

// NewOpaquePaymentCredential constructs a credential wrapper from a handler ID
// and opaque credential token. The token is kept private to prevent accidental exposure.
func NewOpaquePaymentCredential(handlerID, opaqueToken string) *OpaquePaymentCredential {
	return &OpaquePaymentCredential{
		handlerID:             handlerID,
		opaqueCredentialToken: opaqueToken,
	}
}

// HandlerID returns the payment handler identifier.
func (o *OpaquePaymentCredential) HandlerID() string {
	return o.handlerID
}

// OpaqueToken returns the opaque credential reference (tokenized credential).
// This is safe to store and transmit as it contains no raw credential data.
func (o *OpaquePaymentCredential) OpaqueToken() string {
	return o.opaqueCredentialToken
}

// ToPayment converts the opaque credential to a minimal Payment structure
// that only contains the opaque token reference, never exposing raw credentials.
func (o *OpaquePaymentCredential) ToPayment() *model.Payment {
	if o == nil {
		return nil
	}

	// Build a Payment with a single instrument containing only the opaque token.
	// The handler ID and opaque token are the only safe data; raw credential fields
	// (Number, CVC, etc.) are never included.
	return &model.Payment{
		Instruments: []model.SelectedPaymentInstrument{
			{
				PaymentInstrument: model.PaymentInstrument{
					HandlerID: o.handlerID,
					// The opaque token is stored as a TokenCredential, which is the
					// safe way to represent a tokenized/encrypted credential.
					Credential: &model.PaymentCredential{
						Type: "token",
						Token: &model.TokenCredential{
							Type:  "opaque_token",
							Token: o.opaqueCredentialToken,
						},
					},
				},
				Selected: true,
			},
		},
	}
}

// MarshalJSON renders the credential as the wire's "payment" object — the
// same shape ToPayment builds — so a value constructed with
// NewOpaquePaymentCredential round-trips through JSON.
func (o *OpaquePaymentCredential) MarshalJSON() ([]byte, error) {
	if o == nil {
		return []byte("null"), nil
	}
	return json.Marshal(o.ToPayment())
}

// UnmarshalJSON decodes a credential from the wire's Payment object
// (`{"instruments":[{"handler_id":...,"selected":true,"credential":{...}}]}`,
// the same shape CheckoutCreateRequest/CheckoutUpdateRequest's Payment
// field carries — see the reconciled ucp.dev checkout capability), extracting
// only the selected instrument's handler ID and opaque token and discarding
// everything else. This is deliberately NOT `json.Unmarshal(data,
// &model.Payment{})`: that type faithfully models every raw credential
// field a handler might send (Number, CVC, Cryptogram, ...) so it can pass
// them through to the connector unmodified — exactly what checkout-complete
// must never do. Manually plucking two string fields out of a raw map is
// the structural enforcement that raw PANs never enter the checkout flow,
// no matter what the request body actually contains.
func (o *OpaquePaymentCredential) UnmarshalJSON(data []byte) error {
	var wire struct {
		Instruments []struct {
			HandlerID string `json:"handler_id"`
			Selected  bool   `json:"selected"`
			// Credential is declared with only "type"/"token" — the fields
			// a TokenCredential marshals to (model.PaymentCredential's
			// MarshalJSON flattens straight to the concrete subtype, no
			// wrapper key). Any other credential shape (CardCredential,
			// PANCredential, ...) has no "token" field, so it silently
			// decodes to "" here instead of ever being modeled.
			Credential *struct {
				Token string `json:"token"`
			} `json:"credential"`
		} `json:"instruments"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if len(wire.Instruments) == 0 {
		return nil
	}

	// Per the checkout capability's Payment schema, exactly one instrument
	// is selected; fall back to the first (and normally only) instrument if
	// none is explicitly marked, rather than silently accepting no handler.
	instrument := wire.Instruments[0]
	for _, candidate := range wire.Instruments {
		if candidate.Selected {
			instrument = candidate
			break
		}
	}

	o.handlerID = instrument.HandlerID
	if instrument.Credential != nil {
		o.opaqueCredentialToken = instrument.Credential.Token
	}
	return nil
}

// CheckoutCompleteRequest is the request body for completing a checkout
// session. Credential binds to the wire's "payment" field — the same key
// CheckoutCreateRequest/CheckoutUpdateRequest use — because that is what
// the reconciled ucp.dev checkout capability, and every real caller
// (internal/pkg/ucp/agent.Agent.CompleteCheckout included), actually sends.
// The payment credential is an opaque token — never stored raw (architecture §2 invariant 7).
type CheckoutCompleteRequest struct {
	Credential     *OpaquePaymentCredential `json:"payment,omitempty"`
	IdempotencyKey string                   `json:"idempotency_key,omitempty"`
}

// checkoutCompleteResponse is the wire envelope for POST
// .../checkout-sessions/{id}/complete. It carries either Escalation
// (requires_escalation — the buyer must complete an out-of-band action
// before retrying) or Order (the placed order), never both.
type checkoutCompleteResponse struct {
	SessionID string               `json:"session_id"`
	State     serving.SessionState `json:"state"`

	Status      base.CheckoutStatus `json:"status,omitempty"`
	ContinueURL string              `json:"continue_url,omitempty"`
	Messages    []model.Message     `json:"messages,omitempty"`

	Order *checkoutOrderResponse `json:"order,omitempty"`
}

type checkoutOrderResponse struct {
	ID        string              `json:"id"`
	Status    base.CheckoutStatus `json:"status"`
	Total     int64               `json:"total"`
	Currency  string              `json:"currency"`
	Permalink string              `json:"permalink"`
}

// checkoutCompleteBody shapes a serving.CheckoutCompleteResult into the
// endpoint's wire envelope.
func checkoutCompleteBody(result *serving.CheckoutCompleteResult) *checkoutCompleteResponse {
	body := &checkoutCompleteResponse{SessionID: result.SessionID, State: result.State}

	if result.Escalation != nil {
		body.Status = result.Escalation.Status
		body.ContinueURL = result.Escalation.ContinueURL
		body.Messages = result.Escalation.Messages
	}
	if result.Order != nil {
		body.Order = &checkoutOrderResponse{
			ID:        result.Order.ID,
			Status:    result.Order.Status,
			Total:     result.Order.Total,
			Currency:  result.Order.Currency,
			Permalink: result.Order.Permalink,
		}
	}
	return body
}

// mapCheckoutError translates a serving.CheckoutService error into an HTTP
// status. Every checkout endpoint funnels its error through here so the
// status-code policy lives in exactly one place.
// mapCheckoutError maps checkout domain errors to HTTP responses, including
// correlation ID in logs and response bodies for request tracing.
func (h *servingHandler) mapCheckoutError(c echo.Context, err error) error {
	// Extract correlation ID for logging and error response inclusion
	cid := middlewares.GetCorrelationID(c)

	// A business rejection from the merchant backend (out of stock, unknown
	// item, declined payment, a missing required selection) is the buyer's to
	// resolve, not a platform failure: answer 422 with a structured `detail`
	// carrying the store's own message, per the checkout capability's error
	// model, rather than the transport-failure "store unavailable" 503.
	var rejected *base.CheckoutRejectedError
	if errors.As(err, &rejected) {
		h.logger.Debug("checkout error: business rejection", "correlation_id", cid, "code", rejected.Code)
		return c.JSON(http.StatusUnprocessableEntity, echo.Map{"detail": rejected.Detail, "code": rejected.Code})
	}

	switch {
	case errors.Is(err, serving.ErrCartNotFound):
		h.logger.Debug("cart error: cart not found", "correlation_id", cid)
		return echo.NewHTTPError(http.StatusNotFound, "cart not found")
	case errors.Is(err, serving.ErrCartNotReady):
		h.logger.Debug("cart error: cart not ready", "correlation_id", cid)
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	case errors.Is(err, serving.ErrSessionNotFound):
		h.logger.Debug("checkout error: session not found", "correlation_id", cid)
		return echo.NewHTTPError(http.StatusNotFound, "session not found")
	case errors.Is(err, serving.ErrSessionExpired):
		h.logger.Debug("checkout error: session expired", "correlation_id", cid)
		return echo.NewHTTPError(http.StatusGone, "session expired")
	case errors.Is(err, serving.ErrLineItemsRequired):
		h.logger.Debug("checkout error: line items required", "correlation_id", cid)
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	case errors.Is(err, serving.ErrCheckoutNotReady):
		h.logger.Debug("checkout error: checkout not ready", "correlation_id", cid)
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	case errors.Is(err, serving.ErrCheckoutNotModifiable):
		h.logger.Debug("checkout error: not modifiable in current state", "correlation_id", cid)
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	case errors.Is(err, serving.ErrIdempotentReplayIncomplete):
		h.logger.Debug("checkout error: idempotent replay incomplete", "correlation_id", cid)
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	case errors.Is(err, serving.ErrIdempotencyKeyConflict):
		h.logger.Debug("checkout error: idempotency key conflict", "correlation_id", cid)
		return echo.NewHTTPError(http.StatusConflict, "idempotency key reused with different request body")
	case errors.Is(err, serving.ErrFreshnessGuardFailed):
		h.logger.Debug("checkout error: freshness guard failed", "correlation_id", cid)
		return echo.NewHTTPError(http.StatusConflict, "freshness guard failed")
	case errors.Is(err, serving.ErrUnknownPaymentHandler):
		h.logger.Debug("checkout error: unknown payment handler", "correlation_id", cid)
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	case errors.Is(err, serving.ErrConnectorUnavailable):
		h.logger.Warn("checkout error: connector unavailable", "correlation_id", cid, "error", err)
		return echo.NewHTTPError(http.StatusServiceUnavailable, "store unavailable")
	default:
		h.logger.Error("checkout request failed", "correlation_id", cid, "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "internal error")
	}
}

// orderCapabilityName is the reverse-domain capability id order.md's
// webhook_url config lives under in a platform profile.
const orderCapabilityName = "dev.ucp.shopping.order"

// extractOrderWebhookURL reads the negotiated platform profile's
// dev.ucp.shopping.order capability config for webhook_url (order.md): the
// URL the platform wants order-event webhooks delivered to. Empty when the
// platform advertised no such capability or no webhook_url — order webhooks
// then simply are not sent, a nil-safe no-op (WebhookDeliverer), not an
// error: not every agent wants them.
func extractOrderWebhookURL(profile *wire.Profile) string {
	if profile == nil {
		return ""
	}
	for _, entry := range profile.UCP.Capabilities[orderCapabilityName] {
		if len(entry.Config) == 0 {
			continue
		}
		var cfg struct {
			WebhookURL string `json:"webhook_url"`
		}
		if err := json.Unmarshal(entry.Config, &cfg); err == nil && cfg.WebhookURL != "" {
			return cfg.WebhookURL
		}
	}
	return ""
}
