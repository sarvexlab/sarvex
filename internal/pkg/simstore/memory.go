// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package simstore

import (
	"context"
	"sync"
)

// MemoryRepository is an in-memory StoreRepository, for tests and for running
// the simulator without Mongo. It hands out the stored *Store itself, not a
// copy, so a test can change a seeded store (enable pickup, say) and the next
// request sees it — exactly as it would after an Upsert.
type MemoryRepository struct {
	mu     sync.Mutex
	stores map[string]*Store
}

// NewMemoryRepository returns an empty MemoryRepository.
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{stores: make(map[string]*Store)}
}

var _ StoreRepository = (*MemoryRepository)(nil)

// Get implements StoreRepository.
func (m *MemoryRepository) Get(_ context.Context, storeID string) (*Store, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.stores[storeID]
	if !ok {
		return nil, ErrStoreNotFound
	}
	return s, nil
}

// Upsert implements StoreRepository.
func (m *MemoryRepository) Upsert(_ context.Context, s *Store) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stores[s.StoreID] = s
	return nil
}

// List implements StoreRepository.
func (m *MemoryRepository) List(_ context.Context) ([]*Store, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Store, 0, len(m.stores))
	for _, s := range m.stores {
		out = append(out, s)
	}
	return out, nil
}
