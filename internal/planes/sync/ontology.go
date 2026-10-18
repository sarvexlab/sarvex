// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	mongoopts "go.mongodb.org/mongo-driver/v2/mongo/options"
)

// OntologyRule represents a single mapping rule from raw → target field.
type OntologyRule struct {
	ID              string                 `bson:"_id"`
	SourceAttribute string                 `bson:"source_attr"`
	TargetField     string                 `bson:"target_field"`
	Platform        string                 `bson:"platform"` // "woocommerce", "magento", "*"
	Tier            string                 `bson:"tier"`     // "rule", "learned"
	Confidence      float64                `bson:"confidence"`
	TransformFn     string                 `bson:"transform_fn"` // "identity", "lowercase", "enum_map"
	TransformConfig map[string]interface{} `bson:"transform_config"`
	CreatedAt       time.Time              `bson:"created_at"`
	UpdatedAt       time.Time              `bson:"updated_at"`
}

// OntologyStore queries mapping rules from the cross-tenant ontology.
type OntologyStore interface {
	// GetRulesByPlatform returns all applicable rules for a platform, sorted by confidence (highest first).
	GetRulesByPlatform(ctx context.Context, platform string) ([]OntologyRule, error)

	// GetRule returns a single rule by ID.
	GetRule(ctx context.Context, ruleID string) (*OntologyRule, error)

	// ApplyRule transforms a raw attribute value using the rule's transform function.
	ApplyRule(rule OntologyRule, rawValue string) (string, error)
}

// MongoOntologyStore implements OntologyStore backed by MongoDB.
type MongoOntologyStore struct {
	db *mongo.Database
}

// NewMongoOntologyStore creates a MongoDB-backed ontology store.
func NewMongoOntologyStore(db *mongo.Database) *MongoOntologyStore {
	return &MongoOntologyStore{db: db}
}

// GetRulesByPlatform returns rules applicable to a platform (both platform-specific and wildcard).
func (mos *MongoOntologyStore) GetRulesByPlatform(ctx context.Context, platform string) ([]OntologyRule, error) {
	coll := mos.db.Collection("sync_ontology")

	filter := bson.M{
		"platform": bson.M{
			"$in": []string{platform, "*"},
		},
	}

	opts := mongoopts.Find().SetSort(bson.D{{Key: "confidence", Value: -1}, {Key: "tier", Value: 1}})
	cursor, err := coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("query ontology rules: %w", err)
	}
	defer cursor.Close(ctx)

	var rules []OntologyRule
	if err = cursor.All(ctx, &rules); err != nil {
		return nil, fmt.Errorf("decode rules: %w", err)
	}

	return rules, nil
}

// GetRule returns a single rule by ID.
func (mos *MongoOntologyStore) GetRule(ctx context.Context, ruleID string) (*OntologyRule, error) {
	coll := mos.db.Collection("sync_ontology")

	var rule OntologyRule
	err := coll.FindOne(ctx, bson.M{"_id": ruleID}).Decode(&rule)
	if err == mongo.ErrNoDocuments {
		return nil, fmt.Errorf("rule not found: %s", ruleID)
	}
	if err != nil {
		return nil, fmt.Errorf("get rule: %w", err)
	}

	return &rule, nil
}

// ApplyRule transforms a raw value according to the rule's transform function.
func (mos *MongoOntologyStore) ApplyRule(rule OntologyRule, rawValue string) (string, error) {
	switch rule.TransformFn {
	case "identity":
		return rawValue, nil

	case "lowercase":
		return strings.ToLower(rawValue), nil

	case "uppercase":
		return strings.ToUpper(rawValue), nil

	case "enum_map":
		// Map raw value to enum using config
		mapping, ok := rule.TransformConfig["mapping"].(map[string]interface{})
		if !ok {
			return "", fmt.Errorf("enum_map requires 'mapping' config")
		}
		if mapped, ok := mapping[rawValue].(string); ok {
			return mapped, nil
		}
		// If not found, return default or original
		if def, ok := rule.TransformConfig["default"].(string); ok {
			return def, nil
		}
		return rawValue, nil

	case "format_date":
		// Parse date in input format, return in output format
		// For V1: just validate it's a valid date
		if _, err := time.Parse("2006-01-02", rawValue); err != nil {
			return "", fmt.Errorf("invalid date format: %w", err)
		}
		return rawValue, nil

	default:
		return "", fmt.Errorf("unknown transform function: %s", rule.TransformFn)
	}
}
