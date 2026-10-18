// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package domain

// Plan is the commercial shape a merchant is on.
type Plan string

const (
	PlanSelfServe Plan = "self_serve"
	PlanConcierge Plan = "concierge"
	PlanScale     Plan = "scale"
)

// IsConcierge reports whether p is a plan tier where Sarvex does hands-on
// work for the merchant (concierge and scale), as opposed to self_serve,
// where the merchant drives their own Path. Used to decide who owns a
// prescribed task — see validation.Orchestrator.milestoneOwner and
// monitoring.TaskServiceRouter.RouteFindings.
func (p Plan) IsConcierge() bool {
	return p == PlanConcierge || p == PlanScale
}

// BillingStatus reflects what the payment provider reports.
type BillingStatus string

const (
	BillingActive           BillingStatus = "active"
	BillingActiveViaInvoice BillingStatus = "active_via_invoice"
	BillingPastDue          BillingStatus = "past_due"
	BillingSuspended        BillingStatus = "suspended"
	BillingCanceled         BillingStatus = "canceled"
)

// DefaultMemberCap is the team-member seat cap applied when a merchant's
// billing record doesn't yet carry a plan-specific value (no subscription
// yet, or the record predates this field).
const DefaultMemberCap = 5

// BillingUsage tracks consumption against the plan cap.
type BillingUsage struct {
	SKUCount  int `json:"sku_count"`
	SKUCap    int `json:"sku_cap"`
	MemberCap int `json:"member_cap"`
}

// Billing is the provider-authoritative projection of a merchant's subscription.
type Billing struct {
	Plan    Plan          `json:"plan"`
	Status  BillingStatus `json:"status"`
	SKUBand string        `json:"sku_band"`
	Usage   BillingUsage  `json:"usage"`
}

// HostedURL wraps a Stripe-hosted URL.
type HostedURL struct {
	URL string `json:"url"`
}

// NotificationPreferences are the merchant's alert channel settings.
type NotificationPreferences struct {
	EmailEnabled bool    `json:"email_enabled"`
	WeeklyDigest bool    `json:"weekly_digest"`
	AlertEmail   *string `json:"alert_email"`

	// AlertRecipients, AlertSeverityFloor, and the QuietHours* fields
	// configure endpoint-health alert delivery specifically (see
	// internal/planes/control/endpointhealth). Additive to the pre-existing
	// fields above, which nothing else in the codebase currently reads
	// (see architecture doc Gaps) — extending rather than replacing them
	// keeps that surface unchanged for whenever it does gain a caller.
	AlertRecipients []string `json:"alert_recipients,omitempty"`
	// AlertSeverityFloor is "degraded" or "critical" — alerts below this
	// severity are not delivered. Empty means "critical" (the safe
	// default: don't spam on every degraded blip until a merchant opts in).
	AlertSeverityFloor  string `json:"alert_severity_floor,omitempty"`
	QuietHoursStartHour *int   `json:"quiet_hours_start_hour,omitempty"` // 0-23
	QuietHoursEndHour   *int   `json:"quiet_hours_end_hour,omitempty"`   // 0-23
	QuietHoursTimezone  string `json:"quiet_hours_timezone,omitempty"`   // IANA name, e.g. "America/New_York"
}
