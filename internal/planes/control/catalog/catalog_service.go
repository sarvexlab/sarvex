// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/samber/lo"

	pkgdomain "github.com/sarvexlabs/sarvex.cp/internal/pkg/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// CatalogService is the production implementation of Service.
//
// Read paths (readiness, products, remediations) read from the catalog store,
// which is owned and written by the sync plane's scoring workers (§4.3).
//
// Write paths are limited to what the control plane owns:
//   - WriteBackRepository (audit log) for ApplyRemediations / RevertWriteBack
//   - AuditJobRepository for TriggerAudit
//
// Cross-plane commands (telling the sync plane to act) go through CommandEmitter.
// EventBusCommandEmitter is wired in control/api/entry.go (Phase 5).
type CatalogService struct {
	readiness    ReadinessRepository
	products     ProductRepository
	remediations RemediationRepository
	writeBacks   WriteBackRepository
	audits       AuditJobRepository
	commands     CommandEmitter
}

// NewCatalogService constructs a Service backed by MongoDB and a CommandEmitter.
// CommandEmitter should be EventBusCommandEmitter for event-driven sync plane communication.
func NewCatalogService(
	readiness ReadinessRepository,
	products ProductRepository,
	remediations RemediationRepository,
	writeBacks WriteBackRepository,
	audits AuditJobRepository,
	commands CommandEmitter,
) Service {
	return &CatalogService{
		readiness:    readiness,
		products:     products,
		remediations: remediations,
		writeBacks:   writeBacks,
		audits:       audits,
		commands:     commands,
	}
}

// GetReadiness returns the store's Agent Readiness Score. A missing record
// (store not yet scored) is not an error — it returns a zero-value Readiness.
func (s *CatalogService) GetReadiness(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domain.Readiness, error) {
	m, err := s.readiness.Get(ctx, merchantID, storeID)
	if errors.Is(err, ErrNotFound) {
		return domain.Readiness{}, nil
	}
	if err != nil {
		return domain.Readiness{}, fmt.Errorf("get readiness for %s:%s: %w", merchantID, storeID, err)
	}
	return m.ToDomain(), nil
}

// ListProducts returns a paginated list of product summaries for the store.
func (s *CatalogService) ListProducts(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, cursor string, limit int, minScore *int, hasBlockingIssue *bool) (domain.ListOf[domain.ProductSummary], error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	products, hasMore, err := s.products.List(ctx, merchantID, storeID, cursor, limit, minScore, hasBlockingIssue)
	if err != nil {
		return domain.ListOf[domain.ProductSummary]{}, err
	}

	items := lo.Map(products, func(p *ProductModel, _ int) domain.ProductSummary { return p.ToSummary() })

	var nextCursor *string
	if hasMore && len(products) > 0 {
		last := products[len(products)-1].SKU
		nextCursor = &last
	}

	return domain.ListOf[domain.ProductSummary]{
		Data: items,
		Page: domain.Page{Cursor: nextCursor, HasMore: hasMore},
	}, nil
}

// GetProduct returns the full detail for a single product including per-dimension
// scores, issues, and attribute mappings.
func (s *CatalogService) GetProduct(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, sku string) (domain.ProductDetail, error) {
	m, err := s.products.Get(ctx, merchantID, storeID, sku)
	if err != nil {
		return domain.ProductDetail{}, err
	}
	return m.ToDetail(), nil
}

// TriggerAudit enqueues a catalog re-scan for the store. The job is written
// to control_catalog_audits and the sync plane is signalled via CommandEmitter.
func (s *CatalogService) TriggerAudit(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domain.JobAccepted, error) {
	jobID := uuid.New().String()
	job := &AuditJobModel{
		ID:         jobID,
		MerchantID: string(merchantID),
		StoreID:    string(storeID),
		Status:     string(domain.JobQueued),
		CreatedAt:  time.Now().UTC(),
	}
	if err := s.audits.Insert(ctx, job); err != nil {
		return domain.JobAccepted{}, fmt.Errorf("insert audit job for %s:%s: %w", merchantID, storeID, err)
	}

	// Signal the sync plane. Noop until G3 — see commands.go.
	if err := s.commands.RequestAudit(ctx, pkgdomain.MerchantID(merchantID), string(storeID), jobID); err != nil {
		return domain.JobAccepted{}, fmt.Errorf("emit audit requested for %s:%s: %w", merchantID, storeID, err)
	}

	return domain.JobAccepted{JobID: jobID, Status: domain.JobQueued}, nil
}

// ListRemediations returns a paginated list of available rule-based fixes for
// the store's products.
func (s *CatalogService) ListRemediations(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, cursor string, limit int, remediationType *string) (domain.ListOf[domain.Remediation], error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	rems, hasMore, err := s.remediations.List(ctx, merchantID, storeID, cursor, limit, remediationType)
	if err != nil {
		return domain.ListOf[domain.Remediation]{}, err
	}

	items := lo.Map(rems, func(r *RemediationModel, _ int) domain.Remediation { return r.ToDomain() })

	var nextCursor *string
	if hasMore && len(rems) > 0 {
		last := rems[len(rems)-1].ID
		nextCursor = &last
	}

	return domain.ListOf[domain.Remediation]{
		Data: items,
		Page: domain.Page{Cursor: nextCursor, HasMore: hasMore},
	}, nil
}

