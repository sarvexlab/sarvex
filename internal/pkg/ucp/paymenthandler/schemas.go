// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package paymenthandler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/domain"
)

// PaymentHandlerSchemaRegistry maps payment handler types to their config schema URLs and cached schemas.
type PaymentHandlerSchemaRegistry struct {
	schemas map[string]json.RawMessage
}

// paymentHandlerSchemaURL maps each payment handler type — keyed by its
// reverse-domain name (domain.PaymentHandlerType), the key every caller looks
// it up by — to its config schema URL.
var paymentHandlerSchemaURL = map[string]string{
	// "stripe":     "https://woocommerce.com/ucp/stripe-handler/2026-04-08/config.json",
	// "paypal":     "https://woocommerce.com/ucp/paypal-handler/2026-04-08/config.json",
	// "adyen":      "https://magento.com/ucp/adyen-handler/2026-04-08/config.json",
	// "braintree":  "https://magento.com/ucp/braintree-handler/2026-04-08/config.json",
	string(domain.GooglePay): "https://pay.google.com/gp/p/ucp/2026-01-23/schemas/config.json",
}

// schemaFetchTimeout bounds each schema fetch: the registry is built while the
// catalogue loads, so an unresponsive schema host must not stall startup.
const schemaFetchTimeout = 5 * time.Second

var schemaHTTPClient = &http.Client{Timeout: schemaFetchTimeout}

var (
	once                 sync.Once
	cachedSchemaRegistry PaymentHandlerSchemaRegistry
)

// NewPaymentHandlerSchemaRegistry returns a registry of payment handler config schemas.
// Schemas are fetched from their respective URLs on first call and cached for subsequent calls.
func NewPaymentHandlerSchemaRegistry() PaymentHandlerSchemaRegistry {
	once.Do(func() {
		cachedSchemaRegistry.schemas = make(map[string]json.RawMessage)
		for handlerType, schemaURL := range paymentHandlerSchemaURL {
			schema, err := fetchSchema(schemaURL)
			if err != nil {
				// If fetch fails, store empty schema to allow graceful degradation
				cachedSchemaRegistry.schemas[handlerType] = nil
				continue
			}
			cachedSchemaRegistry.schemas[handlerType] = schema
		}
	})
	return cachedSchemaRegistry
}

// fetchSchema fetches a JSON schema from a URL and returns it as json.RawMessage.
func fetchSchema(url string) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), schemaFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := schemaHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// An error page is not a schema; caching it would make Get report a
	// "schema" that fails every validation.
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch schema %s: status %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return json.RawMessage(body), nil
}

// Get returns the schema for a given payment handler type.
// Returns (nil, false) if the type is not found or schema fetch failed.
func (pr PaymentHandlerSchemaRegistry) Get(handlerType string) (json.RawMessage, bool) {
	schema, ok := pr.schemas[handlerType]
	return schema, ok && len(schema) > 0
}
