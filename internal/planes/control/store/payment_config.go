// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"errors"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// PaymentConfigRepository is the persistence interface for per-store payment handler configuration.
type PaymentConfigRepository interface {
	// Get returns the payment config for a store, or ErrNotFound if not configured.
	Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (*PaymentConfigModel, error)

	// Upsert creates or replaces the payment config for a store.
	Upsert(ctx context.Context, m *PaymentConfigModel) error

	// Delete removes the payment config for a store.
	Delete(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) error
}

var ErrPaymentConfigNotFound = errors.New("payment config not found")

// PaymentConfigModel is the MongoDB document for payment handler configuration.
type PaymentConfigModel struct {
	ID         string                               `bson:"_id"`         // StoreID
	MerchantID string                               `bson:"merchant_id"` // Tenant isolation
	Handlers   map[string]PaymentHandlerConfigModel `bson:"handlers"`    // Handler ID → config
	UpdatedAt  interface{}                          `bson:"updated_at"`  // timestamp
	UpdatedBy  string                               `bson:"updated_by"`  // who made the change
}

// PaymentHandlerConfigModel is a sub-document for each handler config.
type PaymentHandlerConfigModel struct {
	Enabled bool              `bson:"enabled"`
	Config  map[string]string `bson:"config,omitempty"`
}

// ToDomain converts the model to a domain.PaymentHandlerConfig.
func (m *PaymentConfigModel) ToDomain() domain.PaymentHandlerConfig {
	handlers := make([]domain.EnabledHandler, 0)
	for handlerID, config := range m.Handlers {
		handlers = append(handlers, domain.EnabledHandler{
			HandlerID: handlerID,
			Enabled:   config.Enabled,
			Config:    config.Config,
		})
	}

	return domain.PaymentHandlerConfig{
		StoreID:   domain.StoreID(m.ID),
		Handlers:  handlers,
		UpdatedBy: m.UpdatedBy,
	}
}
