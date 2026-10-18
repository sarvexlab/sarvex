// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package operator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	specall "github.com/sarvexlab/sarvex/internal/spec/all"

	"github.com/google/uuid"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/serving"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/store"
)

// JourneyService is minimal interface for fetching store journey state.
type JourneyService interface {
	Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domain.Journey, error)
}

// StoreOwnershipChecker verifies that a store belongs to a merchant, backed
// by control_stores (store.StoreRepository already implements this).
// GetManifest needs it because the serving cache it reads from is keyed by
// store_id alone — the serving plane's read path only ever knows a store,
// never a merchant (architecture §A10.2: discovery/read serves only the
// cache) — so no query on that path can enforce merchant_id itself; this is
// the one place that ownership has to be checked explicitly instead of
// falling out of a compound Mongo filter.
type StoreOwnershipChecker interface {
	Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (*store.StoreModel, error)
}

// defaultCacheTTLSeconds is the TTL the control plane's read-only
// ServingCacheReader is constructed with (see entry.go's serving.NewRedisCache
// call) — reported to the console as ManifestCache.TTLSeconds.
const defaultCacheTTLSeconds = 300

// OperatorService is the production implementation of Service.
type OperatorService struct {
	steps        ManualStepRepository
	audit        AuditLogRepository
	board        BoardRepository
	tasks        TaskService
	journey      JourneyService
	servingCache ServingCacheReader
	presence     PresenceRepository
	orders       OrderReader
	consistency  ConsistencyRepository
	health       HealthRepository
	stores       StoreOwnershipChecker
	bus          eventbus.EventBus
	cacheTTLSec  int
	log          *slog.Logger
}

// NewOperatorService constructs a Service backed by MongoDB repositories,
// a TaskService for open task counts, a read-only ServingCacheReader for the
// presence/manifest views (and, for GetHealth, the sync_freshness
// statement), a PresenceRepository materializing serving cache swaps, an
// OrderReader over the sync plane's order records, a ConsistencyRepository
// materializing reconciliation findings for the health/consistency views, a
// HealthRepository materializing semantic-probe results for the
// health/semantic_endpoint and health/profile_edge statements, a
// StoreOwnershipChecker for verifying store/merchant ownership before
// GetManifest reads the store-only-keyed serving cache, and an
// EventBus for RebuildCache/other operator-triggered commands.
// errServingCacheUnavailable is returned by the presence reads when no
// serving-cache reader is wired. Presence is a view *of* the cache, so
// there is no meaningful answer without one — unlike sync freshness, which
// is one optional statement among several and is simply omitted.
var errServingCacheUnavailable = errors.New("operator: serving cache reader not configured")

func NewOperatorService(
	steps ManualStepRepository,
	audit AuditLogRepository,
	board BoardRepository,
	tasks TaskService,
	journey JourneyService,
	servingCache ServingCacheReader,
	presence PresenceRepository,
	orders OrderReader,
	consistency ConsistencyRepository,
	health HealthRepository,
	stores StoreOwnershipChecker,
	bus eventbus.EventBus,
	log *slog.Logger,
) Service {
	if log == nil {
		log = slog.Default()
	}
	return &OperatorService{
		steps:        steps,
		audit:        audit,
		board:        board,
		tasks:        tasks,
		journey:      journey,
		servingCache: servingCache,
		presence:     presence,
		orders:       orders,
		consistency:  consistency,
		health:       health,
		stores:       stores,
		bus:          bus,
		cacheTTLSec:  defaultCacheTTLSeconds,
		log:          log,
	}
}

