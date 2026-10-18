// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package conform

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"
	"time"
)

// This is the single most important behaviour in the package, and the whole
// reason it exists. Both suites cmd/sarvex replaces reported success while
// running zero assertions: their infrastructure guard fired, every spec
// skipped, and the runner printed a pass. A suite that cannot fail is not
// protecting anything.
func TestReportWithNoExecutedChecksIsNotAPass(t *testing.T) {
	r := Report{Results: []Result{
		{ID: "a", Outcome: Skip, Detail: "nothing to buy"},
		{ID: "b", Outcome: Skip, Detail: "nothing to buy"},
	}}
	err := r.Err()
	if err == nil {
		t.Fatal("a report of nothing but skips reported success")
	}
	if !strings.Contains(err.Error(), "no conformance checks executed") {
		t.Errorf("error does not explain the problem: %v", err)
	}
}

func TestEmptyReportIsNotAPass(t *testing.T) {
	if (Report{}).Err() == nil {
		t.Fatal("an empty report reported success")
	}
}

func TestReportErr(t *testing.T) {
	tests := []struct {
		name    string
		results []Result
		wantErr bool
	}{
		{
			name:    "all passed",
			results: []Result{{ID: "a", Outcome: Pass}, {ID: "b", Outcome: Pass}},
			wantErr: false,
		},
		{
			name:    "one failed",
			results: []Result{{ID: "a", Outcome: Pass}, {ID: "b", Outcome: Fail}},
			wantErr: true,
		},
		{
			// A skip alongside real assertions is fine — some checks
			// legitimately have no preconditions to run against.
			name:    "passes with a skip",
			results: []Result{{ID: "a", Outcome: Pass}, {ID: "b", Outcome: Skip}},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Report{Results: tt.results}.Err()
			if (err != nil) != tt.wantErr {
				t.Errorf("Err() = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCounts(t *testing.T) {
	r := Report{Results: []Result{
		{Outcome: Pass}, {Outcome: Pass}, {Outcome: Fail}, {Outcome: Skip},
	}}
	passed, failed, skipped := r.Counts()
	if passed != 2 || failed != 1 || skipped != 1 {
		t.Errorf("Counts() = (%d, %d, %d), want (2, 1, 1)", passed, failed, skipped)
	}
}

// Every check id must be unique: the JUnit report keys test cases on it, and
// CI silently drops or merges duplicates, hiding a check.
func TestCheckIDsAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range checks() {
		if seen[c.id] {
			t.Errorf("duplicate check id %q", c.id)
		}
		seen[c.id] = true
		if c.capability == "" {
			t.Errorf("check %q has no capability label", c.id)
		}
		if c.run == nil {
			t.Errorf("check %q has no run function", c.id)
		}
	}
}

// The suite must only assert against capabilities the serving plane
// actually implements. cmd/ucp-sim asserted against cart, identity, AP2 and
// MCP surfaces that have never existed, producing 85 scenarios that could
// not pass and whose failures said nothing about Sarvex.
func TestChecksOnlyCoverImplementedCapabilities(t *testing.T) {
	implemented := map[string]bool{
		"discovery":      true, // GET /.well-known/ucp
		"catalog.search": true, // POST /catalog/search
		"catalog.lookup": true, // GET /catalog/{itemId}
		"checkout":       true, // POST/GET/PUT /checkout-sessions[/{id}][/complete]
	}
	for _, c := range checks() {
		if !implemented[c.capability] {
			t.Errorf("check %q targets capability %q, which the serving plane does not implement "+
				"(see internal/planes/serving/api/handler.go SetupRoutes)", c.id, c.capability)
		}
	}
}

// Fault-dependent checks must be marked, or a run without a fault injector
// reports them as passing without having exercised anything.
func TestFaultChecksAreMarked(t *testing.T) {
	faultIDs := map[string]bool{
		"checkout.out_of_stock_is_refused":  true,
		"catalog.broken_attribute_surfaces": true,
	}
	for _, c := range checks() {
		if faultIDs[c.id] && !c.needsFault {
			t.Errorf("check %q injects a fault but is not marked needsFault", c.id)
		}
		if !faultIDs[c.id] && c.needsFault {
			t.Errorf("check %q is marked needsFault but is not in the known fault set", c.id)
		}
	}
}

func TestWriteJUnitMarksFailuresAndSkips(t *testing.T) {
	r := &Report{
		StoreID: "store-1",
		Results: []Result{
			{ID: "a", Capability: "checkout", Outcome: Pass, Duration: time.Millisecond},
			{ID: "b", Capability: "checkout", Outcome: Fail, Detail: "total was 0", Duration: time.Millisecond},
			{ID: "c", Capability: "catalog.lookup", Outcome: Skip, Detail: "no item", Duration: time.Millisecond},
		},
		Duration: 3 * time.Millisecond,
	}

	var buf bytes.Buffer
	if err := WriteJUnit(&buf, r); err != nil {
		t.Fatalf("WriteJUnit: %v", err)
	}

	var out junitSuites
	if err := xml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("emitted XML does not parse: %v\n%s", err, buf.String())
	}
	if len(out.Suites) != 1 {
		t.Fatalf("got %d suites, want 1", len(out.Suites))
	}
	suite := out.Suites[0]
	if suite.Tests != 3 || suite.Failures != 1 || suite.Skipped != 1 {
		t.Errorf("suite counts = tests %d failures %d skipped %d, want 3/1/1",
			suite.Tests, suite.Failures, suite.Skipped)
	}
	if suite.Cases[1].Failure == nil {
		t.Error("the failing check has no <failure> element, so CI cannot annotate it")
	} else if !strings.Contains(suite.Cases[1].Failure.Message, "total was 0") {
		t.Errorf("failure message lost the detail: %q", suite.Cases[1].Failure.Message)
	}
	if suite.Cases[2].Skipped == nil {
		t.Error("the skipped check has no <skipped> element")
	}
}

func TestWriteHumanIncludesSeedForReproduction(t *testing.T) {
	var buf bytes.Buffer
	r := &Report{Seed: 4815162342, Results: []Result{{ID: "a", Outcome: Pass}}}
	if err := WriteHuman(&buf, r); err != nil {
		t.Fatalf("WriteHuman: %v", err)
	}
	if !strings.Contains(buf.String(), "4815162342") {
		t.Errorf("human output omits the seed, so a failing run cannot be rebuilt:\n%s", buf.String())
	}
}

func TestWriteJSONIsValid(t *testing.T) {
	var buf bytes.Buffer
	r := &Report{StoreID: "store-1", Results: []Result{{ID: "a", Outcome: Pass}}}
	if err := WriteJSON(&buf, r); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if !strings.Contains(buf.String(), `"store_id": "store-1"`) {
		t.Errorf("JSON output missing store_id:\n%s", buf.String())
	}
}

func TestRunRejectsMissingStoreID(t *testing.T) {
	if _, err := Run(t.Context(), nil, Options{}); err == nil {
		t.Fatal("Run with no store id succeeded")
	}
}
