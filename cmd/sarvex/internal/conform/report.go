// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package conform

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"text/tabwriter"
)

// WriteHuman renders a report for a person reading a terminal.
func WriteHuman(w io.Writer, r *Report) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "\nCHECK\tCAPABILITY\tOUTCOME\tDETAIL\t")
	for _, res := range r.Results {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t\n", res.ID, res.Capability, marker(res.Outcome), res.Detail)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	passed, failed, skipped := r.Counts()
	_, _ = fmt.Fprintf(w, "\n%d passed, %d failed, %d skipped in %s\n",
		passed, failed, skipped, r.Duration.Round(1e6))
	if r.Seed != 0 {
		_, _ = fmt.Fprintf(w, "fixture seed %d — rebuild this exact run with --seed %d\n", r.Seed, r.Seed)
	}
	return nil
}

func marker(o Outcome) string {
	switch o {
	case Pass:
		return "PASS"
	case Fail:
		return "FAIL"
	default:
		return "SKIP"
	}
}

// WriteJSON renders a report as a single JSON document.
func WriteJSON(w io.Writer, r *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// ── JUnit ────────────────────────────────────────────────────────────────
//
// Emitted so CI can annotate a failing run against the specific check that
// failed, instead of pointing at the whole job.

type junitSuites struct {
	XMLName xml.Name     `xml:"testsuites"`
	Suites  []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Skipped  int         `xml:"skipped,attr"`
	Time     float64     `xml:"time,attr"`
	Cases    []junitCase `xml:"testcase"`
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	ClassName string        `xml:"classname,attr"`
	Time      float64       `xml:"time,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	Skipped   *junitSkipped `xml:"skipped,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

type junitSkipped struct {
	Message string `xml:"message,attr"`
}

// WriteJUnit renders a report as a JUnit XML document.
func WriteJUnit(w io.Writer, r *Report) error {
	passed, failed, skipped := r.Counts()
	suite := junitSuite{
		Name:     "sarvex.conformance",
		Tests:    passed + failed + skipped,
		Failures: failed,
		Skipped:  skipped,
		Time:     r.Duration.Seconds(),
	}

	for _, res := range r.Results {
		tc := junitCase{
			Name:      res.ID,
			ClassName: res.Capability,
			Time:      res.Duration.Seconds(),
		}
		switch res.Outcome {
		case Fail:
			tc.Failure = &junitFailure{Message: res.Detail, Body: res.Detail}
		case Skip:
			tc.Skipped = &junitSkipped{Message: res.Detail}
		}
		suite.Cases = append(suite.Cases, tc)
	}

	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(junitSuites{Suites: []junitSuite{suite}}); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}
