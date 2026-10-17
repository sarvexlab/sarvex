// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// verifyRoutes checks that all control-plane API routes are documented in the OpenAPI spec.
// This CLI tool runs in CI to catch drift between the code-registered routes and the
// OpenAPI contract (sarvex-v1-control-plane.yaml).
func main() {
	openapiPath := flag.String("openapi", "sarvex-v1-control-plane.yaml", "path to the OpenAPI spec file")
	// The contract also covers the connector-ingest routes (/connector/*),
	// which the sync plane registers, so both route files are scanned.
	routeFilePaths := flag.String("routes",
		"internal/planes/control/api/handler.go,internal/planes/sync/api/handler.go",
		"comma-separated handler files with route registrations")
	flag.Parse()

	// Read OpenAPI spec
	openapiPaths, err := readOpenAPIPaths(*openapiPath)
	if err != nil {
		log.Fatalf("Failed to read OpenAPI spec: %v", err)
	}

	// Extract routes from Go code
	codeRoutes := map[string]bool{}
	for _, f := range strings.Split(*routeFilePaths, ",") {
		routes, err := extractRoutesFromCode(strings.TrimSpace(f))
		if err != nil {
			log.Fatalf("Failed to extract routes from %s: %v", f, err)
		}
		for r := range routes {
			codeRoutes[r] = true
		}
	}

	// Verify routes match
	drift := verifyRoutes(codeRoutes, openapiPaths)
	if len(drift) > 0 {
		fmt.Fprintf(os.Stderr, "Route drift detected: %d mismatches\n\n", len(drift))
		for _, d := range drift {
			fmt.Fprintf(os.Stderr, "  %s\n", d)
		}
		os.Exit(1)
	}

	fmt.Printf("✓ All %d routes verified against OpenAPI spec\n", len(codeRoutes))
}

// readOpenAPIPaths reads the OpenAPI YAML and extracts all path patterns.
func readOpenAPIPaths(filePath string) (map[string]bool, error) {
	data, err := os.ReadFile(filePath) //nolint:gosec // path from CLI arg, not user input
	if err != nil {
		return nil, err
	}

	var spec map[string]any
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, err
	}

	paths := make(map[string]bool)
	if pathsObj, ok := spec["paths"].(map[string]any); ok {
		for path := range pathsObj {
			paths[path] = true
		}
	}

	return paths, nil
}

// extractRoutesFromCode extracts route patterns from the Go handler code.
// Looks for patterns like: g.GET("/path", handler)
func extractRoutesFromCode(filePath string) (map[string]bool, error) {
	data, err := os.ReadFile(filePath) //nolint:gosec // path from CLI arg, not user input
	if err != nil {
		return nil, err
	}

	routes := make(map[string]bool)

	// Match patterns like: g.GET("/path"...), g.POST("/path"...) etc.
	// This regex finds routes registered via the Echo group methods.
	routePattern := regexp.MustCompile(`g\.(GET|POST|PUT|PATCH|DELETE)\("([^"]+)"`)

	matches := routePattern.FindAllStringSubmatch(string(data), -1)
	for _, match := range matches {
		if len(match) >= 3 {
			path := match[2]
			// Normalize Echo path params from :id to {id} for comparison with OpenAPI
			path = normalizeEchoPath(path)
			routes[path] = true
		}
	}

	return routes, nil
}

// normalizeEchoPath converts Echo path parameters (:id) to OpenAPI format ({id}).
func normalizeEchoPath(path string) string {
	// Convert :param to {param}
	paramPattern := regexp.MustCompile(`:([a-zA-Z_][a-zA-Z0-9_]*)`)
	return paramPattern.ReplaceAllString(path, "{$1}")
}

var openAPIParam = regexp.MustCompile(`\{[^}/]+\}`)

// shape reduces a path to its identity: parameter names are labels, not part
// of the route (`/x/{checkout_id}` and `/x/{checkout_session_id}` are the same
// route), so they are compared as `{}`.
func shape(path string) string { return openAPIParam.ReplaceAllString(path, "{}") }

func shapes(paths map[string]bool) map[string]bool {
	out := make(map[string]bool, len(paths))
	for p := range paths {
		out[shape(p)] = true
	}
	return out
}

// verifyRoutes checks that all code routes are documented in the OpenAPI spec.
// Returns a list of drift messages (routes not in OpenAPI or vice versa).
func verifyRoutes(codeRoutes, openapiPaths map[string]bool) []string {
	var drift []string
	codeShapes, specShapes := shapes(codeRoutes), shapes(openapiPaths)

	// Check code routes against OpenAPI
	for route := range codeRoutes {
		if !specShapes[shape(route)] {
			drift = append(drift, fmt.Sprintf("code-only: %s (not in OpenAPI spec)", route))
		}
	}

	// Check OpenAPI paths against code (only for /v1 control-plane paths)
	for path := range openapiPaths {
		if !codeShapes[shape(path)] {
			// Skip paths that aren't control-plane (e.g., components, webhooks)
			if isControlPlanePath(path) {
				drift = append(drift, fmt.Sprintf("spec-only: %s (not in code)", path))
			}
		}
	}

	// Sort for consistent output
	sort.Strings(drift)
	return drift
}

// isControlPlanePath checks if this is an actual control-plane API path
// (vs OpenAPI spec sections like components, webhooks, etc.).
func isControlPlanePath(path string) bool {
	// Real API paths start with /
	if len(path) == 0 || path[0] != '/' {
		return false
	}
	// Skip meta-paths
	if path == "/" {
		return false
	}
	return true
}
