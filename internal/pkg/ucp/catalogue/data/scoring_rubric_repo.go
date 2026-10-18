// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package data

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	mongoopts "go.mongodb.org/mongo-driver/v2/mongo/options"

	v1 "github.com/sarvexlab/sarvex/internal/pkg/ucp/catalogue/v1"
)

// ScoringRubricRepo provides MongoDB access to scoring rubrics.
// It implements CRUD operations and the seeder can use it to load data into MongoDB.
type ScoringRubricRepo struct {
	db *mongo.Database
}

// NewScoringRubricRepo creates a new scoring rubric repository.
func NewScoringRubricRepo(db *mongo.Database) (*ScoringRubricRepo, error) {
	coll := db.Collection("catalogue_scoring_rubrics")

	// Create indexes
	indexModels := []mongo.IndexModel{
		// Unique index on version
		{
			Keys:    bson.D{{Key: "version", Value: 1}},
			Options: mongoopts.Index().SetUnique(true),
		},
		// Index on created_at for sorting
		{
			Keys: bson.D{{Key: "created_at", Value: -1}},
		},
	}

	opts := mongoopts.CreateIndexes()
	_, err := coll.Indexes().CreateMany(context.Background(), indexModels, opts)
	if err != nil {
		return nil, fmt.Errorf("create scoring rubric indexes: %w", err)
	}

	return &ScoringRubricRepo{db: db}, nil
}

// ScoringRubricModel is the MongoDB document for a scoring rubric.
type ScoringRubricModel struct {
	ID           string                 `bson:"_id"`
	Version      string                 `bson:"version"`
	SpecURL      string                 `bson:"spec_url"`
	Thresholds   ThresholdsDoc          `bson:"thresholds"`
	Rules        []ScoringRuleDoc       `bson:"rules"`
	Remediations []RemediationRuleModel `bson:"remediations"`

	// DimensionWeights are the overall-score composition weights. Without them
	// a rubric read back from Mongo scored with the equal-weight fallback.
	DimensionWeights map[string]int `bson:"dimension_weights,omitempty"`

	CreatedAt time.Time `bson:"created_at"`
	UpdatedAt time.Time `bson:"updated_at"`
}

// ThresholdsDoc is the MongoDB representation of Thresholds.
type ThresholdsDoc struct {
	ZeroBlockers bool `bson:"zero_blockers"`
}

// ScoringRuleDoc is the MongoDB representation of a ScoringRule.
type ScoringRuleDoc struct {
	ID             string `bson:"id"`
	Dimension      string `bson:"dimension"`
	Field          string `bson:"field"`
	Condition      string `bson:"condition"`
	BlocksCheckout bool   `bson:"blocks_checkout"`
	Weight         int    `bson:"weight"`
	SpecClause     string `bson:"spec_clause"`
	RemediationID  string `bson:"remediation_id"`
	Surface        string `bson:"surface,omitempty"`
}

// RemediationRuleModel is the MongoDB representation of a RemediationRule.
type RemediationRuleModel struct {
	ID            string         `bson:"id"`
	ScoringRuleID string         `bson:"scoring_rule_id"`
	Field         string         `bson:"field"`
	Action        string         `bson:"action"`
	Value         string         `bson:"value"`
	Params        map[string]any `bson:"params,omitempty"`
	Description   string         `bson:"description"`
}

// UpsertScoringRubric inserts or updates a scoring rubric.
func (r *ScoringRubricRepo) UpsertScoringRubric(ctx context.Context, rubric *v1.ScoringRubric) error {
	if rubric == nil {
		return fmt.Errorf("rubric is nil")
	}

	coll := r.db.Collection("catalogue_scoring_rubrics")

	// Convert v1.ScoringRubric to document
	doc := convertRubricToDoc(rubric)

	opts := mongoopts.UpdateOne().SetUpsert(true)
	_, err := coll.UpdateOne(ctx, bson.M{"version": rubric.Version}, bson.M{"$set": doc}, opts)
	if err != nil {
		return fmt.Errorf("upsert scoring rubric: %w", err)
	}

	return nil
}

