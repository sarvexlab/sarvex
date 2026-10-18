// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"errors"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// MemoryConformanceStore is an in-memory conformance record store (for testing/development).
//
// Keyed by store id, taken from the record itself. It previously keyed
// writes off a separately-set merchant id while reads came in by store,
// so a saved record could never be read back: GetStatus always reported
// pre_validation as not_run and ListRuns always came back empty, no matter
// how many runs had happened.
type MemoryConformanceStore struct {
	mu      sync.RWMutex
	records map[string][]*domain.ConformanceRecord // storeID → records (oldest first)
}

// NewMemoryConformanceStore creates an in-memory conformance store.
func NewMemoryConformanceStore() *MemoryConformanceStore {
	return &MemoryConformanceStore{
		records: make(map[string][]*domain.ConformanceRecord),
	}
}

// SaveRecord stores a conformance record under its own StoreID.
func (s *MemoryConformanceStore) SaveRecord(ctx context.Context, record *domain.ConformanceRecord) error {
	if record == nil {
		return errors.New("record is nil")
	}
	if record.StoreID == "" {
		return errors.New("record has no store id")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	storeID := string(record.StoreID)
	s.records[storeID] = append(s.records[storeID], record)
	return nil
}

// GetLatest returns the most recent conformance record for a store.
func (s *MemoryConformanceStore) GetLatest(ctx context.Context, storeID string) (*domain.ConformanceRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	records, ok := s.records[storeID]
	if !ok || len(records) == 0 {
		return nil, errors.New("no records found")
	}

	// Return the last record (most recent).
	return records[len(records)-1], nil
}

// ListRuns returns paginated conformance records for a store.
func (s *MemoryConformanceStore) ListRuns(ctx context.Context, storeID string, limit int, cursor string) ([]*domain.ConformanceRecord, *string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	records, ok := s.records[storeID]
	if !ok || len(records) == 0 {
		return []*domain.ConformanceRecord{}, nil, nil
	}

	// Sort by RanAt descending (most recent first).
	sortedRecords := make([]*domain.ConformanceRecord, len(records))
	copy(sortedRecords, records)
	sort.Slice(sortedRecords, func(i, j int) bool {
		return sortedRecords[i].RanAt.After(sortedRecords[j].RanAt)
	})

	// Apply cursor for pagination.
	start := 0
	if cursor != "" {
		// Cursor is a timestamp; find the first record before this time.
		if cursorTime, err := time.Parse(time.RFC3339, cursor); err == nil {
			if i := slices.IndexFunc(sortedRecords, func(rec *domain.ConformanceRecord) bool { return rec.RanAt.Before(cursorTime) }); i >= 0 {
				start = i
			}
		}
	}

	// Paginate.
	end := start + limit
	if end > len(sortedRecords) {
		end = len(sortedRecords)
	} // Could use min(start+limit, len(sortedRecords)) but this is clearer

	var nextCursor *string
	if end < len(sortedRecords) {
		// Next cursor is the timestamp of the last item in this batch.
		c := sortedRecords[end].RanAt.Format(time.RFC3339)
		nextCursor = &c
	}

	return sortedRecords[start:end], nextCursor, nil
}
