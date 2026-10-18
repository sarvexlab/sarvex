// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package all_test

import (
	"testing"

	"github.com/sarvexlab/sarvex/internal/spec/all"
)

// Every registered generation serves something, reads and writes its profile
// document, and has a codec — one registration supplies all of them.
func TestEveryGenerationIsComplete(t *testing.T) {
	reg := all.New()
	if len(reg.All()) == 0 {
		t.Fatal("no generation registered")
	}
	for _, id := range reg.All() {
		v, _ := reg.Get(id)
		if len(v.Serves()) == 0 {
			t.Errorf("%s serves nothing", id)
		}
		if !reg.Codecs().Supported(id) {
			t.Errorf("%s has no codec", id)
		}
		if _, ok := reg.Generations().Get(id); !ok {
			t.Errorf("%s has no profile generation", id)
		}
	}
	if reg.Baseline() != reg.All()[0] || reg.Latest() != reg.All()[len(reg.All())-1] {
		t.Errorf("Baseline/Latest are not the oldest/newest of %v", reg.All())
	}
}

// What a generation serves decides where it is negotiated and advertised:
// a capability is served in exactly the generations that list it.
func TestServing(t *testing.T) {
	reg := all.New()
	for _, id := range reg.All() {
		v, _ := reg.Get(id)
		for _, c := range v.Serves() {
			found := false
			for _, s := range reg.Serving(c) {
				found = found || s == id
			}
			if !found {
				t.Errorf("%s serves %s but Serving(%s) omits it", id, c, c)
			}
		}
	}
	if got := reg.Serving("dev.ucp.example.nothing"); len(got) != 0 {
		t.Errorf("Serving(unserved) = %v", got)
	}
	if _, err := reg.Parse("1999-01-01"); err == nil {
		t.Error("Parse accepted an unknown version")
	}
}
