// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package safego

import (
	"sync"
	"testing"
)

func TestGo_RunsFn(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	ran := false
	Go("test", func() {
		defer wg.Done()
		ran = true
	})
	wg.Wait()
	if !ran {
		t.Fatal("expected fn to run")
	}
}

func TestGo_RecoversPanic(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	Go("test-panic", func() {
		defer wg.Done()
		panic("boom")
	})
	wg.Wait() // must not crash the test process
}
