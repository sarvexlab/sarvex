// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"time"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// StoreModel is the MongoDB document stored in control_stores collection.
// _id is the store_id — one document per store.
type StoreModel struct {
	ID             string             `bson:"_id"`
	MerchantID     string             `bson:"merchant_id"` // For tenant isolation
	ConnectorID    string             `bson:"connector_id"`
	Platform       domain.Platform    `bson:"platform"`
	DisplayName    string             `bson:"display_name"`
	Status         domain.StoreStatus `bson:"status"`
	LastSyncAt     *time.Time         `bson:"last_sync_at,omitempty"`
	ReadinessScore *float32           `bson:"readiness_score,omitempty"`
	CreatedAt      time.Time          `bson:"created_at"`
	UpdatedAt      time.Time          `bson:"updated_at"`
}

// ToDomain converts the model to a domain.Store.
func (m *StoreModel) ToDomain() domain.Store {
	return domain.Store{
		StoreID:        domain.StoreID(m.ID),
		MerchantID:     domain.MerchantID(m.MerchantID),
		ConnectorID:    m.ConnectorID,
		Platform:       m.Platform,
		DisplayName:    m.DisplayName,
		Status:         m.Status,
		LastSyncAt:     m.LastSyncAt,
		ReadinessScore: m.ReadinessScore,
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
	}
}
