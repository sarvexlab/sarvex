// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package activation_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/planes/control/activation"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// This file is R2, mechanically enforced: the activation state machine must
// have no dependency on surface state or on any specific surface's
// configuration.
//
// Two assertions, because there are two ways the dependency could creep
// back. The first is structural — an import. The second is behavioural — a
// state, signal, or transition that names a surface even without importing
// one. Prose in a doc comment stops neither, so neither is left to prose.

// forbiddenImports are packages the activation lifecycle must never reach
// for. surface is the obvious one; the rest are surface-specific
// machinery that must not find its way into onboarding.
var forbiddenImports = []string{
	"planes/control/surface",
	"planes/sync/feed",
	"planes/control/checklist",
}

// TestActivation_ImportsNoSurfacePackage asserts the activation package
// does not import the surface registry, the feed, or the Google checklist —
// directly or through any file in the package.
//
// An import is the cheapest possible way for this coupling to return, and
// the hardest to spot in review once the package is large.
func TestActivation_ImportsNoSurfacePackage(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, packageDir(t), func(fi os.FileInfo) bool {
		// Test files may legitimately reference surfaces — this one does.
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ImportsOnly)
	require.NoError(t, err)
	require.NotEmpty(t, pkgs, "expected to parse the activation package")

	for _, pkg := range pkgs {
		for filename, file := range pkg.Files {
			for _, imp := range file.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				for _, forbidden := range forbiddenImports {
					assert.NotContainsf(t, path, forbidden,
						"%s imports %s — activation must not depend on surface state or any surface's configuration (R2)",
						filepath.Base(filename), path)
				}
			}
		}
	}
}

// TestActivation_NoStateOrSignalNamesASurface asserts nothing in the
// lifecycle's own vocabulary refers to a surface, a distribution partner,
// or any surface-specific concepts (feeds, checklists, sandboxes).
func TestActivation_NoStateOrSignalNamesASurface(t *testing.T) {
	forbiddenTerms := []string{
		"google", "gmc", "merchant_center", "merchantcenter",
		"feed", "surface", "sandbox", "checklist", "waitlist", "acp",
	}

	var vocabulary []string
	for _, s := range activation.AllStates() {
		vocabulary = append(vocabulary, string(s))
	}
	for _, s := range activation.AllSignals() {
		vocabulary = append(vocabulary, string(s))
	}

	for _, word := range vocabulary {
		lower := strings.ToLower(word)
		for _, term := range forbiddenTerms {
			assert.NotContainsf(t, lower, term,
				"activation vocabulary %q references %q — no state or signal in this lifecycle may name a surface (R2)",
				word, term)
		}
	}
}

// TestActivation_ReachesActiveWithNoSurfaceInvolvement is the same rule
// stated as behaviour rather than as structure: a store goes all the way to
// active with no surface service in existence, no surface enrolled, and no
// Google anything. If activation ever grows a surface dependency, this is
// the test that stops compiling or starts failing.
func TestActivation_ReachesActiveWithNoSurfaceInvolvement(t *testing.T) {
	// Constructed with no surface reader, no feed reader, no checklist —
	// there is nowhere for surface state to enter from.
	svc := activation.NewActivationService(newMemRepo(), stubStores{}, nil, nil, nil)
	ctx := t.Context()

	require.NoError(t, svc.Initialize(ctx, testMerchant, testStore))
	for _, sig := range toActive() {
		require.NoError(t, svc.HandleSignal(ctx, testMerchant, testStore, sig))
	}

	a, err := svc.Get(ctx, testMerchant, testStore)
	require.NoError(t, err)
	assert.Equal(t, domain.UCPStateActive, a.UCPState)
	assert.True(t, activation.IsTerminalSuccess(a.UCPState),
		"a store with zero surfaces is fully active, not partially onboarded (R3)")
}

// packageDir returns the activation package's source directory. The test
// binary's working directory is the package directory under `go test`, so
// this is just "." — named for what it is, and resolved absolutely so a
// failure message points somewhere useful.
func packageDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	require.NoError(t, err)
	return dir
}

var _ = ast.Print
