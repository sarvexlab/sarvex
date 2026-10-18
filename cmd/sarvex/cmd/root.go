// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package cmd holds sarvex's command tree.
package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// Global configuration shared by every subcommand. Flags override
// environment variables, which override the local-dev defaults — the same
// values scripts/dev/up.sh exports, so a stack started by `make dev-up`
// needs no flags at all.
type globalOpts struct {
	controlURL string
	servingURL string
	output     string
	timeout    time.Duration
}

const (
	outputHuman = "human"
	outputJSON  = "json"
)

var global globalOpts

// Execute runs the command tree. Errors are returned rather than printed so
// main owns the exit code.
func Execute() error {
	root := &cobra.Command{
		Use:   "sarvex",
		Short: "Drive Sarvex: onboard merchants, and act as a UCP commerce agent against them",
		Long: "sarvex is a development and CI tool with two halves.\n\n" +
			"  onboard  drives a merchant from nothing to a live UCP store, deriving\n" +
			"           every detail from the merchant's name.\n" +
			"  agent    plays the UCP shopping-agent role against a live store, the\n" +
			"           way Google's checkout surface does in production.\n" +
			"  conform  runs the agent half as an assertion suite, including the\n" +
			"           documented failure paths, and reports pass/fail.\n\n" +
			"It talks to running services over HTTP only. Start them with `make dev-up`,\n" +
			"or let `conform run --in-process` start them itself.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringVar(&global.controlURL, "control-url",
		envOr("CONTROL_PLANE_BASE_URL", "http://localhost:8081/v1"),
		"Control plane base URL")
	root.PersistentFlags().StringVar(&global.servingURL, "serving-url",
		envOr("SERVING_BASE_URL", "http://localhost:8083/ucp"),
		"Serving plane base URL")
	root.PersistentFlags().StringVarP(&global.output, "output", "o", outputHuman,
		"Output format: human or json")
	root.PersistentFlags().DurationVar(&global.timeout, "timeout", 3*time.Minute,
		"Overall timeout for operations that wait on a lifecycle")

	root.AddCommand(newOnboardCmd(), newAgentCmd(), newConformCmd())
	return root.Execute()
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// emitJSON writes v as indented JSON.
func emitJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// progressWriter returns where step-by-step progress should go.
//
// In JSON mode it goes to stderr, keeping stdout a single parseable
// document — a caller piping this into jq must not have to strip
// human-readable lines out first.
func progressWriter() io.Writer {
	if global.output == outputJSON {
		return os.Stderr
	}
	return os.Stdout
}

// validateOutput rejects an unknown --output early, rather than silently
// falling back to human and confusing a script that asked for JSON.
func validateOutput() error {
	switch global.output {
	case outputHuman, outputJSON:
		return nil
	default:
		return fmt.Errorf("unknown --output %q: want %s or %s", global.output, outputHuman, outputJSON)
	}
}
