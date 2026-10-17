// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package scaffold_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/types"
	"github.com/sarvexlabs/sarvex.cp/internal/scaffold"
)

const mod = "github.com/sarvexlabs/sarvex.cp/"

// repo writes files (repo-relative path → contents) under a fresh root.
func repo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for path, body := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o750))
		require.NoError(t, os.WriteFile(full, []byte(body), 0o600))
	}
	return root
}

func read(t *testing.T, root, path string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path))) //nolint:gosec // G304: a file under this test's temp dir
	require.NoError(t, err)
	return string(b)
}

const specAll = `package all

import (
	"github.com/sarvexlabs/sarvex.cp/internal/spec"
	v20260101 "github.com/sarvexlabs/sarvex.cp/internal/spec/v20260101/release"
)

func New() *spec.Registry {
	return spec.NewRegistry(
		v20260101.New(),
	)
}
`

func specRepo(t *testing.T) string {
	return repo(t, map[string]string{
		"internal/spec/all/all.go": specAll,
		"internal/spec/v20260101/release.go": `package v20260101

const Release = "2026-01-01"
`,
		"internal/spec/v20260101/codec/v20260101.go": `package codec

import v20260101 "github.com/sarvexlabs/sarvex.cp/internal/spec/v20260101"

var at = v20260101.Release
`,
		"internal/spec/v20260101/DISCREPANCIES.md": "# 2026-01-01\n\nthe template's own gaps\n",
	})
}

func TestSpecVersion_CopiesRenamesAndRegisters(t *testing.T) {
	root := specRepo(t)

	res, err := scaffold.SpecVersion(root, "2026-01-01", "2026-12-01", []types.Version{"2026-01-01"})
	require.NoError(t, err)

	assert.Equal(t, "package v20261201\n\nconst Release = \"2026-12-01\"\n", read(t, root, "internal/spec/v20261201/release.go"))
	codec := read(t, root, "internal/spec/v20261201/codec/v20261201.go")
	assert.Contains(t, codec, `v20261201 "`+mod+`internal/spec/v20261201"`)
	assert.NotContains(t, codec, "20260101")
	assert.NotContains(t, read(t, root, "internal/spec/v20261201/DISCREPANCIES.md"), "template", "the template's discrepancies are not the new generation's")

	all := read(t, root, "internal/spec/all/all.go")
	assert.Contains(t, all, `v20261201 "`+mod+`internal/spec/v20261201/release"`)
	assert.Contains(t, all, "v20260101.New(),\n\t\tv20261201.New(),")

	// The template is untouched.
	assert.Equal(t, "package v20260101\n\nconst Release = \"2026-01-01\"\n", read(t, root, "internal/spec/v20260101/release.go"))
	assert.Contains(t, res.Created, "internal/spec/all/all.go")
	assert.NotEmpty(t, res.Next)
}

func TestSpecVersion_Refuses(t *testing.T) {
	for name, to := range map[string]types.Version{
		"not a date":         "2026-13-01",
		"already registered": "2026-01-01",
	} {
		t.Run(name, func(t *testing.T) {
			root := specRepo(t)
			_, err := scaffold.SpecVersion(root, "2026-01-01", to, []types.Version{"2026-01-01"})
			assert.Error(t, err)
			assert.Equal(t, specAll, read(t, root, "internal/spec/all/all.go"))
		})
	}
}

const platformGo = `package platform

import (
	"github.com/sarvexlabs/sarvex.cp/internal/platform/alpha"
	alphasim "github.com/sarvexlabs/sarvex.cp/internal/platform/alpha/sim"
	alphavalidator "github.com/sarvexlabs/sarvex.cp/internal/platform/alpha/validator"
)

func All() []Platform {
	return []Platform{
		{
			Name: alpha.PlatformName, Connector: alpha.Factory(), Validator: alphavalidator.Factory(),
			ConfigSchema: (&alpha.MerchantConfig{}).Schema(), Plugin: "plugin/alpha", Sim: alphasim.Surface{},
		},
	}
}

func Get(name Name) (Platform, bool) {
	for _, p := range All() {
		if p.Name == name {
			return p, true
		}
	}
	return Platform{}, false
}
`

