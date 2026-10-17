// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Command scaffold adds a UCP spec generation or a backend platform as a new
// module copied from the registered one most like it, plus its registration
// line (internal/scaffold). Run it from the repository through make:
//
//	make new-spec-version V=YYYY-MM-DD   # copies the latest generation
//	make new-platform P=name [D=Display] # copies the default platform
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/types"
	"github.com/sarvexlab/sarvex/internal/platform"
	"github.com/sarvexlab/sarvex/internal/scaffold"
	specall "github.com/sarvexlab/sarvex/internal/spec/all"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "scaffold:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: scaffold spec-version YYYY-MM-DD | scaffold platform [-display Name] name")
	}
	root, err := repoRoot()
	if err != nil {
		return err
	}
	var res scaffold.Result
	switch args[0] {
	case "spec-version":
		if len(args) != 2 {
			return fmt.Errorf("usage: scaffold spec-version YYYY-MM-DD")
		}
		reg := specall.Default()
		res, err = scaffold.SpecVersion(root, reg.Latest(), types.Version(args[1]), reg.All())
	case "platform":
		fs := flag.NewFlagSet("platform", flag.ContinueOnError)
		display := fs.String("display", "", "human-readable name (default: the name, capitalised)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: scaffold platform [-display Name] name")
		}
		known, kerr := scaffold.PlatformModules(root)
		if kerr != nil {
			return kerr
		}
		res, err = scaffold.Platform(root, string(platform.Default()), fs.Arg(0), *display, known)
	default:
		return fmt.Errorf("unknown scaffold %q", args[0])
	}
	if err != nil {
		return err
	}
	fmt.Printf("Created or changed %d files.\n\nStill to do:\n", len(res.Created))
	for i, step := range res.Next {
		fmt.Printf("  %d. %s\n", i+1, step)
	}
	return nil
}

// repoRoot walks up from the working directory to the directory holding go.mod.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod above the working directory")
		}
		dir = parent
	}
}
