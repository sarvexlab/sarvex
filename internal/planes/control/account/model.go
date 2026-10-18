// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package account

import (
	"time"

	"github.com/samber/lo"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// AccountModel is the MongoDB document stored in the control_accounts collection.
// _id is the merchant_id — one document per tenant.
type AccountModel struct {
	ID            string             `bson:"_id"`
	Name          string             `bson:"name"`
	Timezone      string             `bson:"timezone"`
	BusinessFacts BusinessFactsModel `bson:"business_facts"`
	CreatedAt     time.Time          `bson:"created_at"`
	UpdatedAt     time.Time          `bson:"updated_at"`
	CountryCode   string             `bson:"country_code"`
}

// BusinessFactsModel is the embedded sub-document for Google-required merchant facts.
type BusinessFactsModel struct {
	LegalEntityName    string   `bson:"legal_entity_name"`
	SupportEmail       string   `bson:"support_email"`
	SupportPhone       string   `bson:"support_phone"`
	FulfillmentRegions []string `bson:"fulfillment_regions"`
}

func (m *AccountModel) ToDomain() domain.Account {
	return domain.Account{
		MerchantID: domain.MerchantID(m.ID),
		Name:       m.Name,
		Timezone:   m.Timezone,
		BusinessFacts: domain.BusinessFacts{
			LegalEntityName:    m.BusinessFacts.LegalEntityName,
			SupportEmail:       m.BusinessFacts.SupportEmail,
			SupportPhone:       m.BusinessFacts.SupportPhone,
			FulfillmentRegions: m.BusinessFacts.FulfillmentRegions,
		},
		CreatedAt:   m.CreatedAt,
		CountryCode: m.CountryCode,
	}
}

// MemberModel is the MongoDB document stored in the control_members collection.
// Email, Name, and CountryCode are denormalized from the identity provider at invite/create time
// to avoid per-member IdP lookups on list reads.
type MemberModel struct {
	UserID      string   `bson:"user_id"`
	MerchantID  string   `bson:"merchant_id"`
	Email       string   `bson:"email"`
	Name        string   `bson:"name"`
	Roles       []string `bson:"roles"`
	Status      string   `bson:"status"`
	CountryCode string   `bson:"country_code"`
}

func (m *MemberModel) ToDomain() domain.Member {
	roles := lo.Map(m.Roles, func(r string, _ int) domain.MemberRole { return domain.MemberRole(r) })
	return domain.Member{
		UserID:      m.UserID,
		Email:       m.Email,
		Name:        m.Name,
		Roles:       roles,
		Status:      domain.MemberStatus(m.Status),
		CountryCode: m.CountryCode,
	}
}
