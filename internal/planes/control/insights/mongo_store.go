// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package insights

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// mongoStore is a Store backed by five MongoDB collections: the four
// aggregate collections (control_product_demand_daily,
// control_negotiation_daily, control_payment_handler_mismatch_daily,
// control_capability_gaps_monthly) plus a short-lived TTL'd insert-only
// dedup set for the two source event kinds this package folds — mirrors
// channel.mongoStore's telemetryDedup/control_agent_sessions split exactly
// (dedup separate from the transactional/upsert fold, since a single
// catalog-query event can touch many product documents at once and needs
// a single dedup gate for all of them, not a per-document one).
type mongoStore struct {
	productDemand   *mongo.Collection
	negotiation     *mongo.Collection
	handlerMismatch *mongo.Collection
	capabilityGaps  *mongo.Collection
	dedup           *mongo.Collection
}

// dedupTTL bounds how long an event/composite dedup key is remembered —
// mirrors channel.telemetryDedupTTL's reasoning exactly.
const dedupTTL = 48 * time.Hour

// NewMongoStore creates a Store, with the indexes every fold/read method
// needs.
func NewMongoStore(db *mongo.Database) (Store, error) {
	s := &mongoStore{
		productDemand:   db.Collection("control_product_demand_daily"),
		negotiation:     db.Collection("control_negotiation_daily"),
		handlerMismatch: db.Collection("control_payment_handler_mismatch_daily"),
		capabilityGaps:  db.Collection("control_capability_gaps_monthly"),
		dedup:           db.Collection("control_insights_event_dedup"),
	}
	ctx := context.Background()
	if _, err := s.productDemand.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "store_id", Value: 1}, {Key: "date", Value: 1}}},
	}); err != nil {
		return nil, fmt.Errorf("control_product_demand_daily indexes: %w", err)
	}
	if _, err := s.negotiation.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "store_id", Value: 1}, {Key: "date", Value: 1}}},
	}); err != nil {
		return nil, fmt.Errorf("control_negotiation_daily indexes: %w", err)
	}
	if _, err := s.handlerMismatch.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "store_id", Value: 1}, {Key: "date", Value: 1}}},
	}); err != nil {
		return nil, fmt.Errorf("control_payment_handler_mismatch_daily indexes: %w", err)
	}
	if _, err := s.capabilityGaps.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "store_id", Value: 1}, {Key: "month", Value: 1}}},
	}); err != nil {
		return nil, fmt.Errorf("control_capability_gaps_monthly indexes: %w", err)
	}
	if _, err := s.dedup.Indexes().CreateOne(ctx,
		mongo.IndexModel{Keys: bson.D{{Key: "expires_at", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)},
	); err != nil {
		return nil, fmt.Errorf("control_insights_event_dedup index: %w", err)
	}
	return s, nil
}

type dedupDoc struct {
	ID        string    `bson:"_id"`
	ExpiresAt time.Time `bson:"expires_at"`
}

// claimDedup inserts key into the dedup collection. Returns claimed=false
// (no error) for a key already present — the expected, idempotent shape of
// a redelivered event (A1/A2), mirroring channel.RecordDiscoveryRequest's
// identical pattern.
func (s *mongoStore) claimDedup(ctx context.Context, key string) (claimed bool, err error) {
	_, err = s.dedup.InsertOne(ctx, dedupDoc{ID: key, ExpiresAt: time.Now().UTC().Add(dedupTTL)})
	if mongo.IsDuplicateKeyError(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim dedup key: %w", err)
	}
	return true, nil
}

func (s *mongoStore) FoldCatalogQuery(ctx context.Context, q CatalogQuery) error {
	if q.EventID == "" || len(q.ProductIDs) == 0 {
		return nil
	}
	claimed, err := s.claimDedup(ctx, "catalog_query:"+q.EventID)
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}

	date := DateKey(q.OccurredAt)
	surface := string(deriveSurface(q.AgentPlatformHost))
	now := time.Now().UTC()

	for _, productID := range q.ProductIDs {
		id := productDemandID(q.StoreID, date, productID)
		if _, err := s.productDemand.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
			"$setOnInsert": bson.M{
				"_id": id, "merchant_id": q.MerchantID, "store_id": q.StoreID,
				"date": date, "product_id": productID,
			},
			"$inc": bson.M{"request_count": 1, "surface_breakdown." + surface: 1},
			"$set": bson.M{"updated_at": now},
		}, options.UpdateOne().SetUpsert(true)); err != nil {
			return fmt.Errorf("increment product_demand_daily %s: %w", id, err)
		}
	}
	return nil
}

