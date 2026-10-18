// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package operator

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// ManualStepModel is the MongoDB document stored in the control_manual_steps collection.
type ManualStepModel struct {
	ID                   string    `bson:"_id"`
	TargetMerchantID     string    `bson:"target_merchant_id"`
	OperatorUserID       string    `bson:"operator_user_id"`
	EngagementStage      string    `bson:"engagement_stage"` // Stage when captured
	Description          string    `bson:"description"`
	Disposition          string    `bson:"disposition"`
	DaysInStageAtCapture int       `bson:"days_in_stage_at_capture"`
	CreatedAt            time.Time `bson:"created_at"`
	UpdatedAt            time.Time `bson:"updated_at"`
}

func (m *ManualStepModel) ToDomain() domain.ManualStep {
	createdAt := m.CreatedAt
	return domain.ManualStep{
		ManualStepID:         m.ID,
		TargetMerchantID:     domain.MerchantID(m.TargetMerchantID),
		EngagementStage:      domain.EngagementStage(m.EngagementStage),
		Description:          m.Description,
		Disposition:          domain.ManualStepDisposition(m.Disposition),
		CreatedAt:            &createdAt,
		DaysInStageAtCapture: m.DaysInStageAtCapture,
	}
}

// AuditEntryModel is the MongoDB document stored in the control_audit_log collection.
// Append-only: never updated or deleted.
type AuditEntryModel struct {
	ID               string    `bson:"_id"`
	OperatorUserID   string    `bson:"operator_user_id"`
	TargetMerchantID string    `bson:"target_merchant_id"`
	Action           string    `bson:"action"`
	Detail           bson.D    `bson:"detail"`
	OccurredAt       time.Time `bson:"occurred_at"`
}

func (m *AuditEntryModel) ToDomain() domain.AuditEntry {
	// Convert bson.D to map[string]any for the domain type.
	detailMap := make(map[string]any)
	for _, elem := range m.Detail {
		detailMap[elem.Key] = elem.Value
	}
	return domain.AuditEntry{
		EntryID:          m.ID,
		OperatorUserID:   m.OperatorUserID,
		TargetMerchantID: domain.MerchantID(m.TargetMerchantID),
		Action:           m.Action,
		Detail:           detailMap,
		OccurredAt:       m.OccurredAt,
	}
}

// BoardCardModel holds aggregated data for a single merchant's board card.
// This is not a direct collection; it's computed by joining journey, store,
// and account data (see mongoBoardRepo.ListCards).
type BoardCardModel struct {
	MerchantID     string
	StoreID        string
	Name           string
	State          string
	EnteredStateAt time.Time
}

func (m *BoardCardModel) ToDomain(daysInState int, taskCounts domain.TaskCounts) domain.BoardCard {
	stale := daysInState > 14
	blocked := daysInState > 7
	return domain.BoardCard{
		MerchantID:     domain.MerchantID(m.MerchantID),
		StoreID:        domain.StoreID(m.StoreID),
		Name:           m.Name,
		State:          domain.JourneyState(m.State),
		DaysInState:    daysInState,
		OpenTaskCounts: taskCounts,
		Blocked:        blocked,
		Stale:          stale,
	}
}
