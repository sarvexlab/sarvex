// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package domain

import "time"

// SurfaceProtocol names the wire protocol an agent surface speaks. It is
// data on the descriptor, not a branch anywhere in code — a surface with a
// protocol Sarvex does not yet implement simply has adapters that say so.
type SurfaceProtocol string

const (
	SurfaceProtocolUCP SurfaceProtocol = "ucp"
	SurfaceProtocolACP SurfaceProtocol = "acp"
)

// SurfaceState is a store's enrollment position on one agent surface.
//
// Per-store, per-surface, zero-to-many. Surfaces never gate each other, and
// a surface's state never alters the store's UCPState — the single
// cross-lifecycle rule runs the other way: enrollment cannot progress past
// SurfaceAvailable unless the store is UCPStateActive.
type SurfaceState string

const (
	// SurfaceAvailable — the surface is in the registry and the store is
	// not enrolled. Every registered surface reads as available until the
	// merchant acts.
	SurfaceAvailable SurfaceState = "available"
	// SurfacePrerequisitesPending — enrollment started; at least one
	// prerequisite check is failing.
	SurfacePrerequisitesPending SurfaceState = "prerequisites_pending"
	// SurfaceConfigured — prerequisites pass and any Sarvex-generated
	// artifacts exist.
	SurfaceConfigured SurfaceState = "configured"
	// SurfaceSubmitted — the merchant has completed the surface's external
	// steps and told Sarvex so.
	SurfaceSubmitted SurfaceState = "submitted"
	// SurfaceVerifying — awaiting the surface's verification signal.
	SurfaceVerifying SurfaceState = "verifying"
	// SurfaceEnrolled — the verification signal arrived; the store is live
	// on this surface.
	SurfaceEnrolled SurfaceState = "enrolled"
	// SurfaceSuspended — the surface withdrew or suspended the store.
	// Store-level activation is unaffected, as are all other surfaces.
	SurfaceSuspended SurfaceState = "suspended"
)

// PrerequisiteResult is one named check's verdict for one store, in terms a
// merchant can act on.
type PrerequisiteResult struct {
	CheckID string `json:"check_id"`
	// LabelKey is a vue-i18n key, not display text: the console owns copy,
	// the descriptor owns identity.
	LabelKey string `json:"label_key"`
	Passed   bool   `json:"passed"`
	// Reason is merchant-readable and explains a failure. Empty on pass.
	Reason string `json:"reason,omitempty"`
	// RemediationKey is a vue-i18n key for the fix, when one applies.
	RemediationKey string `json:"remediation_key,omitempty"`
	// RemediationURL points at wherever the merchant performs the fix,
	// usually outside Sarvex.
	RemediationURL string    `json:"remediation_url,omitempty"`
	EvaluatedAt    time.Time `json:"evaluated_at"`
}

// SurfaceArtifact is something Sarvex generates and hosts on a surface's
// behalf — scoped to that surface, and generated only for stores enrolling
// in it.
type SurfaceArtifact struct {
	ArtifactID string `json:"artifact_id"`
	LabelKey   string `json:"label_key"`
	// URL is where the artifact is served, when it has a stable address.
	URL string `json:"url,omitempty"`
	// Detail carries artifact-specific read-only facts for display
	// (counts, timestamps). Shape is the adapter's business; the console
	// renders it generically as label/value rows.
	Detail      map[string]string `json:"detail,omitempty"`
	GeneratedAt *time.Time        `json:"generated_at,omitempty"`
}

// SurfaceExternalStep is an action the merchant performs outside Sarvex —
// a form to submit, an approval to wait for. Sarvex can describe it and
// record that the merchant says it is done; it cannot perform it.
type SurfaceExternalStep struct {
	StepID   string `json:"step_id"`
	LabelKey string `json:"label_key"`
	URL      string `json:"url,omitempty"`
	// Owner distinguishes "you do this" from "they do this" so the console
	// can render waiting states without surface-specific knowledge.
	Owner SurfaceStepOwner `json:"owner"`
}

// SurfaceStepOwner names who an external step is waiting on.
type SurfaceStepOwner string

const (
	SurfaceStepOwnerMerchant SurfaceStepOwner = "merchant"
	SurfaceStepOwnerSurface  SurfaceStepOwner = "surface"
)