// ApplyRemediations applies a batch of rule-based fixes. The batch is all-or-
// nothing: if any remediation has already been applied, the whole request fails.
//
// Control-plane writes: a WriteBack record in the audit log. The fixes
// themselves, and marking the records applied, are the sync plane's: it owns
// the remediation records and acts on the ApplyRemediation command.
func (s *CatalogService) ApplyRemediations(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, req domain.ApplyRemediationsRequest) (domain.ApplyRemediationsResponse, error) {
	rems, err := s.remediations.GetByIDs(ctx, merchantID, storeID, req.RemediationIDs)
	if errors.Is(err, ErrNotFound) {
		return domain.ApplyRemediationsResponse{}, ErrNotFound
	}
	if err != nil {
		return domain.ApplyRemediationsResponse{}, fmt.Errorf("get remediations for %s:%s: %w", merchantID, storeID, err)
	}

	if lo.SomeBy(rems, func(r *RemediationModel) bool { return r.Applied }) {
		return domain.ApplyRemediationsResponse{}, ErrRemediationApplied
	}

	writeBackID := uuid.New().String()
	wb := &WriteBackModel{
		ID:             writeBackID,
		MerchantID:     string(merchantID),
		StoreID:        string(storeID),
		RemediationIDs: req.RemediationIDs,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	if err := s.writeBacks.Insert(ctx, wb); err != nil {
		return domain.ApplyRemediationsResponse{}, fmt.Errorf("insert write-back for %s:%s: %w", merchantID, storeID, err)
	}

	// Signal sync plane to push fixes to catalog store and merchant store.
	if err := s.commands.ApplyRemediation(ctx, pkgdomain.MerchantID(merchantID), string(storeID), writeBackID, req.RemediationIDs); err != nil {
		return domain.ApplyRemediationsResponse{}, fmt.Errorf("emit remediations applied for %s:%s: %w", merchantID, storeID, err)
	}

	return domain.ApplyRemediationsResponse{
		Applied:      len(req.RemediationIDs),
		WriteBackIDs: []string{writeBackID},
	}, nil
}

// ListRemediationsForSKU returns the not-yet-applied remediations for a
// single product.
func (s *CatalogService) ListRemediationsForSKU(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, sku string) ([]domain.Remediation, error) {
	rems, err := s.remediations.ListUnappliedBySKU(ctx, merchantID, storeID, sku)
	if err != nil {
		return nil, fmt.Errorf("list unapplied remediations for %s:%s sku %s: %w", merchantID, storeID, sku, err)
	}

	items := lo.Map(rems, func(r *RemediationModel, _ int) domain.Remediation { return r.ToDomain() })
	return items, nil
}

// ListIssuesForSKU returns the open issues on a single product that have no
// rule-based fix — the manual-fix issues surfaced in a readiness task's
// typed drill-in once ListRemediationsForSKU is exhausted.
func (s *CatalogService) ListIssuesForSKU(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, sku string) ([]domain.ProductIssue, error) {
	m, err := s.products.Get(ctx, merchantID, storeID, sku)
	if err != nil {
		return nil, fmt.Errorf("get product for %s:%s sku %s: %w", merchantID, storeID, sku, err)
	}

	issues := m.ToDetail().Issues
	manual := lo.Filter(issues, func(iss domain.ProductIssue, _ int) bool { return iss.Remediable == domain.RemediableByManual })
	return manual, nil
}

// RevertWriteBack reverts a previously applied batch of remediations.
//
// Control-plane writes: the write-back record is marked reverted. Undoing the
// fixes — restoring each field's prior value on the product and in the
// merchant's store, and reopening the records — is the sync plane's, on the
// RevertRemediation command. The command is sent before the record is marked,
// so a failure to send leaves the write-back revertible; the sync plane's
// handler is idempotent, so a repeat is harmless.
func (s *CatalogService) RevertWriteBack(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, writeBackID string) (domain.RevertResponse, error) {
	wb, err := s.writeBacks.Get(ctx, merchantID, storeID, writeBackID)
	if errors.Is(err, ErrWriteBackNotFound) {
		return domain.RevertResponse{}, ErrWriteBackNotFound
	}
	if err != nil {
		return domain.RevertResponse{}, fmt.Errorf("get write-back %s for %s:%s: %w", writeBackID, merchantID, storeID, err)
	}

	if wb.Reverted {
		return domain.RevertResponse{Reverted: true}, nil
	}

	if err := s.commands.RevertRemediation(ctx, pkgdomain.MerchantID(merchantID), string(storeID), writeBackID, wb.RemediationIDs); err != nil {
		return domain.RevertResponse{}, fmt.Errorf("emit revert for write-back %s for %s:%s: %w", writeBackID, merchantID, storeID, err)
	}
	if err := s.writeBacks.MarkReverted(ctx, merchantID, storeID, writeBackID); err != nil {
		return domain.RevertResponse{}, fmt.Errorf("mark write-back %s reverted for %s:%s: %w", writeBackID, merchantID, storeID, err)
	}

	return domain.RevertResponse{Reverted: true}, nil
}

var _ Service = (*CatalogService)(nil)