func (s *OperatorService) Board(ctx context.Context, state *domain.JourneyState, attention *bool, cursor string, limit int) (domain.ListOf[domain.BoardCard], error) {
	cards, hasMore, err := s.board.ListCards(ctx, state, attention, cursor, limit)
	if err != nil {
		return domain.ListOf[domain.BoardCard]{}, err
	}

	// One batched task lookup for the whole page instead of 3 queries
	// (merchant/sarvex/google) per card: previously up to 3*len(cards)
	// sequential round-trips for a single GET /board call.
	merchantIDs := make([]domain.MerchantID, len(cards))
	for i, card := range cards {
		merchantIDs[i] = domain.MerchantID(card.MerchantID)
	}
	taskSummaries, err := s.tasks.ListOpenByMerchants(ctx, merchantIDs)
	if err != nil {
		s.log.Error("failed to batch-list open tasks for board", "error", err, "card_count", len(cards))
		taskSummaries = nil // best-effort: render the board with zeroed task counts rather than failing it
	}

	items := make([]domain.BoardCard, len(cards))
	for i, card := range cards {
		daysInState := int(time.Now().UTC().Sub(card.EnteredStateAt).Hours() / 24)
		summary := taskSummaries[domain.StoreID(card.StoreID)]

		boardCard := card.ToDomain(daysInState, summary.Counts)
		boardCard.OpenTaskIDs = summary.IDs
		items[i] = boardCard
	}

	var nextCursor *string
	if hasMore && len(cards) > 0 {
		last := cards[len(cards)-1].MerchantID
		nextCursor = &last
	}

	return domain.ListOf[domain.BoardCard]{
		Data: items,
		Page: domain.Page{Cursor: nextCursor, HasMore: hasMore},
	}, nil
}

func (s *OperatorService) CaptureManualStep(ctx context.Context, operatorID string, targetMerchant domain.MerchantID, step domain.ManualStep) (domain.ManualStep, error) {
	if step.Description == "" {
		return domain.ManualStep{}, errors.New("description required")
	}

	now := time.Now().UTC()
	disposition := string(step.Disposition)
	if disposition == "" {
		disposition = string(domain.ManualStepDisposition("unclassified"))
	}

	// NOTE: CaptureManualStep is merchant-scoped but journey is per-store.
	// Without a specific storeID, we cannot fetch the engagement stage.
	// In V1, merchants have one store, but this method doesn't have access to storeID.
	// For now, we skip journey context; future refactoring should make this method per-store.
	engagementStage := ""
	daysInStage := 0

	m := &ManualStepModel{
		ID:                   uuid.NewString(),
		TargetMerchantID:     string(targetMerchant),
		OperatorUserID:       operatorID,
		EngagementStage:      engagementStage,
		DaysInStageAtCapture: daysInStage,
		Description:          step.Description,
		Disposition:          disposition,
		CreatedAt:            now,
		UpdatedAt:            now,
	}

	if err := s.steps.Insert(ctx, m); err != nil {
		return domain.ManualStep{}, err
	}

	// Audit the action.
	auditEntry := &AuditEntryModel{
		ID:               uuid.NewString(),
		OperatorUserID:   operatorID,
		TargetMerchantID: string(targetMerchant),
		Action:           "manual_step.captured",
		OccurredAt:       now,
	}
	if err := s.audit.Append(ctx, auditEntry); err != nil {
		s.log.Error("failed to append audit entry", "error", err, "manual_step_id", m.ID)
		// Don't fail the operation; audit is best-effort.
	}

	return m.ToDomain(), nil
}

func (s *OperatorService) RetagManualStep(ctx context.Context, operatorID string, stepID string, disposition domain.ManualStepDisposition) error {
	if disposition == "" {
		return errors.New("disposition required")
	}

	_, err := s.steps.UpdateDisposition(ctx, stepID, string(disposition))
	if err != nil {
		return err
	}

	// Audit the retag action
	auditEntry := &AuditEntryModel{
		ID:             uuid.NewString(),
		OperatorUserID: operatorID,
		Action:         "manual_step.retagged",
		OccurredAt:     time.Now().UTC(),
		Detail:         nil, // TODO: include detail with step_id and new disposition
	}
	if err := s.audit.Append(ctx, auditEntry); err != nil {
		s.log.Error("failed to append audit entry for retag", "error", err, "step_id", stepID)
		// Don't fail the operation; audit is best-effort.
	}

	return nil
}

