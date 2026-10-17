// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"fmt"
	"log/slog"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/wire"

	"github.com/samber/lo"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/catalogue"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/paymenthandler"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// PaymentValidator validates payment handler configuration against the catalogue.
// Ensures only valid handlers are enabled and required config fields are present.
type PaymentValidator struct {
	catalogue catalogue.Catalogue
	log       *slog.Logger
}

// NewPaymentValidator creates a payment handler validator.
func NewPaymentValidator(cat catalogue.Catalogue, log *slog.Logger) *PaymentValidator {
	return &PaymentValidator{
		catalogue: cat,
		log:       log,
	}
}

// ValidateConfig validates the payment handler configuration.
// Every handler must have a spec (and be in the catalogue when one is loaded),
// may carry only the parameters its spec declares public, and — when enabled —
// must carry all required ones: an enabled handler an agent cannot use is not
// a configured handler.
func (pv *PaymentValidator) ValidateConfig(config *domain.PaymentHandlerConfig) error {
	var inCatalogue map[string]bool
	if pv.catalogue == nil {
		pv.log.Warn("catalogue not available, skipping payment handler catalogue membership check")
	} else {
		inCatalogue = lo.SliceToMap(pv.catalogue.PaymentHandlers(), func(h wire.PaymentHandler) (string, bool) { return h.ID, true })
	}

	for _, h := range config.Handlers {
		spec, ok := paymenthandler.Lookup(h.HandlerID)
		if !ok {
			return fmt.Errorf("invalid payment handler: %s", h.HandlerID)
		}
		if inCatalogue != nil && !inCatalogue[string(spec.Name)] {
			return fmt.Errorf("invalid payment handler: %s (not in catalogue)", h.HandlerID)
		}

		if err := spec.ValidateParams(h.Config); err != nil {
			return err
		}
		if h.Enabled {
			if err := spec.ValidateEnabled(h.Config); err != nil {
				return err
			}
		}

		pv.log.Debug("payment handler valid", "handler_id", h.HandlerID, "store_id", config.StoreID)
	}

	return nil
}