// SurfaceConfigFieldType is the input kind of one merchant-editable config
// field. The console renders a form from these with no per-surface code.
type SurfaceConfigFieldType string

const (
	SurfaceConfigText SurfaceConfigFieldType = "text"
	SurfaceConfigBool SurfaceConfigFieldType = "bool"
	SurfaceConfigList SurfaceConfigFieldType = "string_list"
	SurfaceConfigJSON SurfaceConfigFieldType = "json"
)

// SurfaceConfigField declares one entry of a surface's merchant-supplied
// configuration. Values are stored as strings (SurfaceEnrollment.Config); a
// list is newline-separated and json is a JSON document, both validated on
// write. Never a secret: nothing here is a credential.
type SurfaceConfigField struct {
	Key      string                 `json:"key"`
	Type     SurfaceConfigFieldType `json:"type"`
	LabelKey string                 `json:"label_key"`
	// HelpKey is an optional vue-i18n key for help text.
	HelpKey  string `json:"help_key,omitempty"`
	Required bool   `json:"required"`
}

// SurfaceDescriptor is the whole of what makes a surface a surface. Adding
// a surface means adding one of these plus its adapter implementations —
// no core onboarding, state machine, routing, or console code changes.
type SurfaceDescriptor struct {
	SurfaceID string `json:"surface_id"`
	// DisplayNameKey and DescriptionKey are vue-i18n keys.
	DisplayNameKey string          `json:"display_name_key"`
	DescriptionKey string          `json:"description_key"`
	Protocol       SurfaceProtocol `json:"protocol"`
	// RequiredConnectors names connector types that must be linked to the
	// store before enrollment can leave prerequisites_pending.
	RequiredConnectors []ConnectorType `json:"required_connectors"`
	// RequiredCapabilities are UCP capability identifiers the store must
	// declare. Drawn from internal/pkg/domain.CapabilityType only — never
	// free-form (see surface.Registry.Validate).
	RequiredCapabilities []string `json:"required_capabilities"`
	// PrerequisiteChecks names the checks this surface runs, in order.
	// Each name resolves to an adapter implementation at registration.
	PrerequisiteChecks []string `json:"prerequisite_checks"`
	// Artifacts names the Sarvex-generated, surface-scoped artifacts.
	Artifacts []string `json:"artifacts"`
	// ConfigSchema declares the merchant-editable configuration. Empty means
	// the surface takes free-form config (or none), which is how every
	// surface behaved before schemas existed.
	ConfigSchema []SurfaceConfigField `json:"config_schema"`
	// ExternalSteps are merchant actions outside Sarvex.
	ExternalSteps []SurfaceExternalStep `json:"external_steps"`
	// VerificationSignal names what proves enrollment succeeded.
	VerificationSignal string `json:"verification_signal"`
	// AutoEnroll marks a surface that enrolls itself the moment the store
	// reaches active, with no merchant action. direct_agent is the only
	// V1 instance: it represents the plain fact that any agent discovering
	// the profile can transact, and exists so that "active with no
	// distribution partner" is explicit rather than implied.
	AutoEnroll bool `json:"auto_enroll"`
}

// SurfaceEnrollment is a store's position on one surface, as served by
// GET /stores/{store_id}/surfaces.
type SurfaceEnrollment struct {
	SurfaceID        string               `json:"surface_id"`
	StoreID          StoreID              `json:"store_id"`
	State            SurfaceState         `json:"state"`
	Prerequisites    []PrerequisiteResult `json:"prerequisites"`
	Artifacts        []SurfaceArtifact    `json:"artifacts"`
	Config           map[string]string    `json:"config,omitempty"`
	EnrolledAt       *time.Time           `json:"enrolled_at,omitempty"`
	LastVerifiedAt   *time.Time           `json:"last_verified_at,omitempty"`
	SuspensionReason string               `json:"suspension_reason,omitempty"`
	UpdatedAt        time.Time            `json:"updated_at"`
	// Descriptor is echoed alongside state so the console renders the whole
	// flow from one response, with no per-surface knowledge of its own.
	Descriptor SurfaceDescriptor `json:"descriptor"`
	// Blocked is true when the store is not yet UCPStateActive, which is
	// the one thing that holds enrollment at available.
	Blocked       bool   `json:"blocked"`
	BlockedReason string `json:"blocked_reason,omitempty"`
}
