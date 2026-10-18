// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package stack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
)

// The plane entrypoints take a config *file*, not a directory —
// utils.LoadConfigurations reads whatever path it is handed, and cmd/root.go
// composes it as <configPath>/configs.<env>.json. Passing the directory
// instead fails at startup with a bare read error naming neither the plane
// nor the missing filename, which is a slow thing to diagnose.
func TestConfigFileMatchesTheLayoutOnDisk(t *testing.T) {
	// ../../../.. → repo root, from cmd/sarvex/internal/stack.
	root := filepath.Join("..", "..", "..", "..")
	opts := Options{ConfigDir: filepath.Join(root, "configs"), Env: "local"}

	for _, plane := range []string{"serving", "sync", "control"} {
		path := configFile(opts, plane)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("configFile(%q) = %q, which does not exist: %v", plane, path, err)
		}
	}
}

func TestConfigFileUsesEnvSuffix(t *testing.T) {
	tests := []struct {
		env  string
		want string
	}{
		{"local", "configs/control/configs.local.json"},
		{"development", "configs/control/configs.development.json"},
		// An empty env means the unsuffixed default, matching
		// cmd/root.go's buildConfigPath.
		{"", "configs/control/configs.json"},
	}
	for _, tt := range tests {
		got := configFile(Options{ConfigDir: "configs", Env: tt.env}, "control")
		if got != tt.want {
			t.Errorf("configFile(env=%q) = %q, want %q", tt.env, got, tt.want)
		}
	}
}

// Ports must be distinct, or two planes race for the same one and whichever
// loses reports a bind error the readiness wait can only see as a timeout.
func TestFreePortsAreDistinct(t *testing.T) {
	ports, err := freePorts(t.Context(), 4)
	if err != nil {
		t.Fatalf("freePorts: %v", err)
	}
	if len(ports) != 4 {
		t.Fatalf("got %d ports, want 4", len(ports))
	}
	seen := map[int]bool{}
	for _, p := range ports {
		if seen[p] {
			t.Errorf("port %d was handed out twice", p)
		}
		seen[p] = true
		if p <= 0 {
			t.Errorf("got invalid port %d", p)
		}
	}
}

func TestApplyDefaults(t *testing.T) {
	var opts Options
	opts.applyDefaults()
	if opts.ConfigDir != "./configs" {
		t.Errorf("ConfigDir = %q, want ./configs", opts.ConfigDir)
	}
	if opts.Env != "local" {
		t.Errorf("Env = %q, want local", opts.Env)
	}
	if opts.ReadyTimeout <= 0 {
		t.Errorf("ReadyTimeout = %v, want a positive default", opts.ReadyTimeout)
	}
}

// Dev-token auth only works while the control plane is in dev mode. Starting
// a stack that would reject every call this tool makes should say so up
// front, not fail later as an opaque 401 mid-onboarding.
func TestStartRefusesWhenKindeIsConfigured(t *testing.T) {
	t.Setenv("KINDE_ISSUER_URL", "https://example.kinde.com")
	if _, err := Start(t.Context(), Options{}); err == nil {
		t.Fatal("Start succeeded with KINDE_ISSUER_URL set")
	}
}

// The planes read MONGO_CONNECTION_STRING and REDIS_ADDRESS; every test
// suite and CI job sets MONGO_URI and REDIS_ADDR. Out of process
// scripts/dev/up.sh exports both, so the mismatch never showed; in process
// it leaves the planes with no Mongo address and a Redis address pointing
// at a cluster CI does not run.
func TestBridgeInfraEnvTranslatesTestNames(t *testing.T) {
	t.Setenv("MONGO_CONNECTION_STRING", "")
	t.Setenv("REDIS_ADDRESS", "")
	t.Setenv("MONGO_URI", "mongodb://localhost:27017")
	t.Setenv("REDIS_ADDR", "localhost:6379")

	if err := bridgeInfraEnv(); err != nil {
		t.Fatalf("bridgeInfraEnv: %v", err)
	}
	if got := os.Getenv("MONGO_CONNECTION_STRING"); got != "mongodb://localhost:27017" {
		t.Errorf("MONGO_CONNECTION_STRING = %q, want the MONGO_URI value", got)
	}
	if got := os.Getenv("REDIS_ADDRESS"); got != "localhost:6379" {
		t.Errorf("REDIS_ADDRESS = %q, want the REDIS_ADDR value", got)
	}
}

// A caller who set the plane-side name deliberately must win.
func TestBridgeInfraEnvDoesNotOverrideExplicitValues(t *testing.T) {
	t.Setenv("MONGO_CONNECTION_STRING", "mongodb://explicit:1234")
	t.Setenv("REDIS_ADDRESS", "explicit:6379")
	t.Setenv("MONGO_URI", "mongodb://localhost:27017")
	t.Setenv("REDIS_ADDR", "localhost:6379")

	if err := bridgeInfraEnv(); err != nil {
		t.Fatalf("bridgeInfraEnv: %v", err)
	}
	if got := os.Getenv("MONGO_CONNECTION_STRING"); got != "mongodb://explicit:1234" {
		t.Errorf("MONGO_CONNECTION_STRING = %q, want the explicit value preserved", got)
	}
	if got := os.Getenv("REDIS_ADDRESS"); got != "explicit:6379" {
		t.Errorf("REDIS_ADDRESS = %q, want the explicit value preserved", got)
	}
}

// With neither name set the planes would start against nothing and fail as
// a port that never opens. Say so up front instead.
func TestBridgeInfraEnvFailsWhenNeitherNameIsSet(t *testing.T) {
	t.Setenv("MONGO_CONNECTION_STRING", "")
	t.Setenv("MONGO_URI", "")
	err := bridgeInfraEnv()
	if err == nil {
		t.Fatal("bridgeInfraEnv succeeded with no Mongo address configured")
	}
	if !strings.Contains(err.Error(), "MONGO_URI") {
		t.Errorf("error does not name the variable a caller would set: %v", err)
	}
}

// The event bus resolves its NATS JetStream backend through a factory
// registered by an init() in eventbus/natsjetstream. That blank import lives
// in cmd/root.go — package main — so a launcher that starts the planes from
// anywhere else has to carry its own.
//
// Without it every plane fails to build its event bus, which surfaces only
// as a port that never opens after a 60-second readiness timeout. This
// pins it: the error, if any, must be about reaching NATS, never about a
// missing backend registration.
func TestNATSBackendIsRegistered(t *testing.T) {
	_, err := eventbus.New(eventbus.Config{
		ConsumerGroup: "stack-test",
		NATS:          eventbus.NATSConfig{URL: "nats://127.0.0.1:1"},
	})
	if err != nil && strings.Contains(err.Error(), "is not registered") {
		t.Fatalf("the NATS JetStream backend is not registered — this package needs a blank import of "+
			"internal/pkg/eventbus/natsjetstream: %v", err)
	}
}
