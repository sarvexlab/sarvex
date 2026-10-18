// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package generation_test

import (
	"encoding/json"
	"testing"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/generation"
	v20260408 "github.com/sarvexlab/sarvex/internal/spec/v20260408/profile"
)

func sampleProfile(version string) json.RawMessage {
	return json.RawMessage(`{"ucp":{"version":"` + version + `","capabilities":{"dev.ucp.shopping.checkout":[{"version":"` + version + `","spec":"https://ucp.dev/spec/checkout","schema":"https://ucp.dev/schema/checkout"}]}}}`)
}

func TestSniffVersion(t *testing.T) {
	v, err := generation.SniffVersion(sampleProfile("2026-04-08"))
	if err != nil {
		t.Fatalf("SniffVersion() error = %v", err)
	}
	if v != "2026-04-08" {
		t.Errorf("SniffVersion() = %v, want 2026-04-08", v)
	}
}

func TestSniffVersion_MissingVersion(t *testing.T) {
	if _, err := generation.SniffVersion(json.RawMessage(`{"ucp":{}}`)); err == nil {
		t.Error("SniffVersion() with no ucp.version: expected error, got nil")
	}
}

// A version no generation is registered for sniffs fine, and fails to decode.
func TestRegistry_DecodeProfile_UnknownVersion(t *testing.T) {
	reg := generation.NewRegistry()
	reg.Register(v20260408.New())
	if _, err := reg.DecodeProfile(sampleProfile("1999-01-01")); err == nil {
		t.Error("DecodeProfile() with unknown version: expected error, got nil")
	}
}

func TestRegistry_DecodeProfile_ResolvesRegisteredGeneration(t *testing.T) {
	reg := generation.NewRegistry()
	reg.Register(v20260408.New())

	p, err := reg.DecodeProfile(sampleProfile("2026-04-08"))
	if err != nil {
		t.Fatalf("DecodeProfile() error = %v", err)
	}
	if p.UCP.Version != "2026-04-08" {
		t.Errorf("UCP.Version = %v, want 2026-04-08", p.UCP.Version)
	}
	caps := p.UCP.Capabilities
	if len(caps["dev.ucp.shopping.checkout"]) != 1 {
		t.Errorf("UCP.Capabilities = %+v, want one checkout capability", caps)
	}
}

func TestRegistry_DecodeProfile_UnregisteredGeneration(t *testing.T) {
	reg := generation.NewRegistry() // nothing registered

	if _, err := reg.DecodeProfile(sampleProfile("2026-04-08")); err == nil {
		t.Error("DecodeProfile() with no registered generation: expected error, got nil")
	}
}

func TestRegistry_RegisterDuplicateVersionPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Register() with duplicate version: expected panic, got none")
		}
	}()
	reg := generation.NewRegistry()
	reg.Register(v20260408.New())
	reg.Register(v20260408.New())
}

func TestRegistry_All(t *testing.T) {
	reg := generation.NewRegistry()
	reg.Register(v20260408.New())

	all := reg.All()
	if len(all) != 1 || all[0].Version() != "2026-04-08" {
		t.Errorf("All() = %+v, want exactly [v20260408]", all)
	}
}
