// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package commandbus

import (
	"encoding/json"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
)

// AuditRequestedEvent is emitted by the control plane to request catalog re-scoring.
// Producer: control plane (CatalogService.TriggerAudit via CommandEmitter)
// Consumer: sync plane (handleAuditRequested)
type AuditRequestedEvent struct {
	merchantID string
	storeID    string
	jobID      string
	occurredAt time.Time
	payload    AuditRequestedPayload
}

// AuditRequestedPayload is the event payload for ucp.CommandRequestAudit.
type AuditRequestedPayload struct {
	JobID  string `json:"job_id"`
	Source string `json:"source"` // "manual" | "onboarding"
}

// NewAuditRequestedEvent creates a new audit requested event.
func NewAuditRequestedEvent(merchantID, storeID, jobID string) *AuditRequestedEvent {
	return &AuditRequestedEvent{
		merchantID: merchantID,
		storeID:    storeID,
		jobID:      jobID,
		occurredAt: time.Now().UTC(),
		payload: AuditRequestedPayload{
			JobID:  jobID,
			Source: "manual",
		},
	}
}

func (e *AuditRequestedEvent) EventType() string     { return ucp.CommandRequestAudit }
func (e *AuditRequestedEvent) StoreID() string       { return e.storeID }
func (e *AuditRequestedEvent) MerchantID() string    { return e.merchantID }
func (e *AuditRequestedEvent) OccurredAt() time.Time { return e.occurredAt }
func (e *AuditRequestedEvent) Payload() []byte {
	b, _ := json.Marshal(e.payload)
	return b
}

var _ eventbus.DomainEvent = (*AuditRequestedEvent)(nil)

// RemediationsAppliedEvent is emitted when the control plane applies rule-based fixes.
// Producer: control plane (CatalogService.ApplyRemediations via CommandEmitter)
// Consumer: sync plane (handleRemediationsApplied)
type RemediationsAppliedEvent struct {
	merchantID string
	storeID    string
	occurredAt time.Time
	payload    RemediationsAppliedPayload
}

// RemediationsAppliedPayload is the event payload for ucp.CommandApplyRemediations.
type RemediationsAppliedPayload struct {
	WriteBackID    string   `json:"write_back_id"`
	RemediationIDs []string `json:"remediation_ids"`
	Actor          string   `json:"actor"` // operator email or "api-client"
}

// NewRemediationsAppliedEvent creates a new remediations applied event.
func NewRemediationsAppliedEvent(merchantID, storeID, writeBackID string, remediationIDs []string) *RemediationsAppliedEvent {
	return &RemediationsAppliedEvent{
		merchantID: merchantID,
		storeID:    storeID,
		occurredAt: time.Now().UTC(),
		payload: RemediationsAppliedPayload{
			WriteBackID:    writeBackID,
			RemediationIDs: remediationIDs,
			Actor:          "api-client",
		},
	}
}

func (e *RemediationsAppliedEvent) EventType() string     { return ucp.CommandApplyRemediations }
func (e *RemediationsAppliedEvent) StoreID() string       { return e.storeID } // TODO: pass storeID when wiring store context
func (e *RemediationsAppliedEvent) MerchantID() string    { return e.merchantID }
func (e *RemediationsAppliedEvent) OccurredAt() time.Time { return e.occurredAt }
func (e *RemediationsAppliedEvent) Payload() []byte {
	b, _ := json.Marshal(e.payload)
	return b
}

var _ eventbus.DomainEvent = (*RemediationsAppliedEvent)(nil)

// RemediationsRevertedEvent is ucp.CommandRevertRemediations: undo a
// write-back. Producer: control plane (RevertWriteBack). Consumer: sync plane
// (handleRemediationsReverted), which restores each applied record's prior
// value on the product and in the merchant's store, and reopens the record.
type RemediationsRevertedEvent struct {
	merchantID string
	storeID    string
	occurredAt time.Time
	payload    RemediationsRevertedPayload
}

// RemediationsRevertedPayload is the event payload for
// ucp.CommandRevertRemediations.
type RemediationsRevertedPayload struct {
	WriteBackID    string   `json:"write_back_id"`
	RemediationIDs []string `json:"remediation_ids"`
	Actor          string   `json:"actor"` // operator email or "api-client"
}

// NewRemediationsRevertedEvent creates a revert command for writeBackID.
func NewRemediationsRevertedEvent(merchantID, storeID, writeBackID string, remediationIDs []string) *RemediationsRevertedEvent {
	return &RemediationsRevertedEvent{
		merchantID: merchantID,
		storeID:    storeID,
		occurredAt: time.Now().UTC(),
		payload: RemediationsRevertedPayload{
			WriteBackID:    writeBackID,
			RemediationIDs: remediationIDs,
			Actor:          "api-client",
		},
	}
}

func (e *RemediationsRevertedEvent) EventType() string     { return ucp.CommandRevertRemediations }
func (e *RemediationsRevertedEvent) StoreID() string       { return e.storeID }
func (e *RemediationsRevertedEvent) MerchantID() string    { return e.merchantID }
func (e *RemediationsRevertedEvent) OccurredAt() time.Time { return e.occurredAt }
func (e *RemediationsRevertedEvent) Payload() []byte {
	b, _ := json.Marshal(e.payload)
	return b
}

var _ eventbus.DomainEvent = (*RemediationsRevertedEvent)(nil)
