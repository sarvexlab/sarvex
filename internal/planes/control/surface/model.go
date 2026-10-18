// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package surface

import (
	"time"

	"github.com/samber/lo"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// EnrollmentModel is the MongoDB document stored in the
// control_store_surfaces collection.
//
// R14 specifies these as a Firestore subcollection at
// merchants/{merchant_id}/stores/{store_id}/surfaces/{surface_id}. The
// store here is MongoDB, which has flat collections and no document paths,
// so the same hierarchy is expressed as a composite _id plus indexed
// merchant_id/store_id fields — exactly the shape control_checklist_items
// already uses for per-store, per-item records. Reads are scoped by
// (merchant_id, store_id) together, per sarvex-v1-architecture.md A10.4.
type EnrollmentModel struct {
	// ID is "{merchant_id}:{store_id}:{surface_id}".
	ID               string                   `bson:"_id"`
	MerchantID       string                   `bson:"merchant_id"`
	StoreID          string                   `bson:"store_id"`
	SurfaceID        string                   `bson:"surface_id"`
	State            string                   `bson:"state"`
	Prerequisites    []PrerequisiteResultItem `bson:"prerequisites"`
	Artifacts        []ArtifactItem           `bson:"artifacts"`
	Config           map[string]string        `bson:"config,omitempty"`
	EnrolledAt       *time.Time               `bson:"enrolled_at,omitempty"`
	LastVerifiedAt   *time.Time               `bson:"last_verified_at,omitempty"`
	SuspensionReason string                   `bson:"suspension_reason,omitempty"`
	CreatedAt        time.Time                `bson:"created_at"`
	UpdatedAt        time.Time                `bson:"updated_at"`
}

// DocumentID builds the composite _id for one store's enrollment on one
// surface.
func DocumentID(merchantID domain.MerchantID, storeID domain.StoreID, surfaceID string) string {
	return string(merchantID) + ":" + string(storeID) + ":" + surfaceID
}

// PrerequisiteResultItem is the stored form of a check verdict.
type PrerequisiteResultItem struct {
	CheckID        string    `bson:"check_id"`
	LabelKey       string    `bson:"label_key"`
	Passed         bool      `bson:"passed"`
	Reason         string    `bson:"reason,omitempty"`
	RemediationKey string    `bson:"remediation_key,omitempty"`
	RemediationURL string    `bson:"remediation_url,omitempty"`
	EvaluatedAt    time.Time `bson:"evaluated_at"`
}

// ArtifactItem is the stored form of a generated artifact.
type ArtifactItem struct {
	ArtifactID  string            `bson:"artifact_id"`
	LabelKey    string            `bson:"label_key"`
	URL         string            `bson:"url,omitempty"`
	Detail      map[string]string `bson:"detail,omitempty"`
	GeneratedAt *time.Time        `bson:"generated_at,omitempty"`
}

// ToDomain renders the stored enrollment. The descriptor is attached by the
// service (the model does not know the registry), so callers should use
// Service.List/Get rather than this directly when they need the full view.
func (m *EnrollmentModel) ToDomain() domain.SurfaceEnrollment {
	return domain.SurfaceEnrollment{
		SurfaceID: m.SurfaceID,
		StoreID:   domain.StoreID(m.StoreID),
		State:     domain.SurfaceState(m.State),
		Prerequisites: lo.Map(m.Prerequisites, func(p PrerequisiteResultItem, _ int) domain.PrerequisiteResult {
			return domain.PrerequisiteResult{
				CheckID:        p.CheckID,
				LabelKey:       p.LabelKey,
				Passed:         p.Passed,
				Reason:         p.Reason,
				RemediationKey: p.RemediationKey,
				RemediationURL: p.RemediationURL,
				EvaluatedAt:    p.EvaluatedAt,
			}
		}),
		Artifacts: lo.Map(m.Artifacts, func(a ArtifactItem, _ int) domain.SurfaceArtifact {
			return domain.SurfaceArtifact{
				ArtifactID:  a.ArtifactID,
				LabelKey:    a.LabelKey,
				URL:         a.URL,
				Detail:      a.Detail,
				GeneratedAt: a.GeneratedAt,
			}
		}),
		Config:           m.Config,
		EnrolledAt:       m.EnrolledAt,
		LastVerifiedAt:   m.LastVerifiedAt,
		SuspensionReason: m.SuspensionReason,
		UpdatedAt:        m.UpdatedAt,
	}
}

func prerequisiteItems(results []domain.PrerequisiteResult) []PrerequisiteResultItem {
	return lo.Map(results, func(p domain.PrerequisiteResult, _ int) PrerequisiteResultItem {
		return PrerequisiteResultItem{
			CheckID:        p.CheckID,
			LabelKey:       p.LabelKey,
			Passed:         p.Passed,
			Reason:         p.Reason,
			RemediationKey: p.RemediationKey,
			RemediationURL: p.RemediationURL,
			EvaluatedAt:    p.EvaluatedAt,
		}
	})
}

func artifactItems(artifacts []domain.SurfaceArtifact) []ArtifactItem {
	return lo.Map(artifacts, func(a domain.SurfaceArtifact, _ int) ArtifactItem {
		return ArtifactItem{
			ArtifactID:  a.ArtifactID,
			LabelKey:    a.LabelKey,
			URL:         a.URL,
			Detail:      a.Detail,
			GeneratedAt: a.GeneratedAt,
		}
	})
}