func (s *OperatorService) ListManualSteps(ctx context.Context, disposition *domain.ManualStepDisposition, cursor string, limit int) (domain.ListOf[domain.ManualStep], error) {
	var dispStr *string
	if disposition != nil {
		d := string(*disposition)
		dispStr = &d
	}
	steps, hasMore, err := s.steps.ListByDisposition(ctx, dispStr, cursor, limit)
	if err != nil {
		return domain.ListOf[domain.ManualStep]{}, err
	}

	items := make([]domain.ManualStep, len(steps))
	for i, step := range steps {
		items[i] = step.ToDomain()
	}

	var nextCursor *string
	if hasMore && len(steps) > 0 {
		last := steps[len(steps)-1].ID
		nextCursor = &last
	}

	return domain.ListOf[domain.ManualStep]{
		Data: items,
		Page: domain.Page{Cursor: nextCursor, HasMore: hasMore},
	}, nil
}

func (s *OperatorService) AuditLog(ctx context.Context, targetMerchant *domain.MerchantID, cursor string, limit int) (domain.ListOf[domain.AuditEntry], error) {
	entries, hasMore, err := s.audit.List(ctx, targetMerchant, cursor, limit)
	if err != nil {
		return domain.ListOf[domain.AuditEntry]{}, err
	}

	items := make([]domain.AuditEntry, len(entries))
	for i, e := range entries {
		items[i] = e.ToDomain()
	}

	var nextCursor *string
	if hasMore && len(entries) > 0 {
		last := entries[len(entries)-1].ID
		nextCursor = &last
	}

	return domain.ListOf[domain.AuditEntry]{
		Data: items,
		Page: domain.Page{Cursor: nextCursor, HasMore: hasMore},
	}, nil
}

func (s *OperatorService) ListManualStepsFiltered(ctx context.Context, stage *domain.EngagementStage, disposition *domain.ManualStepDisposition, cursor string, limit int) (domain.ListOf[domain.ManualStep], error) {
	if stage == nil || *stage == "" {
		return domain.ListOf[domain.ManualStep]{}, errors.New("engagement stage required for filtered listing")
	}

	var dispStr *string
	if disposition != nil {
		d := string(*disposition)
		dispStr = &d
	}

	steps, hasMore, err := s.steps.ListByStage(ctx, string(*stage), dispStr, cursor, limit)
	if err != nil {
		return domain.ListOf[domain.ManualStep]{}, err
	}

	items := make([]domain.ManualStep, len(steps))
	for i, step := range steps {
		items[i] = step.ToDomain()
	}

	var nextCursor *string
	if hasMore && len(steps) > 0 {
		last := steps[len(steps)-1].ID
		nextCursor = &last
	}

	return domain.ListOf[domain.ManualStep]{
		Data: items,
		Page: domain.Page{Cursor: nextCursor, HasMore: hasMore},
	}, nil
}

func (s *OperatorService) BulkRetagManualSteps(ctx context.Context, operatorID string, stepIDs []string, disposition domain.ManualStepDisposition) error {
	if len(stepIDs) == 0 {
		return errors.New("step_ids required for bulk retag")
	}
	if disposition == "" {
		return errors.New("disposition required")
	}

	updated, err := s.steps.BulkUpdateDisposition(ctx, stepIDs, string(disposition))
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	for _, stepID := range stepIDs {
		auditEntry := &AuditEntryModel{
			ID:             uuid.NewString(),
			OperatorUserID: operatorID,
			Action:         "manual_step.bulk_retagged",
			OccurredAt:     now,
			Detail:         nil,
		}
		if err := s.audit.Append(ctx, auditEntry); err != nil {
			s.log.Error("failed to append audit entry for bulk retag", "error", err, "step_id", stepID)
		}
	}

	s.log.Info("bulk retagged manual steps", "count", updated, "disposition", disposition, "operator_id", operatorID)
	return nil
}

