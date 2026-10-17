// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/eventbus"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/safego"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp"
)

// CatalogQueryKind identifies which discovery read operation produced a
// CatalogQueryRecordedEvent.
type CatalogQueryKind string

const (
	CatalogQuerySearch CatalogQueryKind = "search"
	CatalogQueryLookup CatalogQueryKind = "lookup"
)

// CatalogQueryRecordedEvent closes the emission gap identified in the Agent
// Behavior Insights audit: CatalogService.Search/Lookup (the discovery read
// path, architecture invariant #2 — cache only) previously emitted nothing
// describing what an agent actually asked for. This event carries the
// structured, derived signal only — matched product ids, query kind,
// surface — and deliberately never the raw free-text `q=` search string:
// CLAUDE.md's privacy invariant and R12 of the Behavior Insights task both
// require that no free-text query content be persisted, so it is simply
// never captured here rather than sanitized after the fact.
type CatalogQueryRecordedEvent struct {
	EventID       string           `json:"event_id"`
	StoreID_      string           `json:"store_id"`
	MerchantID_   string           `json:"merchant_id"`
	Kind          CatalogQueryKind `json:"kind"`
	Surface       Surface          `json:"surface"`
	AgentPlatform *string          `json:"agent_platform"`
	// RequestedProductIDs are the product/item identifiers this request
	// resolved against: the returned page's SKUs for a search, or the
	// looked-up item id for a lookup (recorded even when the lookup misses
	// — an agent asking for an id the catalog doesn't have is still a real,
	// honest "requested" signal, not a fabricated one).
	RequestedProductIDs []string  `json:"requested_product_ids"`
	OccurredAt_         time.Time `json:"occurred_at"`
	Payload_            []byte    `json:"-"`
}

func (e *CatalogQueryRecordedEvent) EventType() string     { return ucp.EventCatalogQueryRecorded }
func (e *CatalogQueryRecordedEvent) StoreID() string       { return e.StoreID_ }
func (e *CatalogQueryRecordedEvent) MerchantID() string    { return e.MerchantID_ }
func (e *CatalogQueryRecordedEvent) OccurredAt() time.Time { return e.OccurredAt_ }

func (e *CatalogQueryRecordedEvent) Payload() []byte {
	if e.Payload_ == nil {
		body, _ := json.Marshal(map[string]any{
			"event_id":              e.EventID,
			"store_id":              e.StoreID_,
			"merchant_id":           e.MerchantID_,
			"kind":                  e.Kind,
			"surface":               e.Surface,
			"agent_platform":        e.AgentPlatform,
			"requested_product_ids": e.RequestedProductIDs,
			"occurred_at":           e.OccurredAt_,
		})
		e.Payload_ = body
	}
	return e.Payload_
}

// NewCatalogQueryRecordedEvent builds a CatalogQueryRecordedEvent, deriving
// event id, surface, and occurred_at internally (mirroring telemetry.go's
// WithTelemetry) so callers in the api package only need to supply domain
// data — agentPlatform is the raw UCP-Agent profile host, same input
// agentPlatformFromHeader/deriveSurface already take elsewhere.
func NewCatalogQueryRecordedEvent(storeID, merchantID string, kind CatalogQueryKind, agentPlatform string, requestedProductIDs []string) *CatalogQueryRecordedEvent {
	var agentPlatformPtr *string
	if agentPlatform != "" {
		agentPlatformPtr = &agentPlatform
	}
	return &CatalogQueryRecordedEvent{
		EventID:             newEventID(),
		StoreID_:            storeID,
		MerchantID_:         merchantID,
		Kind:                kind,
		Surface:             deriveSurface(agentPlatform),
		AgentPlatform:       agentPlatformPtr,
		RequestedProductIDs: requestedProductIDs,
		OccurredAt_:         time.Now(),
	}
}

// EmitCatalogQueryRecorded publishes ev in the background. Fire-and-forget
// with a bounded timeout, same as EmitRequestLog/WithTelemetry: the
// discovery read path must never block or fail a request on the event bus.
// pub may be nil (e.g. in unit tests that don't wire an event publisher).
func EmitCatalogQueryRecorded(pub eventbus.EventBus, log *slog.Logger, ev *CatalogQueryRecordedEvent) {
	if pub == nil {
		return
	}
	if log == nil {
		log = slog.Default()
	}
	safego.Go("emit-catalog-query", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		if err := pub.Publish(ctx, ev); err != nil {
			log.Debug("catalog query publish failed", "error", err, "event_id", ev.EventID)
		}
	})
}