func platformRepo(t *testing.T) string {
	return repo(t, map[string]string{
		"internal/platform/platform.go": platformGo,
		"internal/platform/alpha/factory.go": `package alpha

// PlatformName is AlphaShop's registry name.
const PlatformName = "alpha"

const errCode = "alpha_rest_error" // ALPHA
`,
		"internal/platform/alpha/alpha_test.go":     "package alpha_test\n",
		"internal/platform/alpha/sim/surface.go":    "package sim\n",
		"internal/platform/frozen/legacy/legacy.go": "package legacy\n",
	})
}

func TestPlatform_CopiesRenamesRegistersAndAddsPlugin(t *testing.T) {
	root := platformRepo(t)
	known, err := scaffold.PlatformModules(root)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"alpha", "legacy"}, known)

	res, err := scaffold.Platform(root, "alpha", "beta", "BetaCart", known)
	require.NoError(t, err)

	factory := read(t, root, "internal/platform/beta/factory.go")
	assert.Contains(t, factory, "package beta")
	assert.Contains(t, factory, `const PlatformName = "beta"`)
	assert.Contains(t, factory, "BetaCartShop's registry name", "mixed case is the display name")
	assert.Contains(t, factory, "// BETA", "upper case stays upper case")
	assert.Contains(t, factory, `"beta_rest_error"`)
	assert.Equal(t, "package beta_test\n", read(t, root, "internal/platform/beta/beta_test.go"), "file names are rewritten too")
	assert.Equal(t, "package sim\n", read(t, root, "internal/platform/beta/sim/surface.go"))
	assert.Contains(t, read(t, root, "plugin/beta/README.md"), "BetaCart")

	reg := read(t, root, "internal/platform/platform.go")
	assert.Contains(t, reg, `betasim "`+mod+`internal/platform/beta/sim"`)
	assert.Contains(t, reg, `betavalidator "`+mod+`internal/platform/beta/validator"`)
	assert.Contains(t, reg, `Plugin: "plugin/beta", Sim: betasim.Surface{}`)
	assert.Less(t, strings.Index(reg, "alpha.PlatformName"), strings.Index(reg, "beta.PlatformName"), "registered after the existing platforms")
	assert.Less(t, strings.Index(reg, "beta.PlatformName"), strings.Index(reg, "func Get"), "registered inside All")

	assert.Contains(t, res.Created, "plugin/beta/README.md")
	assert.Contains(t, strings.Join(res.Next, "\n"), "store_credentials_beta")
}

func TestPlatform_DisplayNameDefaultsToCapitalised(t *testing.T) {
	root := platformRepo(t)
	_, err := scaffold.Platform(root, "alpha", "beta", "", []string{"alpha"})
	require.NoError(t, err)
	assert.Contains(t, read(t, root, "internal/platform/beta/factory.go"), "BetaShop's registry name")
}

func TestPlatform_Refuses(t *testing.T) {
	for name, to := range map[string]string{
		"not lowercase letters":   "Beta2",
		"contains an existing":    "alphax",
		"inside an existing":      "alph",
		"a frozen platform's":     "legacy",
		"the template's own name": "alpha",
	} {
		t.Run(name, func(t *testing.T) {
			root := platformRepo(t)
			known, err := scaffold.PlatformModules(root)
			require.NoError(t, err)
			_, err = scaffold.Platform(root, "alpha", to, "", known)
			assert.Error(t, err)
			assert.Equal(t, platformGo, read(t, root, "internal/platform/platform.go"))
			assert.NoDirExists(t, filepath.Join(root, "plugin", to))
		})
	}
}
