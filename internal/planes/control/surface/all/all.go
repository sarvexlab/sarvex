// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package all is the control plane's surface registration root: one line per
// surface. The composition root builds the registry from Registrations and
// names no surface itself.
package all

import (
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/sarvexlab/sarvex/internal/planes/control/surface"
	"github.com/sarvexlab/sarvex/internal/planes/control/surface/directagent"
)

// Registrations returns every registered surface.
func Registrations(db *mongo.Database) []surface.Registration {
	_ = surface.NewMongoSyncedArtifactReader(db)
	return []surface.Registration{
		directagent.Registration(),
	}
}
