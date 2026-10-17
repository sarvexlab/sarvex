// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package endpointhealth

import (
	"encoding/json"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp"
)

// ProbeCompletedEvent is published once per probe run (not once per store —
// each store's cycle produces len(AllProbeTypes) of these), carrying enough
// detail for the evaluator (and GET .../health/history) without a second
// query back to the prober.
type ProbeCompletedEvent struct {
	StoreID_    string      `json:"store_id"`
	MerchantID_ string      `json:"merchant_id"`
	ProbeType   ProbeType   `json:"probe_type"`
	Outcome     Outcome     `json:"outcome"`
	LatencyMs   int64       `json:"latency_ms"`
	FailureCode FailureCode `json:"failure_code,omitempty"`
	Detail      string      `json:"detail,omitempty"`
	Synthetic   bool        `json:"synthetic"`
	ObservedAt_ time.Time   `json:"observed_at"`
	payload     []byte
}

func (e *ProbeCompletedEvent) EventType() string     { return ucp.EventHealthProbeCompleted }
func (e *ProbeCompletedEvent) StoreID() string       { return e.StoreID_ }
func (e *ProbeCompletedEvent) MerchantID() string    { return e.MerchantID_ }
func (e *ProbeCompletedEvent) OccurredAt() time.Time { return e.ObservedAt_ }

func (e *ProbeCompletedEvent) Payload() []byte {
	if e.payload == nil {
		e.payload, _ = json.Marshal(map[string]any{
			"store_id":     e.StoreID_,
			"merchant_id":  e.MerchantID_,
			"probe_type":   e.ProbeType,
			"outcome":      e.Outcome,
			"latency_ms":   e.LatencyMs,
			"failure_code": e.FailureCode,
			"detail":       e.Detail,
			"synthetic":    e.Synthetic,
			"observed_at":  e.ObservedAt_,
		})
	}
	return e.payload
}

// StatusChangeEvent backs both ops.health.threshold_breached and
// ops.health.recovered — one per {store, check_type} transition (not one
// per evaluation), published by the flap-suppression state machine in
// status.go, never directly by the evaluator.
type StatusChangeEvent struct {
	kind        string
	StoreID_    string    `json:"store_id"`
	MerchantID_ string    `json:"merchant_id"`
	CheckType   CheckType `json:"check_type"`
	Severity    Severity  `json:"severity"`
	Value       float64   `json:"value"`
	AlertID     string    `json:"alert_id,omitempty"`
	At_         time.Time `json:"at"`
	payload     []byte
}

// NewThresholdBreachedEvent constructs the ops.health.threshold_breached event.
func NewThresholdBreachedEvent(merchantID, storeID string, check CheckType, sev Severity, value float64, alertID string) *StatusChangeEvent {
	return &StatusChangeEvent{kind: ucp.EventHealthThresholdBreached, StoreID_: storeID, MerchantID_: merchantID, CheckType: check, Severity: sev, Value: value, AlertID: alertID, At_: time.Now().UTC()}
}

// NewRecoveredEvent constructs the ops.health.recovered event.
func NewRecoveredEvent(merchantID, storeID string, check CheckType, value float64, alertID string) *StatusChangeEvent {
	return &StatusChangeEvent{kind: ucp.EventHealthRecovered, StoreID_: storeID, MerchantID_: merchantID, CheckType: check, Severity: SeverityHealthy, Value: value, AlertID: alertID, At_: time.Now().UTC()}
}

func (e *StatusChangeEvent) EventType() string     { return e.kind }
func (e *StatusChangeEvent) StoreID() string       { return e.StoreID_ }
func (e *StatusChangeEvent) MerchantID() string    { return e.MerchantID_ }
func (e *StatusChangeEvent) OccurredAt() time.Time { return e.At_ }

func (e *StatusChangeEvent) Payload() []byte {
	if e.payload == nil {
		e.payload, _ = json.Marshal(map[string]any{
			"store_id":    e.StoreID_,
			"merchant_id": e.MerchantID_,
			"check_type":  e.CheckType,
			"severity":    e.Severity,
			"value":       e.Value,
			"alert_id":    e.AlertID,
			"at":          e.At_,
		})
	}
	return e.payload
}
