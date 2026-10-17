// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/surfaces"
)

// TestSurfaceBoundaries enforces that an agent surface's behaviour stays in
// its own modules, the same way a spec version or a platform does.
//
// A surface is a control-plane half (internal/planes/control/surface/<name>)
// and a sync-plane half (internal/planes/sync/surface/<name>). A file outside
// them that names the surface's id as a string literal, or imports its
// module, is a violation — except the shared id package (surfaces.All is where
// ids live) and the two registration roots (…/surface/all), which exist to
// register every surface once. Generic surface code, the API layer, activation
// and the catalogue reach a surface by registry or by the shared constants,
// never by spelling it.
//
// There is no allowlist.
func TestSurfaceBoundaries(t *testing.T) {
	root := filepath.Join("..", "..")
	ids := map[string]string{} // id -> module dir name (id without underscores)
	for _, id := range surfaces.All {
		ids[id] = strings.ReplaceAll(id, "_", "")
	}

	moduleDir := regexp.MustCompile(`^internal/planes/(?:control|sync)/surface/([a-z0-9]+)(/|$)`)
	const sharedIDs = "internal/pkg/ucp/surfaces"

	got := map[string]int{}
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" || d.Name() == "archive" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		dir := filepath.ToSlash(filepath.Dir(rel))
		if dir == sharedIDs {
			return nil
		}
		ownerModule := ""
		if m := moduleDir.FindStringSubmatch(dir); m != nil {
			ownerModule = m[1]
		}
		if ownerModule == "all" {
			return nil // a registration root
		}

		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if m := moduleDir.FindStringSubmatch(strings.TrimPrefix(p, module)); m != nil && m[1] != "all" && m[1] != ownerModule {
				got["import "+rel+" -> "+m[1]]++
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, _ := strconv.Unquote(lit.Value)
			for id, mod := range ids {
				if s == id && mod != ownerModule {
					got["literal "+rel+" -> "+id]++
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > 0 {
		var problems []string
		for k := range got {
			problems = append(problems, k)
		}
		sort.Strings(problems)
		t.Errorf("surface boundary violated (use surfaces.<Id> or a registry; a surface's code lives in its own module):\n  %s",
			strings.Join(problems, "\n  "))
	}
}