func (s *OperatorService) TimelineMetrics(ctx context.Context, merchantID *domain.MerchantID, startDate, endDate string) ([]domain.TimelineDatapoint, error) {
	agg, err := s.steps.CountByStageAndDay(ctx, merchantID, startDate, endDate)
	if err != nil {
		return nil, err
	}

	result := make([]domain.TimelineDatapoint, len(agg))
	for i, a := range agg {
		result[i] = domain.TimelineDatapoint{
			Date:           a.Date,
			Stage:          domain.EngagementStage(a.Stage),
			Count:          a.Count,
			AvgDaysInStage: int(a.AvgDaysInStage),
		}
	}

	return result, nil
}

// GetPresence assembles the plain-language presence summary from two
// sources: the ServingCacheReader (the served profile + freshness
// watermark) and the PresenceRepository (product_count/built_at, only
// obtainable from the serving.snapshot.swapped events PresenceSubscriber
// materializes — ServingCache itself exposes no count). A store that has
// never completed its first cache swap legitimately has neither yet; that
// is reported as ServingStateNotServing, not an error.
func (s *OperatorService) GetPresence(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domain.Presence, error) {
	rec, err := s.presence.Get(ctx, string(merchantID), string(storeID))
	if errors.Is(err, ErrPresenceNotFound) {
		return domain.Presence{Serving: domain.ServingStateNotServing}, nil
	}
	if err != nil {
		return domain.Presence{}, fmt.Errorf("get presence record: %w", err)
	}

	if s.servingCache == nil {
		return domain.Presence{}, errServingCacheUnavailable
	}

	profile, err := s.servingCache.GetProfile(ctx, string(storeID))
	if errors.Is(err, serving.ErrNotFound) {
		return domain.Presence{Serving: domain.ServingStateNotServing}, nil
	}
	if err != nil {
		return domain.Presence{}, fmt.Errorf("get cached profile: %w", err)
	}

	watermark, err := s.servingCache.Watermark(ctx, string(storeID))
	if err != nil {
		return domain.Presence{}, fmt.Errorf("get cache watermark: %w", err)
	}

	capabilities := make([]string, 0, len(profile.UCP.Capabilities))
	for name := range profile.UCP.Capabilities {
		capabilities = append(capabilities, name)
	}

	servingState := domain.ServingStateActive
	if watermark.IsStale {
		servingState = domain.ServingStateDegraded
	}

	return domain.Presence{
		Summary:      fmt.Sprintf("Agents can see %d products across %d capabilities.", rec.ProductCount, len(capabilities)),
		ProductCount: rec.ProductCount,
		Capabilities: capabilities,
		FreshnessDeclarations: map[string]string{
			"catalog": watermark.SourceSync.Format(time.RFC3339),
		},
		Serving:    servingState,
		ProfileURL: profileURL(profile),
		// profile_reachable reflects whether the serving cache currently has
		// a servable profile — not a live HTTP check of the merchant origin.
		// The real merchant-origin reachability signal is the monitoring
		// loop's profile_edge health statement (GetHealth), a separate,
		// still-unbuilt surface.
		ProfileReachable: true,
		// PublishedVersion: the one version the cached /.well-known/ucp
		// document itself declares (spec-correct — a profile document
		// carries exactly one "ucp.version"). SupportedVersions: everything
		// Sarvex's negotiation engine can actually serve a live response in
		// for this store, per store/per-platform, not just what's baked
		// into the static document — see the spec registry's Servable and
		// internal/pkg/ucp/negotiation's SelectVersion.
		PublishedVersion:  profile.UCP.Version,
		SupportedVersions: supportedVersionStrings(),
	}, nil
}

// supportedVersionStrings converts the servable generations to plain strings for
// domain.Presence.SupportedVersions. Kept as one conversion point so
// presence never drifts from the single source of truth negotiation
// itself selects from.
func supportedVersionStrings() []string {
	servable := specall.Default().Servable()
	out := make([]string, len(servable))
	for i, v := range servable {
		out[i] = v.String()
	}
	return out
}

// profileURL builds the merchant-domain well-known URL from the cached
// profile's storefront URL.
func profileURL(profile *serving.CacheProfile) string {
	if profile.StorefrontURL == "" {
		return ""
	}
	return strings.TrimRight(profile.StorefrontURL, "/") + "/.well-known/ucp"
}