// GetScoringRubric fetches a scoring rubric by version.
func (r *ScoringRubricRepo) GetScoringRubric(ctx context.Context, version string) (*v1.ScoringRubric, error) {
	coll := r.db.Collection("catalogue_scoring_rubrics")

	var doc ScoringRubricModel
	err := coll.FindOne(ctx, bson.M{"version": version}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil // Not found, return nil without error
		}
		return nil, fmt.Errorf("fetch scoring rubric: %w", err)
	}

	return convertDocToRubric(&doc), nil
}

// GetLatestScoringRubric fetches the most recently created scoring rubric.
func (r *ScoringRubricRepo) GetLatestScoringRubric(ctx context.Context) (*v1.ScoringRubric, error) {
	coll := r.db.Collection("catalogue_scoring_rubrics")

	opts := mongoopts.FindOne().SetSort(bson.D{{Key: "created_at", Value: -1}})
	var doc ScoringRubricModel
	err := coll.FindOne(ctx, bson.M{}, opts).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, fmt.Errorf("fetch latest scoring rubric: %w", err)
	}

	return convertDocToRubric(&doc), nil
}

// convertRubricToDoc converts v1.ScoringRubric to ScoringRubricDoc.
func convertRubricToDoc(rubric *v1.ScoringRubric) *ScoringRubricModel {
	now := time.Now().UTC()

	// Convert rules
	rules := make([]ScoringRuleDoc, len(rubric.Rules))
	for i, rule := range rubric.Rules {
		rules[i] = ScoringRuleDoc{
			ID:             rule.ID,
			Dimension:      rule.Dimension,
			Field:          rule.Field,
			Condition:      rule.Condition,
			BlocksCheckout: rule.BlocksCheckout,
			Weight:         rule.Weight,
			SpecClause:     rule.SpecClause,
			RemediationID:  rule.RemediationID,
			Surface:        rule.Surface,
		}
	}

	// Convert remediations
	remediations := make([]RemediationRuleModel, len(rubric.Remediations))
	for i, rem := range rubric.Remediations {
		remediations[i] = RemediationRuleModel{
			ID:            rem.ID,
			ScoringRuleID: rem.ScoringRuleID,
			Field:         rem.Field,
			Action:        rem.Action,
			Value:         rem.Value,
			Params:        rem.Params,
			Description:   rem.Description,
		}
	}

	return &ScoringRubricModel{
		ID:      rubric.Version,
		Version: rubric.Version,
		SpecURL: rubric.SpecURL,
		Thresholds: ThresholdsDoc{
			ZeroBlockers: rubric.Thresholds.ZeroBlockers,
		},
		Rules:            rules,
		Remediations:     remediations,
		DimensionWeights: rubric.DimensionWeights,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
}

// convertDocToRubric converts ScoringRubricDoc to v1.ScoringRubric.
func convertDocToRubric(doc *ScoringRubricModel) *v1.ScoringRubric {
	// Convert rules
	rules := make([]v1.ScoringRule, len(doc.Rules))
	for i, rule := range doc.Rules {
		rules[i] = v1.ScoringRule{
			ID:             rule.ID,
			Dimension:      rule.Dimension,
			Field:          rule.Field,
			Condition:      rule.Condition,
			BlocksCheckout: rule.BlocksCheckout,
			Weight:         rule.Weight,
			SpecClause:     rule.SpecClause,
			RemediationID:  rule.RemediationID,
			Surface:        rule.Surface,
		}
	}

	// Convert remediations
	remediations := make([]v1.RemediationRule, len(doc.Remediations))
	for i, rem := range doc.Remediations {
		remediations[i] = v1.RemediationRule{
			ID:            rem.ID,
			ScoringRuleID: rem.ScoringRuleID,
			Field:         rem.Field,
			Action:        rem.Action,
			Value:         rem.Value,
			Params:        rem.Params,
			Description:   rem.Description,
		}
	}

	return &v1.ScoringRubric{
		Version: doc.Version,
		SpecURL: doc.SpecURL,
		Thresholds: v1.Thresholds{
			ZeroBlockers: doc.Thresholds.ZeroBlockers,
		},
		Rules:            rules,
		Remediations:     remediations,
		DimensionWeights: doc.DimensionWeights,
	}
}
