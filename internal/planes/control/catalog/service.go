// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package catalog manages product readiness, scoring, and remediations.
package catalog

import (
	"context"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// Service provides read access to catalog readiness and triggers remediations.
type Service interface {
	GetReadiness(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domain.Readiness, error)
	ListProducts(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, cursor string, limit int, minScore *int, hasBlockingIssue *bool) (domain.ListOf[domain.ProductSummary], error)

	GetProduct(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, sku string) (domain.ProductDetail, error)
	TriggerAudit(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domain.JobAccepted, error)

	ListRemediations(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, cursor string, limit int, remediationType *string) (domain.ListOf[domain.Remediation], error)
	ApplyRemediations(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, req domain.ApplyRemediationsRequest) (domain.ApplyRemediationsResponse, error)
	RevertWriteBack(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, writeBackID string) (domain.RevertResponse, error)

	// ListRemediationsForSKU returns the not-yet-applied remediations for a
	// single product. Used by the task service to attach a typed drill-in
	// payload to readiness-sourced tasks (task.source_ref == sku).
	ListRemediationsForSKU(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, sku string) ([]domain.Remediation, error)

	// ListIssuesForSKU returns the open issues on a single product that have
	// no rule-based fix (remediable=manual). Used by the task service to
	// round out a readiness-sourced task's typed drill-in once
	// ListRemediationsForSKU is exhausted (task.source_ref == sku).
	ListIssuesForSKU(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, sku string) ([]domain.ProductIssue, error)
}
