// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package scaffold creates a new spec generation or backend platform module
// from an existing one and registers it — the mechanical half of
// docs/HOWTO-add-spec-version.md and docs/HOWTO-add-platform.md.
//
// A new module is a copy of a registered one with every spelling of the
// template's name rewritten, plus its one registration line. The copy builds,
// passes the boundary check and serves exactly what its template serves; the
// person adding it then changes what actually differs. Nothing outside the new
// module and the registration root is touched, so no established version or
// platform changes (sarvex-v1-architecture.md).
//
// The template is passed in, never named here: cmd/scaffold takes it from the
// registries (the latest generation, the default platform).
package scaffold

import (
	"errors"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/types"
)

const module = "github.com/sarvexlab/sarvex/"

// Repository paths, relative to the root.
const (
	specDir      = "internal/spec"
	specRegistry = "internal/spec/all/all.go"
	platformDir  = "internal/platform"
	platformReg  = "internal/platform/platform.go"
	pluginDir    = "plugin"
)

// Result is what a scaffold created and what is left for a person to do.
type Result struct {
	Created []string // repo-relative paths written
	Next    []string // remaining manual steps
}

// SpecVersion copies generation from into a new module for generation to and
// registers it in internal/spec/all. known is every registered generation.
func SpecVersion(root string, from, to types.Version, known []types.Version) (Result, error) {
	if _, err := time.Parse(time.DateOnly, string(to)); err != nil {
		return Result{}, fmt.Errorf("version %q is not a YYYY-MM-DD date", to)
	}
	for _, k := range known {
		if k == to {
			return Result{}, fmt.Errorf("version %s is already registered", to)
		}
	}
	fromPkg, toPkg := pkgName(from), pkgName(to)
	src := filepath.Join(root, specDir, fromPkg)
	dst := filepath.Join(root, specDir, toPkg)

	rw := strings.NewReplacer(string(from), string(to), fromPkg[1:], toPkg[1:])
	created, err := copyTree(root, src, dst, rw.Replace)
	if err != nil {
		return Result{}, err
	}
	// The template's discrepancies are the template's; the new generation
	// starts with none recorded.
	discrepancies := filepath.Join(dst, "DISCREPANCIES.md")
	if err := writeFile(discrepancies, []byte(discrepanciesStub(to))); err != nil {
		return Result{}, err
	}
	if err := register(root, specRegistry,
		lastLine(`^\t\S+ "`+regexp.QuoteMeta(module+specDir)+`/v\d{8}/release"$`),
		fmt.Sprintf("\t%s %q", toPkg, module+specDir+"/"+toPkg+"/release"),
		lastLine(`^\t\tv\d{8}\.New\(\),$`),
		fmt.Sprintf("\t\t%s.New(),", toPkg),
	); err != nil {
		return Result{}, err
	}
	return Result{
		Created: append(created, specRegistry),
		Next: []string{
			fmt.Sprintf("Change %s/%s to what %s actually says: the schema types, wire/, codec/, profile/, extensions/ and release/ Serves(); record every gap in DISCREPANCIES.md.", specDir, toPkg, to),
			"Create the new generation's goldens: UPDATE_GOLDENS=1 go test ./internal/planes/... and review every new file under testdata/golden (no existing golden may change).",
			"Run make test-unit and make test-scenarios.",
			fmt.Sprintf("Label the PR spec-change/%s.", toPkg),
		},
	}, nil
}

var platformName = regexp.MustCompile(`^[a-z]+$`)

