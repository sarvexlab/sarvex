// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package codec is the wireadapt codec for UCP 2026-04-08. The canonical
// model's JSON is this generation's wire form, so a response is only given its
// 2026-04-08 "ucp" envelope.
package codec

import (
	"encoding/json"

	v20260408 "github.com/sarvexlab/sarvex/internal/spec/v20260408"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/types"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wireadapt"
)

// Codec is the 2026-04-08 codec.
type Codec struct{}

// New returns the 2026-04-08 codec.
func New() Codec { return Codec{} }

// Version implements wireadapt.Codec.
func (Codec) Version() types.Version { return v20260408.Release }

// DecodeRequest implements wireadapt.Codec: the body is already canonical.
func (Codec) DecodeRequest(body []byte) ([]byte, wireadapt.RequestExtras, error) {
	return body, wireadapt.RequestExtras{}, nil
}

// EncodeResponse implements wireadapt.Codec: the canonical form plus this
// generation's envelope.
func (Codec) EncodeResponse(r wireadapt.Resource, canonical []byte, _ func() json.RawMessage) ([]byte, error) {
	return wireadapt.WithEnvelope(canonical, v20260408.Release, r)
}
