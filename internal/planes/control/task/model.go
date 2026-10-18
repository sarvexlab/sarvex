// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package task

import (
	"time"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

type TaskModel struct {
	UpdatedAt      time.Time                  `bson:"updated_at"`
	CreatedAt      time.Time                  `bson:"created_at"`
	SourceRef      *string                    `bson:"source_ref,omitempty"`
	ResolutionNote *string                    `bson:"resolution_note,omitempty"`
	Finding        *domain.ConsistencyFinding `bson:"finding,omitempty"`
	Resolution     string                     `bson:"resolution"`
	ID             string                     `bson:"_id"`
	State          string                     `bson:"state"`
	Source         string                     `bson:"source"`
	Owner          string                     `bson:"owner"`
	WhyItMatters   string                     `bson:"why_it_matters"`
	Title          string                     `bson:"title"`
	StoreID        string                     `bson:"store_id"`
	MerchantID     string                     `bson:"merchant_id"`
	BlocksCheckout bool                       `bson:"blocks_checkout"`
}

func (m *TaskModel) ToDomain() domain.Task {
	createdAt := m.CreatedAt
	updatedAt := m.UpdatedAt
	return domain.Task{
		TaskID:         m.ID,
		StoreID:        domain.StoreID(m.StoreID),
		Title:          m.Title,
		WhyItMatters:   m.WhyItMatters,
		Owner:          domain.TaskOwner(m.Owner),
		Resolution:     domain.TaskResolution(m.Resolution),
		State:          domain.TaskState(m.State),
		BlocksCheckout: m.BlocksCheckout,
		Source:         domain.TaskSource(m.Source),
		SourceRef:      m.SourceRef,
		Finding:        m.Finding,
		Note:           m.ResolutionNote,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
	}
}
