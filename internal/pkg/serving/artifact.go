// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// Artifact is a surface's Sarvex-generated document as the serving cache
// holds it: opaque bytes plus the media type they are served with.
type Artifact struct {
	ContentType string
	Body        []byte
}

// ArtifactReader is the serving plane's read of surface artifacts. Like the
// rest of the serving cache it is read-only there; the sync plane writes.
type ArtifactReader interface {
	// GetArtifact returns the artifact published under token, or ErrNotFound.
	GetArtifact(ctx context.Context, token string) (*Artifact, error)
}

// ArtifactWriter is the sync plane's write side.
type ArtifactWriter interface {
	// PutArtifact replaces the artifact under token. Atomic: a concurrent
	// reader sees the old or the new document, never a partial one.
	PutArtifact(ctx context.Context, token string, a Artifact) error
	// DeleteArtifact unpublishes token. Deleting an absent token is not an
	// error.
	DeleteArtifact(ctx context.Context, token string) error
}

// artifactPrefix keeps artifacts out of the per-store "serving:{store}:"
// keyspace, whose product keys SwapSnapshot deletes and rewrites: a snapshot
// rebuild must never take a published artifact down with it.
const artifactPrefix = "serving:artifact:"

type redisArtifacts struct{ client redis.Cmdable }

// NewRedisArtifacts returns the Redis-backed artifact store.
func NewRedisArtifacts(client redis.Cmdable) interface {
	ArtifactReader
	ArtifactWriter
} {
	return &redisArtifacts{client: client}
}

func (r *redisArtifacts) PutArtifact(ctx context.Context, token string, a Artifact) error {
	pipe := r.client.TxPipeline()
	pipe.HSet(ctx, artifactPrefix+token, "content_type", a.ContentType, "body", a.Body)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("write artifact: %w", err)
	}
	return nil
}

func (r *redisArtifacts) GetArtifact(ctx context.Context, token string) (*Artifact, error) {
	m, err := r.client.HGetAll(ctx, artifactPrefix+token).Result()
	if err != nil {
		return nil, fmt.Errorf("read artifact: %w", err)
	}
	if len(m) == 0 {
		return nil, ErrNotFound
	}
	return &Artifact{ContentType: m["content_type"], Body: []byte(m["body"])}, nil
}

func (r *redisArtifacts) DeleteArtifact(ctx context.Context, token string) error {
	if err := r.client.Del(ctx, artifactPrefix+token).Err(); err != nil {
		return fmt.Errorf("delete artifact: %w", err)
	}
	return nil
}
