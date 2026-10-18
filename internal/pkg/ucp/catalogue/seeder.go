// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package catalogue

import (
	"context"
	"fmt"
	"log/slog"

	"go.mongodb.org/mongo-driver/v2/mongo"

	cataloguedata "github.com/sarvexlab/sarvex/internal/pkg/ucp/catalogue/data"
	v1cat "github.com/sarvexlab/sarvex/internal/pkg/ucp/catalogue/v1"
)

// ScoringRubricSeeder handles seeding scoring rubrics into MongoDB.
type ScoringRubricSeeder struct {
	repo *cataloguedata.ScoringRubricRepo
	log  *slog.Logger
}

// NewScoringRubricSeeder creates a new scoring rubric seeder.
func NewScoringRubricSeeder(db *mongo.Database, log *slog.Logger) (*ScoringRubricSeeder, error) {
	if log == nil {
		log = slog.Default()
	}

	repo, err := cataloguedata.NewScoringRubricRepo(db)
	if err != nil {
		return nil, fmt.Errorf("create scoring rubric repo: %w", err)
	}

	return &ScoringRubricSeeder{
		repo: repo,
		log:  log,
	}, nil
}

// SeedRubric loads the default scoring rubric from code and seeds it into MongoDB.
// This is idempotent: if the rubric already exists (same version), it is updated.
// No runtime file I/O: the rubric is defined as hardcoded Go data.
func (s *ScoringRubricSeeder) SeedRubric(ctx context.Context) error {
	// Get the default rubric from code (no file I/O at runtime)
	rubric := v1cat.DefaultScoringRubric()

	// Upsert into MongoDB
	if err := s.repo.UpsertScoringRubric(ctx, rubric); err != nil {
		return fmt.Errorf("upsert scoring rubric: %w", err)
	}

	s.log.Info("seeded scoring rubric",
		"version", rubric.Version,
		"rules", len(rubric.Rules),
		"remediations", len(rubric.Remediations),
	)

	return nil
}