// Platform copies platform from into a new module for platform to (display is
// its human-readable name), registers it in internal/platform and creates its
// plugin directory. known is every platform module, frozen ones included.
func Platform(root, from, to, display string, known []string) (Result, error) {
	if !platformName.MatchString(to) {
		return Result{}, fmt.Errorf("platform %q must be lowercase letters only", to)
	}
	for _, k := range known {
		// The boundary check finds a platform by its name appearing in an
		// identifier or literal, so no name may contain another.
		if strings.Contains(k, to) || strings.Contains(to, k) {
			return Result{}, fmt.Errorf("platform %q clashes with existing platform %q", to, k)
		}
	}
	if display == "" {
		display = string(unicode.ToUpper(rune(to[0]))) + to[1:]
	}
	src := filepath.Join(root, platformDir, from)
	dst := filepath.Join(root, platformDir, to)

	spelled := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(from))
	rw := func(s string) string {
		return spelled.ReplaceAllStringFunc(s, func(m string) string {
			switch m {
			case strings.ToLower(m):
				return to
			case strings.ToUpper(m):
				return strings.ToUpper(to)
			default:
				return display
			}
		})
	}
	created, err := copyTree(root, src, dst, rw)
	if err != nil {
		return Result{}, err
	}

	plugin := filepath.Join(root, pluginDir, to)
	if err := mkdirAll(plugin); err != nil {
		return Result{}, err
	}
	readme := filepath.Join(plugin, "README.md")
	if err := writeFile(readme, []byte(pluginStub(to, display))); err != nil {
		return Result{}, err
	}
	created = append(created, pluginDir+"/"+to+"/README.md")

	pkg := module + platformDir + "/" + to
	if err := register(root, platformReg,
		lastLine(`^\t(\S+ )?"`+regexp.QuoteMeta(module+platformDir)+`/[a-z]+(/sim|/validator)?"$`),
		fmt.Sprintf("\t%q\n\t%ssim %q\n\t%svalidator %q", pkg, to, pkg+"/sim", to, pkg+"/validator"),
		lastLine(`^\t\t},$`),
		fmt.Sprintf("\t\t{\n\t\t\tName: %[1]s.PlatformName, Connector: %[1]s.Factory(), Validator: %[1]svalidator.Factory(),\n"+
			"\t\t\tConfigSchema: (&%[1]s.MerchantConfig{}).Schema(), Plugin: %[2]q, Sim: %[1]ssim.Surface{},\n\t\t},",
			to, pluginDir+"/"+to),
	); err != nil {
		return Result{}, err
	}
	return Result{
		Created: append(created, platformReg),
		Next: []string{
			fmt.Sprintf("Add %s to the platform enum in sarvex-v1-control-plane.yaml and a store_credentials_%s schema beside the others (internal/platform's tests fail until you do), then cd web/console && npm run gen:api and give it a label in src/platforms.ts and src/locales/en.ts (typecheck fails until you do).", to, to),
			fmt.Sprintf("Change %s/%s to talk to %s: client, mapper, types, config schema, validator and sim/ (the copy still speaks the template's store API).", platformDir, to, display),
			fmt.Sprintf("Build the merchant-side plugin in %s/%s (signalling + /.well-known/ucp doorway only, sarvex-v1-architecture.md).", pluginDir, to),
			"Create the new platform's goldens: UPDATE_GOLDENS=1 go test ./internal/planes/... and review every new file under testdata/golden (no existing golden may change).",
			"Run make test-unit and make test-scenarios.",
			fmt.Sprintf("Label the PR platform-change/%s.", to),
		},
	}, nil
}

// pkgName is a generation's module name: 2026-08-25 → v20260825.
func pkgName(v types.Version) string { return "v" + strings.ReplaceAll(string(v), "-", "") }

