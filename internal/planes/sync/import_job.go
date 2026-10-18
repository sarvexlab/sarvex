// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"errors"
	"time"
)

// ErrImportJobNotFound is returned when no import job exists for a store.
var ErrImportJobNotFound = errors.New("sync: import job not found")

// ImportStatus is the lifecycle state of a catalog import job.
type ImportStatus string

const (
	ImportStatusRunning   ImportStatus = "running"
	ImportStatusCompleted ImportStatus = "completed"
	ImportStatusFailed    ImportStatus = "failed"
)

// ImportCounts tracks running totals for a catalog import, surfaced as the
// job's progress (the durable record IS the progress signal an operator or
// future status endpoint reads; see catalog_import.go for the one
// best-effort progress event this package also emits).
type ImportCounts struct {
	Fetched  int `bson:"fetched" json:"fetched"`
	Upserted int `bson:"upserted" json:"upserted"`
}

// ImportJob is the persisted state of one store's catalog import. It is
// keyed by store_id: at most one import is tracked per store at a time,
// consistent with "one writer per store" (architecture §4.5) — a second
// concurrent import for the same store is refused, not raced.
//
// Cursor is the last-committed SKU in FetchCatalog's returned order. A
// restart (process crash, redeploy) resumes past already-upserted products
// by re-fetching the catalog and skipping forward to Cursor, instead of
// reprocessing from the beginning.
type ImportJob struct {
	StoreID        string       `bson:"_id" json:"store_id"`
	MerchantID     string       `bson:"merchant_id" json:"merchant_id"`
	Reason         string       `bson:"reason" json:"reason"` // "initial" | "resync"
	IdempotencyKey string       `bson:"idempotency_key" json:"idempotency_key"`
	Status         ImportStatus `bson:"status" json:"status"`
	Cursor         string       `bson:"cursor" json:"cursor"`
	Counts         ImportCounts `bson:"counts" json:"counts"`
	StartedAt      time.Time    `bson:"started_at" json:"started_at"`
	UpdatedAt      time.Time    `bson:"updated_at" json:"updated_at"`
	CompletedAt    *time.Time   `bson:"completed_at,omitempty" json:"completed_at,omitempty"`
	Error          string       `bson:"error,omitempty" json:"error,omitempty"`
}

// ImportJobRepository persists ImportJob state so a restart can resume an
// in-flight import from its last committed cursor rather than from zero.
type ImportJobRepository interface {
	// Get retrieves the import job for a store. Returns ErrImportJobNotFound
	// if none exists.
	Get(ctx context.Context, storeID string) (*ImportJob, error)
	// Upsert persists the current state of an import job (atomic).
	Upsert(ctx context.Context, job *ImportJob) error
	// ListByStatus returns all jobs in the given status. Used at process
	// start to resume imports that were still running when the process
	// last stopped.
	ListByStatus(ctx context.Context, status ImportStatus) ([]*ImportJob, error)
}
