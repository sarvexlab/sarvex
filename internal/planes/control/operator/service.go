// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package operator provides operator-only surfaces: the merchant board, manual
// step capture, audit log, and the UCP presence/health views.
package operator

import (
	"context"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// Service is the operator-elevation surface for cross-merchant and admin actions.
type Service interface {
	// Board returns all merchants sorted by journey state for operator triage.
	Board(ctx context.Context, state *domain.JourneyState, attention *bool, cursor string, limit int) (domain.ListOf[domain.BoardCard], error)

	// CaptureManualStep records a hand-performed action for the Phase B backlog.
	// The operatorID is logged in the audit entry.
	CaptureManualStep(ctx context.Context, operatorID string, targetMerchant domain.MerchantID, step domain.ManualStep) (domain.ManualStep, error)

	// ListManualSteps returns manual steps filtered by disposition (Phase 10A).
	ListManualSteps(ctx context.Context, disposition *domain.ManualStepDisposition, cursor string, limit int) (domain.ListOf[domain.ManualStep], error)

	// RetagManualStep updates a manual step's disposition and audits the action (Phase 10A).
	RetagManualStep(ctx context.Context, operatorID string, stepID string, disposition domain.ManualStepDisposition) error

	// ListManualStepsFiltered returns manual steps filtered by engagement stage and disposition (Phase 10C+).
	ListManualStepsFiltered(ctx context.Context, stage *domain.EngagementStage, disposition *domain.ManualStepDisposition, cursor string, limit int) (domain.ListOf[domain.ManualStep], error)

	// BulkRetagManualSteps updates multiple manual steps' dispositions in one operation (Phase 10C+).
	BulkRetagManualSteps(ctx context.Context, operatorID string, stepIDs []string, disposition domain.ManualStepDisposition) error

	// TimelineMetrics returns manual step counts by engagement stage and date for visualization (Phase 10C+).
	TimelineMetrics(ctx context.Context, merchantID *domain.MerchantID, startDate, endDate string) ([]domain.TimelineDatapoint, error)

	// AuditLog returns the append-only record of operator mutations.
	AuditLog(ctx context.Context, targetMerchant *domain.MerchantID, cursor string, limit int) (domain.ListOf[domain.AuditEntry], error)

	// Presence and health read-paths (per-store, scoped to storeID).
	GetPresence(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domain.Presence, error)
	GetManifest(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domain.ManifestView, error)
	RebuildCache(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domain.JobAccepted, error)

	// Orders view (per-store).
	ListOrders(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, cursor string, limit int, status *domain.OrderStatus) (domain.ListOf[domain.Order], error)
	GetOrder(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, orderID string) (domain.Order, error)

	// Health view (per-store).
	GetHealth(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domain.Health, error)
	ListConsistencyFindings(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, cursor string, limit int, severity *domain.Severity) (domain.ListOf[domain.ConsistencyFinding], error)

	// ActOnBehalf executes an operator action on behalf of a merchant (elevation).
	// action: "unblock", "resolve_task", "acknowledge_issue", etc.
	ActOnBehalf(ctx context.Context, operatorID string, targetMerchant domain.MerchantID, action string, params map[string]interface{}) error
}
