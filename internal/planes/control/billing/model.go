// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package billing

import (
	"time"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// BillingRecordModel is the MongoDB document in the control_billing_records collection.
// _id is the merchant_id — one document per tenant.
type BillingRecordModel struct {
	MerchantID           string    `bson:"_id"`
	StripeCustomerID     string    `bson:"stripe_customer_id"`
	StripeSubscriptionID string    `bson:"stripe_subscription_id"`
	Plan                 string    `bson:"plan"`
	Status               string    `bson:"status"`
	SKUBand              string    `bson:"sku_band"`
	SKUCap               int       `bson:"sku_cap"`
	SKUCount             int       `bson:"sku_count"`
	MemberCap            int       `bson:"member_cap"`
	UpdatedAt            time.Time `bson:"updated_at"`
}

func (m *BillingRecordModel) ToDomain() domain.Billing {
	memberCap := m.MemberCap
	if memberCap <= 0 {
		memberCap = domain.DefaultMemberCap
	}
	return domain.Billing{
		Plan:    domain.Plan(m.Plan),
		Status:  domain.BillingStatus(m.Status),
		SKUBand: m.SKUBand,
		Usage: domain.BillingUsage{
			SKUCount:  m.SKUCount,
			SKUCap:    m.SKUCap,
			MemberCap: memberCap,
		},
	}
}

// NotificationPrefsModel is the MongoDB document in the control_notification_prefs collection.
// _id is the merchant_id — one document per tenant.
type NotificationPrefsModel struct {
	MerchantID          string   `bson:"_id"`
	EmailEnabled        bool     `bson:"email_enabled"`
	WeeklyDigest        bool     `bson:"weekly_digest"`
	AlertEmail          *string  `bson:"alert_email,omitempty"`
	AlertRecipients     []string `bson:"alert_recipients,omitempty"`
	AlertSeverityFloor  string   `bson:"alert_severity_floor,omitempty"`
	QuietHoursStartHour *int     `bson:"quiet_hours_start_hour,omitempty"`
	QuietHoursEndHour   *int     `bson:"quiet_hours_end_hour,omitempty"`
	QuietHoursTimezone  string   `bson:"quiet_hours_timezone,omitempty"`
}

func (m *NotificationPrefsModel) ToDomain() domain.NotificationPreferences {
	return domain.NotificationPreferences{
		EmailEnabled:        m.EmailEnabled,
		WeeklyDigest:        m.WeeklyDigest,
		AlertEmail:          m.AlertEmail,
		AlertRecipients:     m.AlertRecipients,
		AlertSeverityFloor:  m.AlertSeverityFloor,
		QuietHoursStartHour: m.QuietHoursStartHour,
		QuietHoursEndHour:   m.QuietHoursEndHour,
		QuietHoursTimezone:  m.QuietHoursTimezone,
	}
}
