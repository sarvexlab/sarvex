// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"gopkg.in/go-playground/assert.v1"
	"testing"
)

func TestBuildConfigPathWithoutEnv(t *testing.T) {
	want := "./config/communication/configs.json"
	got := buildConfigPath("./config/communication", "")

	assert.Equal(t, want, got)
}

func TestBuildConfigPathWithEnv(t *testing.T) {
	want := "./config/communication/configs.development.json"
	got := buildConfigPath("./config/communication", "development")

	assert.Equal(t, want, got)
}
