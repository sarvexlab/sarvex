// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"

	"github.com/redis/go-redis/v9"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

// ErrCartNotFound means no cart with that id exists for the store (or it
// expired). Carts are addressed by an opaque Sarvex id, never the merchant's
// own — on WooCommerce a cart is a pending order, so exposing the backend id
// would let an agent read any order in the store.
var ErrCartNotFound = errors.New("serving: cart not found")

// ErrCartNotReady means the cart was reserved but the merchant never confirmed
// it (e.g. the create crashed between reserving the idempotency key and the
// connector call), so there is nothing to return.
var ErrCartNotReady = errors.New("serving: cart has no backend cart yet — retry with a new idempotency key")

// CartRecord is Sarvex's bookkeeping for one cart, owned by exactly one store.
// The cart itself lives at the merchant; this maps the agent-facing id to it
// and remembers the last response, which Get serves without a live call.
type CartRecord struct {
	CartID        string `json:"cart_id"`
	StoreID       string `json:"store_id"`
	MerchantID    string `json:"merchant_id"`
	BackendCartID string `json:"backend_cart_id,omitempty"`

	// The request state, kept so a cart can seed a checkout (cart_id).
	LineItems []base.LineItemCreateRequest `json:"line_items,omitempty"`
	Buyer     *model.Buyer                 `json:"buyer,omitempty"`
	Discounts *model.DiscountsObject       `json:"discounts,omitempty"`

	IdempotencyKey string             `json:"idempotency_key,omitempty"`
	BodyHash       string             `json:"body_hash,omitempty"`
	LastResponse   *base.CartResponse `json:"last_response,omitempty"`
	CreatedAt      time.Time          `json:"created_at"`
	UpdatedAt      time.Time          `json:"updated_at"`
}

// CartStore persists cart records, fenced by store on every operation.
type CartStore interface {
	// Create stores rec. When rec.IdempotencyKey is already bound in the same
	// store it returns the bound record with replayed=true if the body hash
	// matches, or ErrIdempotencyKeyConflict if it does not.
	Create(ctx context.Context, rec *CartRecord) (stored *CartRecord, replayed bool, err error)
	// Get returns the store's cart, or ErrCartNotFound.
	Get(ctx context.Context, storeID, cartID string) (*CartRecord, error)
	// Update replaces an existing record, or returns ErrCartNotFound.
	Update(ctx context.Context, rec *CartRecord) error
}

func cartIdemKey(storeID, key string) string { return storeID + ":" + key }

// ─── In-memory ───────────────────────────────────────────────────────────────

// MemoryCartStore is a single-process CartStore for tests and local use.
type MemoryCartStore struct {
	mu    sync.Mutex
	ttl   time.Duration
	carts map[string]*CartRecord // cartID → record
	idem  map[string]string      // "storeID:key" → cartID
}

// NewMemoryCartStore creates an in-memory cart store; records older than ttl
// (since last update) read as not found.
func NewMemoryCartStore(ttl time.Duration) *MemoryCartStore {
	return &MemoryCartStore{ttl: ttl, carts: map[string]*CartRecord{}, idem: map[string]string{}}
}

func (s *MemoryCartStore) live(rec *CartRecord) bool {
	return rec != nil && (s.ttl <= 0 || time.Since(rec.UpdatedAt) < s.ttl)
}

