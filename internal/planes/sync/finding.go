// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package sync

// Finding represents a single readiness or consistency issue on a product.
// It is immutable once stored and tied to a specific point-in-time assessment.
type Finding struct {
	Code           string                 `json:"code" bson:"code"`
	Message        string                 `json:"message" bson:"message"`
	Severity       string                 `json:"severity" bson:"severity"` // "info", "warning", "critical"
	Field          string                 `json:"field" bson:"field"`       // "price", "color", "title", etc.
	RequiredFor    string                 `json:"required_for" bson:"required_for"`
	ActionableBy   string                 `json:"actionable_by" bson:"actionable_by"` // "rule", "manual", "merchant_action"
	BlocksCheckout bool                   `json:"blocks_checkout" bson:"blocks_checkout"`
	Details        map[string]interface{} `json:"details,omitempty" bson:"details,omitempty"`
}

// FindingBuilder constructs Finding objects with fluent API.
type FindingBuilder struct {
	f *Finding
}

// NewFinding returns a new FindingBuilder.
func NewFinding(code, message, severity string) *FindingBuilder {
	return &FindingBuilder{
		f: &Finding{
			Code:     code,
			Message:  message,
			Severity: severity,
		},
	}
}

// Field sets the affected field name.
func (fb *FindingBuilder) Field(field string) *FindingBuilder {
	fb.f.Field = field
	return fb
}

// RequiredFor sets why this field is required.
func (fb *FindingBuilder) RequiredFor(req string) *FindingBuilder {
	fb.f.RequiredFor = req
	return fb
}

// ActionableBy sets who can fix this issue.
func (fb *FindingBuilder) ActionableBy(actor string) *FindingBuilder {
	fb.f.ActionableBy = actor
	return fb
}

// BlocksCheckout marks this finding as blocking transactions.
func (fb *FindingBuilder) BlocksCheckout(blocks bool) *FindingBuilder {
	fb.f.BlocksCheckout = blocks
	return fb
}

// Details adds contextual metadata.
func (fb *FindingBuilder) Details(details map[string]interface{}) *FindingBuilder {
	fb.f.Details = details
	return fb
}

// Build returns the completed Finding.
func (fb *FindingBuilder) Build() *Finding {
	return fb.f
}