// GetManifest returns the raw manifest as served to agents (the same
// document GetProfile hands back at /.well-known/ucp) plus cache freshness
// state. A store with no cache yet returns an empty manifest with
// Cache.Stale=true rather than an error.
//
// The serving cache this reads from is keyed by store_id alone (it has no
// merchant concept — architecture §A10.2), so ownership must be checked
// here explicitly: without it, any authenticated merchant could read any
// other merchant's manifest by guessing/enumerating a store_id (this was
// exactly F-007's finding). A store that doesn't belong to merchantID reads
// as ErrNotFound, matching the "cross-tenant = not-found, never forbidden"
// convention used throughout this API (see api/activation.go's
// getActivation).
func (s *OperatorService) GetManifest(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domain.ManifestView, error) {
	if s.servingCache == nil {
		return domain.ManifestView{}, errServingCacheUnavailable
	}
	if s.stores != nil {
		if _, err := s.stores.Get(ctx, merchantID, storeID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return domain.ManifestView{}, ErrNotFound
			}
			return domain.ManifestView{}, fmt.Errorf("verify store ownership: %w", err)
		}
	}

	profile, err := s.servingCache.GetProfile(ctx, string(storeID))
	if errors.Is(err, serving.ErrNotFound) {
		return domain.ManifestView{Manifest: json.RawMessage("{}"), Cache: domain.ManifestCache{Stale: true}}, nil
	}
	if err != nil {
		return domain.ManifestView{}, fmt.Errorf("get cached profile: %w", err)
	}

	// Marshal once and hand the bytes straight to the response as
	// json.RawMessage — no need to decode into map[string]any just to
	// re-encode it a moment later when the HTTP handler serializes this
	// ManifestView.
	raw, err := json.Marshal(profile)
	if err != nil {
		return domain.ManifestView{}, fmt.Errorf("marshal served manifest: %w", err)
	}

	watermark, err := s.servingCache.Watermark(ctx, string(storeID))
	if err != nil {
		return domain.ManifestView{}, fmt.Errorf("get cache watermark: %w", err)
	}

	return domain.ManifestView{
		Manifest: json.RawMessage(raw),
		Cache: domain.ManifestCache{
			BuiltAt:         watermark.BuiltAt,
			TTLSeconds:      s.cacheTTLSec,
			Stale:           watermark.IsStale,
			SourceWatermark: watermark.SourceSync,
		},
	}, nil
}

// RebuildCache force-queues a serving-cache rebuild by publishing
// serving.rebuild.requested — the same command internal/planes/sync/mapping.go's
// requestRebuild already publishes for price/stock/re-score triggers, and
// internal/planes/sync/service.go's handleServingRebuildRequested already
// consumes. There is no synchronous job tracking for rebuilds today (mirrors
// ConnectorService.TriggerResync): the returned JobAccepted is a synthesized
// acknowledgement, not a trackable job record.
func (s *OperatorService) RebuildCache(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domain.JobAccepted, error) {
	jobID := uuid.New().String()

	payload, err := json.Marshal(rebuildRequestedPayload{Reason: "operator_requested", Priority: true})
	if err != nil {
		return domain.JobAccepted{}, fmt.Errorf("marshal serving.rebuild.requested: %w", err)
	}

	event := newDomainEvent(ucp.EventRebuildRequested, string(merchantID), string(storeID), payload)
	if err := s.bus.Publish(ctx, event); err != nil {
		s.log.Error("failed to publish rebuild request", "error", err, "store_id", storeID)
		return domain.JobAccepted{}, fmt.Errorf("publish serving.rebuild.requested: %w", err)
	}

	s.log.Info("cache rebuild queued", "store_id", storeID, "job_id", jobID)
	return domain.JobAccepted{JobID: jobID, Status: domain.JobQueued}, nil
}

// ListOrders returns a store's orders from the sync plane's order records
// (OrderReader is read-only; the sync plane's order-intent handler is the
// one writer).
func (s *OperatorService) ListOrders(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, cursor string, limit int, status *domain.OrderStatus) (domain.ListOf[domain.Order], error) {
	return s.orders.List(ctx, storeID, cursor, limit, status)
}

