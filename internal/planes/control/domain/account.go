// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package domain

import "time"

// Account is the merchant's top-level record.
type Account struct {
	MerchantID    MerchantID    `json:"merchant_id"`
	Name          string        `json:"name"`
	Timezone      string        `json:"timezone"`
	BusinessFacts BusinessFacts `json:"business_facts"`
	CreatedAt     time.Time     `json:"created_at"`
	CountryCode   string        `json:"country_code"`
}

// BusinessFacts are the Google-required facts collected at onboarding.
type BusinessFacts struct {
	LegalEntityName    string   `json:"legal_entity_name"`
	SupportEmail       string   `json:"support_email"`
	SupportPhone       string   `json:"support_phone"`
	FulfillmentRegions []string `json:"fulfillment_regions"`
}

// AccountUpdate carries the fields a merchant may change.
type AccountUpdate struct {
	Name          *string        `json:"name,omitempty"`
	Timezone      *string        `json:"timezone,omitempty"`
	CountryCode   *string        `json:"country_code,omitempty"`
	BusinessFacts *BusinessFacts `json:"business_facts,omitempty"`
}

// Member is a team member on a merchant account.
type Member struct {
	UserID      string       `json:"user_id"`
	Email       string       `json:"email"`
	Status      MemberStatus `json:"status"`
	Name        string       `json:"full_name"`
	Roles       []MemberRole `json:"roles"`
	CountryCode string       `json:"country_code"`
}

type MemberRole string

const (
	MemberRoleOwner MemberRole = "owner"
	MemberRoleStaff MemberRole = "staff"
)

// ToRole converts a MemberRole to a Role for use in the Principal.
func (mr MemberRole) ToRole() Role {
	switch mr {
	case MemberRoleOwner:
		return RoleOwner
	case MemberRoleStaff:
		return RoleStaff
	default:
		return ""
	}
}

type MemberStatus string

const (
	MemberStatusActive   MemberStatus = "active"
	MemberStatusInvited  MemberStatus = "invited"
	MemberStatusInactive MemberStatus = "inactive"
	MemberStatusPending  MemberStatus = "pending"
)

// LoginRequest is the body for POST /auth/login.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// InviteAcceptRequest is the body for POST /auth/invites/accept.
type InviteAcceptRequest struct {
	InviteToken string `json:"invite_token"`
	Password    string `json:"password"`
}
