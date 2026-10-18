// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package profile is the UCP 2026-08-25 generation's generation.Generation: it reads and
// writes profile documents in that generation's form (spec/v20260825/wire).
package profile

import (
	"encoding/json"
	"fmt"

	v20260825 "github.com/sarvexlab/sarvex/internal/spec/v20260825"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/types"
	ucpwire "github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
	"github.com/sarvexlab/sarvex/internal/spec/v20260825/wire"
)

// Generation implements generation.Generation for the UCP 2026-08-25
// spec.
type Generation struct{}

// New returns the 2026-08-25 Generation adapter.
func New() Generation { return Generation{} }

// Version implements generation.Generation.
func (Generation) Version() types.Version { return v20260825.Release }

// DecodeProfile implements generation.Generation.
func (Generation) DecodeProfile(raw json.RawMessage) (*ucpwire.Profile, error) {
	var p wire.Profile
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("spec/v20260825/profile: decode profile: %w", err)
	}
	return p.Canonical(), nil
}

// EncodeProfile implements generation.Generation.
func (Generation) EncodeProfile(p *ucpwire.Profile) ([]byte, error) {
	return json.Marshal(wire.FromCanonical(p))
}
