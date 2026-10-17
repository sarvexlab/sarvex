// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package archtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	specall "github.com/sarvexlab/sarvex/internal/spec/all"
)

// TestVersionAndPlatformBoundaries enforces that spec-generation-specific and
// platform-specific code stays inside its own packages
// (docs/plans/version-platform-modularity.md §4).
//
// Every non-test Go file under internal/ and cmd/ is given an owner: the spec
// generation it belongs to (internal/spec/vYYYYMMDD/...), the platform it belongs to
// (internal/platform/<platform>/..., frozen ones included), a registration
// root that is allowed to name every version and platform, or none (shared
// code). A file that reaches a version or platform it does not own — by
// import, by a string literal holding the version date or platform name, or by
// an identifier naming it — is a violation. So is a plane importing another
// plane (sarvex-v1-architecture.md A10.1).
//
// Any violation fails: there is no allowlist. (There was one while the tree
// was brought into line; it shrank to nothing and was removed.) Version- or
// platform-specific behaviour belongs in that version's internal/spec module
// or that platform's internal/platform module, reached through their
// registries.
func TestVersionAndPlatformBoundaries(t *testing.T) {
	root := filepath.Join("..", "..")
	versions := knownVersions()
	platforms := knownPlatforms(t, root)

	got := map[violation]int{}
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" || d.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			return scanFile(path, filepath.ToSlash(rel), versions, platforms, got)
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}

	if len(got) > 0 {
		problems := make([]string, 0, len(got))
		for v, n := range got {
			problems = append(problems, fmt.Sprintf("%s (%d)", v, n))
		}
		sort.Strings(problems)
		t.Errorf("version/platform boundary violated:\n  %s", strings.Join(problems, "\n  "))
	}
}

// violation is one kind of boundary crossing in one file.
type violation struct {
	rule   string // import, literal or ident
	file   string // repo-relative, slash-separated
	target string // the version (vYYYYMMDD) or platform reached
}

func (v violation) String() string { return v.rule + " " + v.file + " -> " + v.target }

// version is one known spec generation in the forms code spells it.
type version struct {
	pkg  string // v20260408
	date string // 2026-04-08
}

func knownVersions() []version {
	var out []version
	for _, v := range specall.Default().All() {
		out = append(out, version{pkg: "v" + strings.ReplaceAll(string(v), "-", ""), date: string(v)})
	}
	return out
}

// knownPlatforms lists every platform module under internal/platform, frozen
// ones included: they are platform-specific code all the same.
func knownPlatforms(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	for _, dir := range []string{platformRoot, platformRoot + "/frozen"} {
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
		if err != nil {
			t.Fatalf("list platforms: %v", err)
		}
		for _, e := range entries {
			if e.IsDir() && e.Name() != "frozen" {
				out = append(out, e.Name())
			}
		}
	}
	return out
}

const module = "github.com/sarvexlab/sarvex/"

var (
	versionPkg  = regexp.MustCompile(`^internal/spec/(v\d{8})(/|$)`)
	platformPkg = regexp.MustCompile(`^internal/platform/(?:frozen/)?([a-z]+)(/|$)`)
	planePkg    = regexp.MustCompile(`^internal/planes/([a-z]+)(/|$)`)
)

// platformRoot is the platform registry; each platform is a module beneath it.
const platformRoot = "internal/platform"

// registrationRoots may name every version and platform: they are where each
// one is registered, once.
var registrationRoots = map[string]bool{
	"internal/pkg/ucp/types": true,
	"internal/spec/all":      true,
	platformRoot:             true,
}

// owner is what a package may reach: its own version or platform, or
// everything for a registration root.
type owner struct {
	all      bool
	version  string
	platform string
}

func ownerOf(dir string) owner {
	if registrationRoots[dir] {
		return owner{all: true}
	}
	if m := versionPkg.FindStringSubmatch(dir); m != nil {
		return owner{version: m[1]}
	}
	if m := platformPkg.FindStringSubmatch(dir); m != nil && m[1] != "frozen" {
		return owner{platform: m[1]}
	}
	return owner{}
}

// targetOf classifies an import path as a version or platform package.
func targetOf(importPath string) (versionPkgName, platformName string) {
	if !strings.HasPrefix(importPath, module) {
		return "", ""
	}
	o := ownerOf(strings.TrimPrefix(importPath, module))
	return o.version, o.platform
}

// planeOf returns the plane a repo-relative path belongs to, or "".
func planeOf(rel string) string {
	if m := planePkg.FindStringSubmatch(rel); m != nil {
		return m[1]
	}
	return ""
}

func scanFile(path, rel string, versions []version, platforms []string, got map[violation]int) error {
	own := ownerOf(filepath.ToSlash(filepath.Dir(rel)))
	if own.all {
		return nil
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("parse %s: %w", rel, err)
	}

	importNames := map[string]bool{}
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		name := p[strings.LastIndex(p, "/")+1:]
		if imp.Name != nil {
			name = imp.Name.Name
		}
		importNames[name] = true
		if from, to := planeOf(rel), planeOf(strings.TrimPrefix(p, module)); from != "" && to != "" && from != to {
			// sarvex-v1-architecture.md A10.1: planes talk over the bus or read collections,
			// never by importing each other.
			got[violation{"plane-import", rel, to}] = 1
		}
		v, pl := targetOf(p)
		if v != "" && v != own.version {
			got[violation{"import", rel, v}] = 1
		}
		if pl != "" && pl != own.platform {
			got[violation{"import", rel, pl}] = 1
		}
	}

	// Package qualifiers (the X in pkg.Name) are covered by the import rule.
	qualifiers := map[*ast.Ident]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && importNames[id.Name] {
				qualifiers[id] = true
			}
		}
		return true
	})

	count := func(rule, text string) {
		lower := strings.ToLower(text)
		for _, v := range versions {
			if v.pkg == own.version {
				continue
			}
			if (rule == "literal" && strings.Contains(text, v.date)) ||
				(rule == "ident" && strings.Contains(lower, v.pkg[1:])) {
				got[violation{rule, rel, v.pkg}]++
			}
		}
		for _, p := range platforms {
			if p != own.platform && strings.Contains(lower, p) {
				got[violation{rule, rel, p}]++
			}
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.ImportSpec:
			return false
		case *ast.BasicLit:
			if x.Kind == token.STRING {
				count("literal", x.Value)
			}
		case *ast.Ident:
			if !qualifiers[x] {
				count("ident", x.Name)
			}
		}
		return true
	})
	return nil
}
