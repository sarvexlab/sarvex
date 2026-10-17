// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package platform_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/pkg/simstore"
	"github.com/sarvexlab/sarvex/internal/platform"
)

const repoRoot = "../.."

func TestEveryPlatformIsComplete(t *testing.T) {
	for _, p := range platform.All() {
		assert.NotEmpty(t, p.Name)
		assert.NotNil(t, p.Connector, "%s: connector factory", p.Name)
		assert.NotNil(t, p.Validator, "%s: validator factory", p.Name)
		assert.NotEmpty(t, p.ConfigSchema, "%s: config schema", p.Name)
		assert.True(t, platform.Supported(p.Name))
		got, ok := platform.Get(p.Name)
		assert.True(t, ok)
		assert.Equal(t, p.Plugin, got.Plugin)
	}
	assert.False(t, platform.Supported("shopify"), "frozen connectors are not served")
}

// Every merchant-side plugin belongs to exactly one registered platform, and
// every registered platform names a plugin that exists — the PHP side is part
// of the platform module too.
func TestPluginsMatchRegisteredPlatforms(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join(repoRoot, "plugin"))
	require.NoError(t, err)
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, "plugin/"+e.Name())
		}
	}
	var declared []string
	for _, p := range platform.All() {
		declared = append(declared, p.Plugin)
		info, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(p.Plugin)))
		if assert.NoError(t, err, "%s: plugin %s", p.Name, p.Plugin) {
			assert.True(t, info.IsDir())
		}
	}
	slices.Sort(dirs)
	slices.Sort(declared)
	assert.Equal(t, dirs, declared, "plugin/ directories and registered platforms' plugins differ")
}

// The control-plane contract (sarvex-v1-control-plane.yaml) is authoritative
// (sarvex-v1-architecture.md), so a registered platform must be one the contract accepts
// and must have its credentials schema there.
func TestControlPlaneContractListsEveryPlatform(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot, "sarvex-v1-control-plane.yaml"))
	require.NoError(t, err)
	enum := regexp.MustCompile(`(?m)^    platform:\n      type: string\n      enum: \[([^\]]*)\]`).FindSubmatch(raw)
	require.NotNil(t, enum, "platform enum not found in the contract")
	accepted := regexp.MustCompile(`[a-z_]+`).FindAllString(string(enum[1]), -1)
	for _, p := range platform.All() {
		assert.Contains(t, accepted, string(p.Name), "contract platform enum")
		assert.Contains(t, string(raw), "store_credentials_"+string(p.Name)+":", "contract credentials schema")
	}
}

// The simulator gives each platform's simulated store its own routes, and the
// first platform to claim a path serves it, so two platforms claiming one path
// would leave the second's sim unreachable. Registering every surface on one
// mux panics on exactly that overlap. A newly scaffolded platform fails here
// until its sim speaks its own store API instead of its template's.
func TestSimSurfacesDoNotOverlap(t *testing.T) {
	mux := http.NewServeMux()
	for _, p := range platform.All() {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s: simulated store claims a route another platform's already does: %s", p.Name, fmt.Sprint(r))
				}
			}()
			p.Sim.Register(mux, simstore.Env{})
		}()
	}
}