// GetOrder returns a single order with its sync state.
func (s *OperatorService) GetOrder(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, orderID string) (domain.Order, error) {
	return s.orders.Get(ctx, storeID, orderID)
}

// GetHealth reports a store's health as a set of semantic statements
// (contract kinds: consistency, semantic_endpoint, sync_freshness,
// profile_edge — sarvex-v1-control-plane.yaml health_statement).
//
//   - consistency is computed live, rolled up from persisted reconciliation
//     findings (s.consistency — written by monitoring.DiscrepancySubscriber
//     and the scheduled monitoring loop).
//   - semantic_endpoint and profile_edge are read from the latest scheduled
//     semantic-probe run (s.health — written by monitoring.MonitoringLoop,
//     run on an interval by monitoring.Scheduler; see entry.go).
//   - sync_freshness is computed live from the serving cache's own
//     watermark (s.servingCache), the same staleness signal GetPresence
//     already surfaces — no separate persistence needed.
//
// A kind is omitted from the result only if it has never been checked yet
// (e.g. a brand-new store the scheduler hasn't reached its first pass for).
func (s *OperatorService) GetHealth(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domain.Health, error) {
	statements := make([]domain.HealthStatement, 0, 4)

	consistency, err := s.consistencyHealthStatement(ctx, storeID)
	if err != nil {
		return domain.Health{}, fmt.Errorf("consistency health: %w", err)
	}
	statements = append(statements, consistency)

	if s.health != nil {
		probed, err := s.health.GetByStore(ctx, string(storeID))
		if err != nil {
			return domain.Health{}, fmt.Errorf("get probed health statements: %w", err)
		}
		statements = append(statements, probed...)
	}

	if s.servingCache != nil {
		freshness, ok, err := s.syncFreshnessStatement(ctx, storeID)
		if err != nil {
			return domain.Health{}, fmt.Errorf("sync freshness health: %w", err)
		}
		if ok {
			statements = append(statements, freshness)
		}
	}

	return domain.Health{Statements: statements}, nil
}

// consistencyHealthStatement rolls persisted findings up into a single
// statement — always present, since s.consistency is a required dependency.
func (s *OperatorService) consistencyHealthStatement(ctx context.Context, storeID domain.StoreID) (domain.HealthStatement, error) {
	critical, err := s.consistency.CountByStore(ctx, string(storeID), domain.SeverityCritical)
	if err != nil {
		return domain.HealthStatement{}, fmt.Errorf("count critical findings: %w", err)
	}
	warning, err := s.consistency.CountByStore(ctx, string(storeID), domain.SeverityWarning)
	if err != nil {
		return domain.HealthStatement{}, fmt.Errorf("count warning findings: %w", err)
	}

	statement := domain.HealthStatement{
		Kind:      domain.HealthConsistency,
		OK:        critical == 0 && warning == 0,
		Message:   "No consistency findings detected.",
		CheckedAt: time.Now().UTC(),
		// Counts as data, so a client can pluralize in its own language
		// instead of inheriting this function's "finding(s)".
		Params: map[string]any{
			"critical_count": critical,
			"warning_count":  warning,
		},
	}
	switch {
	case critical > 0:
		statement.Message = fmt.Sprintf("%d critical consistency finding(s) detected.", critical)
	case warning > 0:
		statement.Message = fmt.Sprintf("%d warning-level consistency finding(s) detected.", warning)
	}
	return statement, nil
}