// copyTree copies src to dst, passing every file's contents and relative path
// through rewrite, and gofmt-ing Go files (a rewritten name can change
// alignment). dst must not exist. It returns the repo-relative paths written.
func copyTree(root, src, dst string, rewrite func(string) string) ([]string, error) {
	if _, err := os.Stat(src); err != nil {
		return nil, fmt.Errorf("template %s: %w", src, err)
	}
	if _, err := os.Stat(dst); err == nil {
		return nil, fmt.Errorf("%s already exists", dst)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	var created []string
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rewrite(rel))
		if d.IsDir() {
			return mkdirAll(out)
		}
		raw, err := readFile(path)
		if err != nil {
			return err
		}
		body := []byte(rewrite(string(raw)))
		if strings.HasSuffix(out, ".go") {
			if body, err = format.Source(body); err != nil {
				return fmt.Errorf("format %s: %w", out, err)
			}
		}
		if err := writeFile(out, body); err != nil {
			return err
		}
		relOut, err := filepath.Rel(root, out)
		if err != nil {
			return err
		}
		created = append(created, filepath.ToSlash(relOut))
		return nil
	})
	return created, err
}

// lastLine finds the index of the last line matching pattern.
func lastLine(pattern string) func([]string) int {
	re := regexp.MustCompile(pattern)
	return func(lines []string) int {
		for i := len(lines) - 1; i >= 0; i-- {
			if re.MatchString(lines[i]) {
				return i
			}
		}
		return -1
	}
}

// register inserts importLine after the line importAt finds and entry after
// the line entryAt finds in the registration root file, then gofmts it.
func register(root, file string, importAt func([]string) int, importLine string, entryAt func([]string) int, entry string) error {
	path := filepath.Join(root, file)
	raw, err := readFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(raw), "\n")
	i := importAt(lines)
	if i < 0 {
		return fmt.Errorf("%s: no registration import to add after", file)
	}
	lines = insertAfter(lines, i, importLine)
	j := entryAt(lines)
	if j < 0 || j < i {
		return fmt.Errorf("%s: no registration entry to add after", file)
	}
	lines = insertAfter(lines, j, entry)
	out, err := format.Source([]byte(strings.Join(lines, "\n")))
	if err != nil {
		return fmt.Errorf("format %s: %w", file, err)
	}
	return writeFile(path, out)
}

func insertAfter(lines []string, i int, s string) []string {
	return append(lines[:i+1], append([]string{s}, lines[i+1:]...)...)
}

func discrepanciesStub(v types.Version) string {
	return fmt.Sprintf(`# UCP %s — discrepancies

Where this generation's published schema and its prose disagree, or where
Sarvex deliberately departs from it, record it here: what the spec says, what
Sarvex does, and why.

None recorded yet.
`, v)
}

func pluginStub(name, display string) string {
	return fmt.Sprintf(`# %s plugin

The merchant-side plugin for %s stores (registry name `+"`%s`"+`). It has the
same two roles as every Sarvex plugin, and nothing else (sarvex-v1-architecture.md):

1. **Signalling** — on a product, order or settings change, POST a thin,
   HMAC-signed signal (type, id, sequence) to `+"`/connector/signals`"+`.
2. **Profile doorway** — serve `+"`/.well-known/ucp`"+` from the merchant's domain
   with the bytes from `+"`GET /connector/profile`"+`, keeping a last-known-good
   copy.

Not yet built: see docs/HOWTO-add-platform.md.
`, display, display, name)
}

// PlatformModules lists every platform module directory, frozen ones included.
func PlatformModules(root string) ([]string, error) {
	var out []string
	for _, dir := range []string{platformDir, platformDir + "/frozen"} {
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() && e.Name() != "frozen" {
				out = append(out, e.Name())
			}
		}
	}
	return out, nil
}

// Scaffolded files are source the repository commits, so they get the tree's
// usual permissions, and every path is inside the repository being scaffolded.

func writeFile(path string, body []byte) error {
	return os.WriteFile(path, body, 0o644) //nolint:gosec // G306: committed source, world-readable like the rest of the tree
}

func mkdirAll(path string) error {
	return os.MkdirAll(path, 0o755) //nolint:gosec // G301: committed source directory
}

func readFile(path string) ([]byte, error) {
	return os.ReadFile(path) //nolint:gosec // G304: a path inside the repository being scaffolded
}
