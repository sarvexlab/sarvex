// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package account

import (
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// mongoAccountRepo implements AccountRepository against the control_accounts collection.
type mongoAccountRepo struct {
	col *mongo.Collection
}

// NewMongoAccountRepository creates an AccountRepository backed by MongoDB.
// The returned value also implements AccountListRepository (the operator list surface).
func NewMongoAccountRepository(db *mongo.Database) (AccountListRepository, error) {
	r := &mongoAccountRepo{col: db.Collection("control_accounts")}
	// _id is merchant_id — unique by default; no extra indexes needed.
	return r, nil
}

func (r *mongoAccountRepo) Get(ctx context.Context, id domain.MerchantID) (*AccountModel, error) {
	var m AccountModel
	err := r.col.FindOne(ctx, bson.M{"_id": string(id)}).Decode(&m)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get account %s: %w", id, err)
	}
	return &m, nil
}

func (r *mongoAccountRepo) Upsert(ctx context.Context, m *AccountModel) error {
	opts := options.UpdateOne().SetUpsert(true)
	_, err := r.col.UpdateOne(ctx,
		bson.M{"_id": m.ID},
		bson.M{"$set": m},
		opts,
	)
	if err != nil {
		return fmt.Errorf("upsert account %s: %w", m.ID, err)
	}
	return nil
}

func (r *mongoAccountRepo) List(ctx context.Context, afterID string, limit int) ([]*AccountModel, bool, error) {
	filter := bson.M{}
	if afterID != "" {
		filter["_id"] = bson.M{"$gt": afterID}
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "_id", Value: 1}}).
		SetLimit(int64(limit + 1))

	cursor, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, false, fmt.Errorf("list accounts: %w", err)
	}
	defer cursor.Close(ctx)

	var accounts []*AccountModel
	if err = cursor.All(ctx, &accounts); err != nil {
		return nil, false, fmt.Errorf("decode accounts: %w", err)
	}
	hasMore := len(accounts) > limit
	if hasMore {
		accounts = accounts[:limit]
	}
	return accounts, hasMore, nil
}

// mongoMemberRepo implements MemberRepository against the control_members collection.
type mongoMemberRepo struct {
	col *mongo.Collection
}

// NewMongoMemberRepository creates a MemberRepository backed by MongoDB.
func NewMongoMemberRepository(db *mongo.Database) (MemberRepository, error) {
	r := &mongoMemberRepo{col: db.Collection("control_members")}
	if err := r.createIndexes(context.Background()); err != nil {
		return nil, fmt.Errorf("control_members indexes: %w", err)
	}
	return r, nil
}

func (r *mongoMemberRepo) createIndexes(ctx context.Context) error {
	_, err := r.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "merchant_id", Value: 1}, {Key: "user_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{Keys: bson.D{{Key: "merchant_id", Value: 1}}},
	})
	return err
}

func (r *mongoMemberRepo) ListByMerchant(ctx context.Context, id domain.MerchantID, afterUserID string, limit int) ([]*MemberModel, bool, error) {
	filter := bson.M{"merchant_id": string(id)}
	if afterUserID != "" {
		filter["user_id"] = bson.M{"$gt": afterUserID}
	}

	// Fetch one extra to determine whether there is a next page.
	opts := options.Find().
		SetSort(bson.D{{Key: "user_id", Value: 1}}).
		SetLimit(int64(limit + 1))

	cursor, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, false, fmt.Errorf("list members for merchant %s: %w", id, err)
	}
	defer cursor.Close(ctx)

	var members []*MemberModel
	if err = cursor.All(ctx, &members); err != nil {
		return nil, false, fmt.Errorf("decode members for merchant %s: %w", id, err)
	}

	hasMore := len(members) > limit
	if hasMore {
		members = members[:limit]
	}
	return members, hasMore, nil
}

func (r *mongoMemberRepo) UpsertMember(ctx context.Context, m *MemberModel) error {
	opts := options.UpdateOne().SetUpsert(true)
	_, err := r.col.UpdateOne(ctx,
		bson.M{"merchant_id": m.MerchantID, "email": m.Email},
		bson.M{"$set": m},
		opts,
	)
	if err != nil {
		return fmt.Errorf("upsert member %s/%s: %w", m.MerchantID, m.Email, err)
	}
	return nil
}

func (r *mongoMemberRepo) GetByEmail(ctx context.Context, merchantID domain.MerchantID, email string) (*MemberModel, error) {
	var m MemberModel
	err := r.col.FindOne(ctx, bson.M{
		"merchant_id": string(merchantID),
		"email":       email,
	}).Decode(&m)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get member %s/%s: %w", merchantID, email, err)
	}
	return &m, nil
}

func (r *mongoMemberRepo) GetInvitedByEmail(ctx context.Context, email string) (*MemberModel, error) {
	var m MemberModel
	err := r.col.FindOne(ctx, bson.M{
		"email":  email,
		"status": string(domain.MemberStatusInvited),
	}).Decode(&m)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get invited member %s: %w", email, err)
	}
	return &m, nil
}

func (r *mongoMemberRepo) GetActiveByEmail(ctx context.Context, email string) (*MemberModel, error) {
	var m MemberModel
	err := r.col.FindOne(ctx, bson.M{
		"email":  email,
		"status": string(domain.MemberStatusActive),
	}).Decode(&m)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get active member %s: %w", email, err)
	}
	return &m, nil
}