func (s *mongoStore) FoldNegotiationOutcome(ctx context.Context, out NegotiationOutcome) error {
	if out.DedupKey == "" {
		return nil
	}
	claimed, err := s.claimDedup(ctx, "negotiation:"+out.DedupKey)
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}

	date := DateKey(out.OccurredAt)
	now := time.Now().UTC()

	if err := s.foldNegotiationDaily(ctx, out, date, now); err != nil {
		return err
	}
	if err := s.foldPaymentHandlerMismatch(ctx, out, date, now); err != nil {
		return err
	}
	if !out.Succeeded && out.FailureReason == NegotiationFailureCapabilitiesIncompatible {
		if err := s.foldCapabilityGaps(ctx, out, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *mongoStore) foldNegotiationDaily(ctx context.Context, out NegotiationOutcome, date string, now time.Time) error {
	id := negotiationDailyID(out.StoreID, date)
	if _, err := s.negotiation.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$setOnInsert": bson.M{"_id": id, "merchant_id": out.MerchantID, "store_id": out.StoreID, "date": date},
		"$set":         bson.M{"updated_at": now},
	}, options.UpdateOne().SetUpsert(true)); err != nil {
		return fmt.Errorf("ensure negotiation_daily document: %w", err)
	}

	inc := bson.M{"attempts": 1}
	if out.Succeeded {
		inc["successes"] = 1
	}
	if _, err := s.negotiation.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$inc": inc}); err != nil {
		return fmt.Errorf("increment negotiation_daily attempts: %w", err)
	}
	if out.Succeeded {
		return nil
	}

	// missing_capability_counts only means something for
	// capabilities_incompatible — see NegotiationFailureBreakdown's doc
	// comment (model.go). Passing it for version_unsupported would
	// misrepresent an incidental set difference as the delta that caused
	// the failure.
	var missing []string
	if out.FailureReason == NegotiationFailureCapabilitiesIncompatible {
		missing = missingCapabilities(out.RequestedCapabilities, out.DeclaredCapabilities)
	}
	if err := s.incrementFailureBreakdown(ctx, id, out.FailureReason, missing); err != nil {
		return err
	}
	return nil
}

// incrementFailureBreakdown increments the matching reason's entry in
// NegotiationDaily.Failures, appending a new breakdown element if this is
// the first occurrence of reason that day — mirrors ledger.foldIntoRollup's
// increment-existing-or-append pattern exactly.
func (s *mongoStore) incrementFailureBreakdown(ctx context.Context, docID string, reason NegotiationFailureReason, missing []string) error {
	incFields := bson.M{"failures.$.count": 1}
	for _, cap := range missing {
		incFields["failures.$.missing_capability_counts."+cap] = 1
	}
	res, err := s.negotiation.UpdateOne(ctx,
		bson.M{"_id": docID, "failures.reason": reason},
		bson.M{"$inc": incFields},
	)
	if err != nil {
		return fmt.Errorf("increment existing failure breakdown: %w", err)
	}
	if res.MatchedCount > 0 {
		return nil
	}

	counts := map[string]int64{}
	for _, cap := range missing {
		counts[cap] = 1
	}
	breakdown := NegotiationFailureBreakdown{Reason: reason, Count: 1}
	if len(counts) > 0 {
		breakdown.MissingCapabilityCounts = counts
	}
	if _, err := s.negotiation.UpdateOne(ctx, bson.M{"_id": docID}, bson.M{"$push": bson.M{"failures": breakdown}}); err != nil {
		return fmt.Errorf("append new failure breakdown: %w", err)
	}
	return nil
}

func (s *mongoStore) foldPaymentHandlerMismatch(ctx context.Context, out NegotiationOutcome, date string, now time.Time) error {
	if len(out.RequestedPaymentHandlers) == 0 {
		return nil
	}
	offered := missingCapabilities(out.RequestedPaymentHandlers, out.DeclaredPaymentHandlers)
	// A genuine overlap exists — not a no_common_payment_handler occurrence.
	if len(offered) < len(out.RequestedPaymentHandlers) {
		return nil
	}

	id := paymentHandlerMismatchID(out.StoreID, date)
	if _, err := s.handlerMismatch.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$setOnInsert": bson.M{"_id": id, "merchant_id": out.MerchantID, "store_id": out.StoreID, "date": date},
		"$set":         bson.M{"updated_at": now, "merchant_declared_handlers": out.DeclaredPaymentHandlers},
	}, options.UpdateOne().SetUpsert(true)); err != nil {
		return fmt.Errorf("ensure payment_handler_mismatch_daily document: %w", err)
	}

	inc := bson.M{"occurrences": 1}
	for _, h := range offered {
		inc["offered_handler_counts."+h] = 1
	}
	if _, err := s.handlerMismatch.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$inc": inc}); err != nil {
		return fmt.Errorf("increment payment_handler_mismatch_daily: %w", err)
	}
	return nil
}