// syncFreshnessStatement reports whether the serving cache's last swap is
// within the freshness window. ok is false when the store has no cache
// snapshot yet (ErrNotFound) — not an error, just nothing to report.
func (s *OperatorService) syncFreshnessStatement(ctx context.Context, storeID domain.StoreID) (domain.HealthStatement, bool, error) {
	// No cache reader wired is the same answer as no snapshot yet: there is
	// nothing to say about serving freshness, which is exactly what the
	// ok=false return means. A deployment or test harness without a Redis
	// cache loses this one statement rather than panicking on a nil reader.
	if s.servingCache == nil {
		return domain.HealthStatement{}, false, nil
	}

	watermark, err := s.servingCache.Watermark(ctx, string(storeID))
	if errors.Is(err, serving.ErrNotFound) {
		return domain.HealthStatement{}, false, nil
	}
	if err != nil {
		return domain.HealthStatement{}, false, fmt.Errorf("get cache watermark: %w", err)
	}

	statement := domain.HealthStatement{
		Kind:      domain.HealthSyncFreshness,
		OK:        !watermark.IsStale,
		CheckedAt: time.Now().UTC(),
	}
	// The age as a number, because Duration.String() renders nanosecond
	// precision ("2h0m58.304129489s") that no merchant should ever be shown.
	statement.Params = map[string]any{"age_seconds": int(watermark.Age.Seconds())}
	if watermark.IsStale {
		statement.Message = fmt.Sprintf("Served data is stale (source sync %s old).", watermark.Age)
	} else {
		statement.Message = "Served data is fresh."
	}
	return statement, true, nil
}

// ListConsistencyFindings returns persisted reconciliation findings for a
// store, most-recently-detected first.
func (s *OperatorService) ListConsistencyFindings(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, cursor string, limit int, severity *domain.Severity) (domain.ListOf[domain.ConsistencyFinding], error) {
	findings, hasMore, err := s.consistency.ListByStore(ctx, string(storeID), cursor, limit, severity)
	if err != nil {
		return domain.ListOf[domain.ConsistencyFinding]{}, fmt.Errorf("list consistency findings: %w", err)
	}

	items := make([]domain.ConsistencyFinding, len(findings))
	for i, f := range findings {
		items[i] = *f
	}

	var nextCursor *string
	if hasMore && len(findings) > 0 {
		last := findings[len(findings)-1].DetectedAt.Format(time.RFC3339Nano)
		nextCursor = &last
	}

	return domain.ListOf[domain.ConsistencyFinding]{
		Data: items,
		Page: domain.Page{Cursor: nextCursor, HasMore: hasMore},
	}, nil
}

// ActOnBehalf executes an operator action on behalf of a merchant.
func (s *OperatorService) ActOnBehalf(ctx context.Context, operatorID string, targetMerchant domain.MerchantID, action string, params map[string]interface{}) error {
	now := time.Now().UTC()

	switch action {
	case "resolve_task":
		// Resolve a task on behalf of the merchant.
		taskID, ok := params["task_id"].(string)
		if !ok || taskID == "" {
			return errors.New("task_id required for resolve_task action")
		}
		// Note: this is a stub; actual implementation would use the Task service.
		// For now, just audit the action.
		s.log.Debug("operator resolved task on behalf of merchant", "task_id", taskID, "merchant_id", targetMerchant, "operator_id", operatorID)

	case "unblock":
		// Unblock a merchant (e.g., clear a blocker).
		reason, ok := params["reason"].(string)
		if !ok || reason == "" {
			reason = "operator_unblock"
		}
		s.log.Debug("operator unblocked merchant", "merchant_id", targetMerchant, "operator_id", operatorID, "reason", reason)

	case "acknowledge_issue":
		// Acknowledge an issue without resolution (e.g., for monitoring alerts).
		issueType, ok := params["issue_type"].(string)
		if !ok || issueType == "" {
			issueType = "unknown"
		}
		s.log.Debug("operator acknowledged issue", "merchant_id", targetMerchant, "operator_id", operatorID, "issue_type", issueType)

	default:
		return errors.New("unknown action: " + action)
	}

	// Audit the action.
	auditEntry := &AuditEntryModel{
		ID:               uuid.NewString(),
		OperatorUserID:   operatorID,
		TargetMerchantID: string(targetMerchant),
		Action:           "elevation." + action,
		OccurredAt:       now,
	}
	if err := s.audit.Append(ctx, auditEntry); err != nil {
		s.log.Error("failed to audit elevation action", "error", err, "action", action, "merchant_id", targetMerchant)
		// Don't fail the operation; audit is best-effort.
	}

	return nil
}

var _ Service = (*OperatorService)(nil)
