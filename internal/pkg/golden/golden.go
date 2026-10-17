// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package golden compares JSON a test produced against a frozen file under the
// calling package's testdata/golden directory.
//
// Goldens are the regression guard for the version and platform axes: every
// (spec generation, platform) pair's wire output is frozen, so adding a
// generation or a platform cannot silently change an existing pair's output.
// Changing an existing golden is a deliberate act: regenerate with
//
//	UPDATE_GOLDENS=1 go test ./...
//
// and label the PR spec-change/<version> or platform-change/<platform> — the
// golden-guard workflow (.github/workflows/golden-guard.yml) refuses an
// unlabelled change.
package golden

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// UpdateEnv is the environment variable that rewrites goldens instead of
// comparing against them.
const UpdateEnv = "UPDATE_GOLDENS"

// Timestamp replaces every RFC 3339 timestamp string.
const Timestamp = "<timestamp>"

var rfc3339 = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$`)

// Assert marshals got to indented JSON, normalises it, and compares it with
// testdata/golden/<rel>.json. replace maps run-specific substrings (a test
// server's address, a generated store id) to stable placeholders; it is applied
// to the marshalled JSON before timestamps are normalised to Timestamp.
func Assert(t testing.TB, rel string, got any, replace map[string]string) {
	t.Helper()
	b, err := Normalise(got, replace)
	if err != nil {
		t.Fatalf("golden %s: %v", rel, err)
	}

	path := filepath.Join("testdata", "golden", filepath.FromSlash(rel)+".json")
	if os.Getenv(UpdateEnv) != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("golden %s: %v", path, err)
		}
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Fatalf("golden %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path) //nolint:gosec // G304: a file under the calling package's testdata
	if err != nil {
		t.Fatalf("golden %s: %v (run with %s=1 to create it)", path, err, UpdateEnv)
	}
	if !bytes.Equal(b, want) {
		t.Errorf("golden %s changed. If intended, rerun with %s=1 and label the PR "+
			"spec-change/<version> or platform-change/<platform>.\n--- want\n%s\n--- got\n%s",
			path, UpdateEnv, want, b)
	}
}

// Normalise renders v as the canonical golden bytes: indented JSON with sorted
// object keys, replace applied, timestamps masked, and a trailing newline.
func Normalise(v any, replace map[string]string) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	for from, to := range replace {
		if from != "" {
			raw = bytes.ReplaceAll(raw, []byte(from), []byte(to))
		}
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, err
	}
	out, err := json.MarshalIndent(maskTimestamps(generic), "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func maskTimestamps(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			x[k] = maskTimestamps(e)
		}
	case []any:
		for i, e := range x {
			x[i] = maskTimestamps(e)
		}
	case string:
		if rfc3339.MatchString(x) {
			return Timestamp
		}
	}
	return v
}
