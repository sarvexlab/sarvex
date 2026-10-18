// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package paymenthandler

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/sarvexlab/sarvex/internal/pkg/domain"
)

// Configuration status values, matching the control-plane contract's
// payment_config.handlers[].config_status enum.
const (
	StatusNotStarted = "not_started"
	StatusIncomplete = "incomplete"
	StatusReady      = "ready"
)

// Param is one merchant-supplied parameter of a payment handler. Every
// parameter listed here is PUBLIC by construction — it is published in the
// store's /.well-known/ucp profile so agents can tokenize against it. A
// handler needing secret material (processor API keys) must not be modelled
// as a Param: the payment-credential boundary (sarvex-v1-architecture.md A10.8) keeps secrets
// out of Sarvex entirely, so unknown parameter keys are rejected rather than
// stored.
type Param struct {
	Key      string
	Required bool
}

// Spec describes a payment handler Sarvex can advertise.
type Spec struct {
	// Name is the reverse-domain key the handler is published under in a
	// profile's payment_handlers registry.
	Name domain.PaymentHandlerType
	// ID is the short handler id the control-plane contract uses
	// (payment_config.handlers[].handler_id).
	ID string
	// Platforms lists the commerce systems the handler can process payments
	// for; empty means every platform.
	Platforms []domain.CommerceSystemType
	Params    []Param
}

var specs = []Spec{
	{
		Name: domain.GooglePay,
		ID:   "google_pay",
		Params: []Param{
			{Key: "merchant_name", Required: true},
			{Key: "merchant_id", Required: true},
			{Key: "merchant_origin", Required: true},
			{Key: "gateway", Required: true},
			{Key: "gateway_merchant_id", Required: true},
			{Key: "environment"},
		},
	},
}

// Lookup resolves a handler by its contract id ("google_pay") or its
// reverse-domain name ("com.google.pay").
func Lookup(idOrName string) (Spec, bool) {
	for _, s := range specs {
		if s.ID == idOrName || string(s.Name) == idOrName {
			return s, true
		}
	}
	return Spec{}, false
}

// AppliesTo reports whether the handler can be offered for platform.
func (s Spec) AppliesTo(platform string) bool {
	if len(s.Platforms) == 0 {
		return true
	}
	for _, p := range s.Platforms {
		if string(p) == platform {
			return true
		}
	}
	return false
}

// ValidateParams rejects any parameter key the handler does not declare.
func (s Spec) ValidateParams(params map[string]string) error {
	known := make(map[string]bool, len(s.Params))
	for _, p := range s.Params {
		known[p.Key] = true
	}
	var unknown []string
	for k := range params {
		if !known[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("handler %s does not accept parameters %v", s.ID, unknown)
	}
	return nil
}

// missing returns the required parameters params lacks, in declared order.
func (s Spec) missing(params map[string]string) []string {
	var out []string
	for _, p := range s.Params {
		if p.Required && params[p.Key] == "" {
			out = append(out, p.Key)
		}
	}
	return out
}

// ValidateEnabled additionally requires every required parameter; used when a
// handler is being enabled, since an enabled handler without them could not be
// used by an agent.
func (s Spec) ValidateEnabled(params map[string]string) error {
	if err := s.ValidateParams(params); err != nil {
		return err
	}
	if m := s.missing(params); len(m) > 0 {
		return fmt.Errorf("handler %s requires parameters %v", s.ID, m)
	}
	return nil
}

// Status is the handler's configuration status for the contract's
// config_status field.
func (s Spec) Status(enabled bool, params map[string]string) string {
	switch {
	case !enabled && len(params) == 0:
		return StatusNotStarted
	case len(s.missing(params)) > 0:
		return StatusIncomplete
	default:
		return StatusReady
	}
}

// ProfileConfig renders the handler's public `config` block for the profile
// from the merchant's parameters. Only a ready handler has one.
func (s Spec) ProfileConfig(params map[string]string) (map[string]any, error) {
	if m := s.missing(params); len(m) > 0 {
		return nil, fmt.Errorf("handler %s requires parameters %v", s.ID, m)
	}
	var v any
	switch s.Name {
	case domain.GooglePay:
		v = googlePayConfig(params)
	default:
		return nil, fmt.Errorf("handler %s has no profile config builder", s.ID)
	}
	body, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// googlePayConfig builds the Google Pay handler config from the merchant's
// public parameters, using the standard card parameters for everything the
// merchant does not choose.
func googlePayConfig(p map[string]string) GooglePayConfig {
	cfg := DummyGooglePayConfig()
	cfg.Environment = "PRODUCTION"
	if e := p["environment"]; e != "" {
		cfg.Environment = e
	}
	cfg.MerchantInfo = GooglePayMerchantInfo{
		MerchantName:   p["merchant_name"],
		MerchantID:     p["merchant_id"],
		MerchantOrigin: p["merchant_origin"],
	}
	cfg.AllowedPaymentMethods[0].TokenizationSpecification.Parameters = GooglePayTokenizationParameters{
		Gateway:           p["gateway"],
		GatewayMerchantID: p["gateway_merchant_id"],
	}
	return cfg
}
