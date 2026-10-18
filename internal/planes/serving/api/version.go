// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	specall "github.com/sarvexlab/sarvex/internal/spec/all"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlab/sarvex/internal/pkg/middlewares"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/negotiation"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/types"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wireadapt"
)

// Every UCP resource is rendered at the HTTP edge in the generation negotiated
// for the request, by that generation's codec (internal/spec/vYYYYMMDD/codec,
// through wireadapt). These helpers are that edge: every checkout, cart, order
// and catalog handler reads its request and writes its response through them,
// so no handler knows which generation it serves, and none can answer in a
// generation other than the negotiated one.

// wireCodecs are the served spec generations' wire codecs.
var wireCodecs = specall.Default().Codecs()

// requestVersion is the UCP generation negotiated for this request. A request
// that carried no UCP-Agent (so was never negotiated) is served in the oldest
// served generation, the behaviour that predates multi-generation serving.
func requestVersion(c echo.Context) types.Version {
	if result, ok := negotiation.GetNegotiationResult(c); ok {
		if v := types.Version(result.Version); wireCodecs.Supported(v) {
			return v
		}
	}
	return wireCodecs.Baseline()
}

// bind reads the request body in the negotiated generation's shape and binds
// its canonical form into dst. It returns what the canonical form cannot hold
// (see wireadapt.RequestExtras).
func (h *servingHandler) bind(c echo.Context, dst any) (wireadapt.RequestExtras, error) {
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return wireadapt.RequestExtras{}, err
	}
	canonical, extras, err := wireCodecs.DecodeRequest(requestVersion(c), body)
	if err != nil {
		return wireadapt.RequestExtras{}, err
	}
	c.Request().Body = io.NopCloser(bytes.NewReader(canonical))
	return extras, c.Bind(dst)
}

// respond writes a canonical resource in the negotiated generation's wire form.
func (h *servingHandler) respond(c echo.Context, status int, r wireadapt.Resource, canonical any) error {
	v := requestVersion(c)

	// The consent the platform sent, for a generation whose consent carries
	// more than the canonical form; looked up only if that codec asks.
	consentWire := func() json.RawMessage {
		cr, ok := canonical.(*base.CheckoutResponse)
		if !ok || cr.Buyer == nil || cr.Buyer.Consent == nil {
			return nil
		}
		instance, ok := negotiation.GetStoreProfile(c)
		if !ok {
			return nil
		}
		return h.checkout.ConsentWire(c.Request().Context(), instance.StoreID(), cr.ID)
	}

	body, err := wireCodecs.EncodeResponse(v, r, canonical, consentWire)
	if err != nil {
		return h.renderFailure(c, v, string(r), err)
	}
	return c.JSONBlob(status, body)
}

// renderFailure answers a response that cannot be rendered in the negotiated
// generation. That is a Sarvex bug, never something to send an agent in
// another generation's shape.
func (h *servingHandler) renderFailure(c echo.Context, v types.Version, what string, err error) error {
	h.logger.Error("failed to render response in negotiated spec version",
		"version", v, "resource", what, "error", err, "correlation_id", middlewares.GetCorrelationID(c))
	return echo.NewHTTPError(http.StatusInternalServerError, "internal error")
}
