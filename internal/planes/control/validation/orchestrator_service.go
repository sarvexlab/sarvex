// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"

	"github.com/samber/lo"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// OrchestratorService adapts Orchestrator to the Service interface.
// It wraps validation runs as asynchronous jobs for API consumers.
type OrchestratorService struct {
	orchestrator *Orchestrator
}

// NewOrchestratorService creates a Service backed by an Orchestrator.
func NewOrchestratorService(orchestrator *Orchestrator) Service {
	return &OrchestratorService{
		orchestrator: orchestrator,
	}
}

// GetStatus returns the current validation state for a store.
func (s *OrchestratorService) GetStatus(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domain.ValidationStatus, error) {
	status, err := s.orchestrator.GetStatus(ctx, merchantID, storeID)
	if err != nil {
		return domain.ValidationStatus{}, fmt.Errorf("get status: %w", err)
	}
	if status == nil {
		return domain.ValidationStatus{}, nil
	}
	return *status, nil
}

// TriggerRun starts a validation run for a specific store.
// The run executes synchronously; returns the run ID as a completed job.
func (s *OrchestratorService) TriggerRun(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domain.JobAccepted, error) {
	record, err := s.orchestrator.RunValidation(ctx, merchantID, storeID)
	if err != nil {
		return domain.JobAccepted{}, fmt.Errorf("run validation: %w", err)
	}

	// Return the run as an accepted job.
	// Status is "completed" since validation is synchronous in V1.
	return domain.JobAccepted{
		JobID:  record.RunID,
		Status: domain.JobStatus("completed"),
	}, nil
}

// ListRuns returns paginated conformance records for a specific store.
func (s *OrchestratorService) ListRuns(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, cursor string, limit int) (domain.ListOf[domain.ConformanceRecord], error) {
	records, nextCursor, err := s.orchestrator.ListRuns(ctx, string(storeID), limit, cursor)
	if err != nil {
		return domain.ListOf[domain.ConformanceRecord]{}, fmt.Errorf("list runs: %w", err)
	}

	// Convert pointers to values
	data := lo.Map(records, func(r *domain.ConformanceRecord, _ int) domain.ConformanceRecord {
		if r == nil {
			return domain.ConformanceRecord{}
		}
		return *r
	})

	return domain.ListOf[domain.ConformanceRecord]{
		Data: data,
		Page: domain.Page{
			Cursor:  nextCursor,
			HasMore: nextCursor != nil && *nextCursor != "",
		},
	}, nil
}

var _ Service = (*OrchestratorService)(nil)