func (s *mongoStore) foldCapabilityGaps(ctx context.Context, out NegotiationOutcome, now time.Time) error {
	missing := missingCapabilities(out.RequestedCapabilities, out.DeclaredCapabilities)
	if len(missing) == 0 {
		return nil
	}
	month := MonthKey(out.OccurredAt)
	surface := string(deriveSurface(profileHost(out.PlatformProfileURI)))

	for _, capabilityID := range missing {
		id := capabilityGapID(out.StoreID, month, capabilityID)
		if _, err := s.capabilityGaps.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
			"$setOnInsert": bson.M{
				"_id": id, "merchant_id": out.MerchantID, "store_id": out.StoreID,
				"month": month, "capability_id": capabilityID,
				"first_seen_at":          out.OccurredAt,
				"estimated_impact_minor": int64(0),
				"impact_confidence":      ImpactUnmeasurable,
				"merchant_supported":     false,
				"remediation_available":  false,
				"recommended_action":     recommendedActionFor(capabilityID),
			},
			"$set": bson.M{"updated_at": now, "last_seen_at": out.OccurredAt},
			"$inc": bson.M{"request_count": 1, "surface_breakdown." + surface: 1},
		}, options.UpdateOne().SetUpsert(true)); err != nil {
			return fmt.Errorf("upsert capability_gaps_monthly %s: %w", id, err)
		}
		if out.PlatformProfileURI != "" {
			if _, err := s.capabilityGaps.UpdateOne(ctx, bson.M{"_id": id},
				bson.M{"$addToSet": bson.M{"distinct_platform_uris": out.PlatformProfileURI}}); err != nil {
				return fmt.Errorf("record distinct platform for capability_gaps_monthly %s: %w", id, err)
			}
		}
	}
	return nil
}

// missingCapabilities returns the elements of requested not present in
// declared, preserving requested's order — the requested-minus-declared
// set R8's capability-gap computation and R5's failure delta both key on.
func missingCapabilities(requested, declared []string) []string {
	declaredSet := make(map[string]struct{}, len(declared))
	for _, d := range declared {
		declaredSet[d] = struct{}{}
	}
	missing := make([]string, 0, len(requested))
	for _, r := range requested {
		if _, ok := declaredSet[r]; !ok {
			missing = append(missing, r)
		}
	}
	return missing
}

func (s *mongoStore) ListProductDemand(ctx context.Context, merchantID, storeID, from, to string) ([]ProductDemandDaily, error) {
	q := bson.M{"merchant_id": merchantID, "date": bson.M{"$gte": from, "$lte": to}}
	if storeID != "" {
		q["store_id"] = storeID
	}
	cur, err := s.productDemand.Find(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list product_demand_daily: %w", err)
	}
	defer func() { _ = cur.Close(ctx) }()
	rows := []ProductDemandDaily{}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("decode product_demand_daily: %w", err)
	}
	return rows, nil
}

func (s *mongoStore) ListNegotiationDaily(ctx context.Context, merchantID, storeID, from, to string) ([]NegotiationDaily, error) {
	q := bson.M{"merchant_id": merchantID, "date": bson.M{"$gte": from, "$lte": to}}
	if storeID != "" {
		q["store_id"] = storeID
	}
	cur, err := s.negotiation.Find(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list negotiation_daily: %w", err)
	}
	defer func() { _ = cur.Close(ctx) }()
	rows := []NegotiationDaily{}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("decode negotiation_daily: %w", err)
	}
	return rows, nil
}

func (s *mongoStore) ListPaymentHandlerMismatchDaily(ctx context.Context, merchantID, storeID, from, to string) ([]PaymentHandlerMismatchDaily, error) {
	q := bson.M{"merchant_id": merchantID, "date": bson.M{"$gte": from, "$lte": to}}
	if storeID != "" {
		q["store_id"] = storeID
	}
	cur, err := s.handlerMismatch.Find(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list payment_handler_mismatch_daily: %w", err)
	}
	defer func() { _ = cur.Close(ctx) }()
	rows := []PaymentHandlerMismatchDaily{}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("decode payment_handler_mismatch_daily: %w", err)
	}
	return rows, nil
}

func (s *mongoStore) ListCapabilityGaps(ctx context.Context, merchantID, storeID, month string) ([]CapabilityGapMonthly, error) {
	q := bson.M{"merchant_id": merchantID, "month": month}
	if storeID != "" {
		q["store_id"] = storeID
	}
	cur, err := s.capabilityGaps.Find(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list capability_gaps_monthly: %w", err)
	}
	defer func() { _ = cur.Close(ctx) }()
	rows := []CapabilityGapMonthly{}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("decode capability_gaps_monthly: %w", err)
	}
	return rows, nil
}

// profileHost extracts the host from a platform_profile_uri for Surface
// derivation — negotiation's platform_profile_uri is a full URI (see
// negotiation.ParsePlatformProfileURI), not just a host, unlike
// CatalogQueryRecordedEvent's AgentPlatform field.
func profileHost(profileURI string) string {
	// Best-effort classification only (mirrors
	// serving.agentPlatformFromHeader's own best-effort contract, never a
	// request decision) — strip scheme, then take everything before the
	// first '/'.
	s := profileURI
	if _, after, ok := strings.Cut(s, "://"); ok {
		s = after
	}
	if before, _, ok := strings.Cut(s, "/"); ok {
		s = before
	}
	return s
}
