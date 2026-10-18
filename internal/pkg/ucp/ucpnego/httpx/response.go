// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package httpx

import (
	"encoding/json"
	"net/http"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/ucpnego/negotiate"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

// SuccessEnvelope is the shape of the `ucp` object in a successful
// response: the negotiated protocol version and the filtered capability
// set relevant to the operation.
type SuccessEnvelope struct {
	Version         wire.Version                       `json:"version"`
	Capabilities    map[string][]capabilityVersionOnly `json:"capabilities,omitempty"`
	PaymentHandlers map[string][]paymentHandlerSummary `json:"payment_handlers,omitempty"`
}

// In responses the spec encodes capabilities as {"version": "..."} only —
// not the full declaration. We use small structs to avoid leaking the
// business's spec/schema URLs unnecessarily.
type capabilityVersionOnly struct {
	Version wire.Version `json:"version"`
}

type paymentHandlerSummary struct {
	ID                   string                   `json:"id,omitempty"`
	Version              wire.Version             `json:"version"`
	AvailableInstruments []wire.PaymentInstrument `json:"available_instruments,omitempty"`
}

// BuildSuccessEnvelope produces the `ucp` envelope to embed in a
// successful response, filtered to the operation type.
func BuildSuccessEnvelope(r negotiate.Result, op negotiate.Operation) SuccessEnvelope {
	env := SuccessEnvelope{
		Version:         r.ProtocolVersion,
		Capabilities:    make(map[string][]capabilityVersionOnly),
		PaymentHandlers: make(map[string][]paymentHandlerSummary),
	}

	for name, c := range negotiate.FilterResponseCapabilities(r, op) {
		env.Capabilities[name] = []capabilityVersionOnly{{Version: c.Version}}
	}

	for name, h := range r.PaymentHandlers {
		env.PaymentHandlers[name] = []paymentHandlerSummary{{
			ID:                   h.ID,
			Version:              h.Version,
			AvailableInstruments: h.AvailableInstruments,
		}}
	}
	return env
}

// WriteSuccess writes a 200 OK response with the given UCP envelope and
// caller-supplied operation payload. The payload is merged at the top
// level alongside `ucp` to match the spec's response shape.
func WriteSuccess(w http.ResponseWriter, env SuccessEnvelope, payload map[string]interface{}) error {
	body := map[string]interface{}{"ucp": env}
	for k, v := range payload {
		if k == "ucp" {
			continue // protect the envelope
		}
		body[k] = v
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(body)
}