func (s *MemoryCartStore) Create(_ context.Context, rec *CartRecord) (*CartRecord, bool, error) {
	if rec == nil || rec.CartID == "" || rec.StoreID == "" {
		return nil, false, errors.New("cart id and store id required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if rec.IdempotencyKey != "" {
		if id, ok := s.idem[cartIdemKey(rec.StoreID, rec.IdempotencyKey)]; ok {
			if existing := s.carts[id]; s.live(existing) {
				if existing.BodyHash != rec.BodyHash {
					return nil, false, ErrIdempotencyKeyConflict
				}
				cp := *existing
				return &cp, true, nil
			}
		}
		s.idem[cartIdemKey(rec.StoreID, rec.IdempotencyKey)] = rec.CartID
	}
	cp := *rec
	s.carts[rec.CartID] = &cp
	return rec, false, nil
}

func (s *MemoryCartStore) Get(_ context.Context, storeID, cartID string) (*CartRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.carts[cartID]
	if !s.live(rec) || rec.StoreID != storeID {
		return nil, ErrCartNotFound
	}
	cp := *rec
	return &cp, nil
}

func (s *MemoryCartStore) Update(_ context.Context, rec *CartRecord) error {
	if rec == nil {
		return ErrCartNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing := s.carts[rec.CartID]
	if !s.live(existing) || existing.StoreID != rec.StoreID {
		return ErrCartNotFound
	}
	cp := *rec
	s.carts[rec.CartID] = &cp
	return nil
}

// ─── Redis ───────────────────────────────────────────────────────────────────

// RedisCartStore is the distributed CartStore used in production.
type RedisCartStore struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRedisCartStore creates a Redis-backed cart store whose records expire ttl
// after their last write.
func NewRedisCartStore(client *redis.Client, ttl time.Duration) *RedisCartStore {
	return &RedisCartStore{client: client, ttl: ttl}
}

func cartKey(storeID, cartID string) string { return fmt.Sprintf("cart:%s:%s", storeID, cartID) }
func cartIdxKey(storeID, key string) string { return fmt.Sprintf("cart-idem:%s:%s", storeID, key) }

func (s *RedisCartStore) Create(ctx context.Context, rec *CartRecord) (*CartRecord, bool, error) {
	if rec == nil || rec.CartID == "" || rec.StoreID == "" {
		return nil, false, errors.New("cart id and store id required")
	}

	if rec.IdempotencyKey != "" {
		// Reserve the key atomically; the loser of a race reads the winner.
		won, err := s.client.SetNX(ctx, cartIdxKey(rec.StoreID, rec.IdempotencyKey), rec.CartID, s.ttl).Result()
		if err != nil {
			return nil, false, fmt.Errorf("reserve cart idempotency key: %w", err)
		}
		if !won {
			id, err := s.client.Get(ctx, cartIdxKey(rec.StoreID, rec.IdempotencyKey)).Result()
			if err != nil {
				return nil, false, fmt.Errorf("read cart idempotency key: %w", err)
			}
			existing, err := s.Get(ctx, rec.StoreID, id)
			if err != nil {
				return nil, false, fmt.Errorf("read cart bound to idempotency key: %w", err)
			}
			if existing.BodyHash != rec.BodyHash {
				return nil, false, ErrIdempotencyKeyConflict
			}
			return existing, true, nil
		}
	}

	if err := s.write(ctx, rec); err != nil {
		return nil, false, err
	}
	return rec, false, nil
}

func (s *RedisCartStore) write(ctx context.Context, rec *CartRecord) error {
	body, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal cart: %w", err)
	}
	if err := s.client.Set(ctx, cartKey(rec.StoreID, rec.CartID), body, s.ttl).Err(); err != nil {
		return fmt.Errorf("write cart: %w", err)
	}
	return nil
}

func (s *RedisCartStore) Get(ctx context.Context, storeID, cartID string) (*CartRecord, error) {
	body, err := s.client.Get(ctx, cartKey(storeID, cartID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrCartNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read cart: %w", err)
	}
	var rec CartRecord
	if err := json.Unmarshal(body, &rec); err != nil {
		return nil, fmt.Errorf("decode cart: %w", err)
	}
	return &rec, nil
}

func (s *RedisCartStore) Update(ctx context.Context, rec *CartRecord) error {
	if rec == nil {
		return ErrCartNotFound
	}
	if _, err := s.Get(ctx, rec.StoreID, rec.CartID); err != nil {
		return err
	}
	return s.write(ctx, rec)
}
