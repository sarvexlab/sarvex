// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package journey

import (
	"time"

	"github.com/samber/lo"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// JourneyModel is the MongoDB document stored in the control_journey collection.
// _id is the store_id — one document per store. MerchantID is denormalized onto
// every document so reads can be scoped by (merchant_id, store_id) together —
// per CLAUDE.md invariant 4, a store_id alone must never be a sufficient key for
// a tenant-scoped read.
type JourneyModel struct {
	ID             string               `bson:"_id"`
	MerchantID     string               `bson:"merchant_id"`
	State          string               `bson:"state"`
	History        []JourneyHistoryItem `bson:"history"`
	Stage          string               `bson:"stage"`
	EnteredStateAt time.Time            `bson:"entered_state_at"`
	EnteredStageAt time.Time            `bson:"entered_stage_at"`
	CreatedAt      time.Time            `bson:"created_at"`
	UpdatedAt      time.Time            `bson:"updated_at"`
}

// JourneyHistoryItem is one entry in the append-only state history.
type JourneyHistoryItem struct {
	State     string    `bson:"state"`
	EnteredAt time.Time `bson:"entered_at"`
}

func (m *JourneyModel) ToDomain() domain.Journey {
	history := lo.Map(m.History, func(h JourneyHistoryItem, _ int) domain.JourneyHistoryEntry {
		return domain.JourneyHistoryEntry{
			State:     domain.JourneyState(h.State),
			EnteredAt: h.EnteredAt,
		}
	})
	return domain.Journey{
		State:          domain.JourneyState(m.State),
		History:        history,
		Stage:          domain.EngagementStage(m.Stage),
		EnteredStateAt: m.EnteredStateAt,
		EnteredStageAt: m.EnteredStageAt,
	}
}
