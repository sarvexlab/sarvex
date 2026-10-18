// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package stack starts Sarvex's three planes inside this process, on
// OS-assigned ports, for a conformance run that must not depend on a stack
// somebody started earlier.
//
// It calls the same entrypoints the real binary calls —
// {serving,sync,control}api.NewXPlaneEntry(...).Init(...) — rather than
// re-wiring repositories and services by hand. That matters: a hand-built
// harness is a second copy of the composition root, and a second copy
// drifts. Anything this package can start, `sarvex --service=X` starts
// the same way.
//
// Ports are taken from the OS rather than fixed, so a run cannot collide
// with a dev stack, with CI, or with another run. Fixed ports are exactly
// how the suites this replaces ended up pointed at the wrong plane.
package stack

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	// Registers eventbus's NATS JetStream backend, which eventbus/backend.go
	// resolves through a factory that only exists once the package's init
	// has run — it says so in the error it would otherwise return.
	// cmd/root.go carries the same blank import, but that is package main,
	// so starting the planes from anywhere else has to carry it too.
	// test/e2e/journey/suite_test.go does the same for the same reason.
	_ "github.com/sarvexlab/sarvex/internal/pkg/eventbus/natsjetstream"
	controlapi "github.com/sarvexlab/sarvex/internal/planes/control/api"
	servingapi "github.com/sarvexlab/sarvex/internal/planes/serving/api"
	syncapi "github.com/sarvexlab/sarvex/internal/planes/sync/api"
)

// Stack is a running set of planes and the URLs to reach them at.
type Stack struct {
	ControlURL string
	ServingURL string
	SyncURL    string
	SimURL     string

	errs     chan error
	shutdown []func()
}

// Close stops what Stack owns. The planes themselves run until the process
// exits — their entrypoints install their own signal handling and offer no
// programmatic stop — which is fine for a one-shot conformance run.
func (s *Stack) Close() {
	for _, fn := range s.shutdown {
		fn()
	}
}

// Options configure a stack.
type Options struct {
	// ConfigDir is the directory holding per-plane config subdirectories
	// (configs/serving, configs/sync, configs/control). Defaults to
	// "./configs", which is correct when running from the repo root.
	ConfigDir string
	// Env selects the config profile. Defaults to "local".
	Env string
	// ReadyTimeout bounds how long to wait for each plane's port to open.
	ReadyTimeout time.Duration
	// SimStoreFactory builds the simulated merchant backend the connector
	// pulls from, given the base URL it will be reachable at.
	//
	// A factory rather than a ready-made handler because of ordering: the
	// simulated store's own dependencies read SYNC_PLANE_BASE_URL and
	// friends from the environment, and those are not correct until Start
	// has reserved ports and exported them. Start calls this after that,
	// and never before. Nil skips the simulated store entirely, which is
	// what a run against a real merchant backend wants.
	SimStoreFactory func(baseURL string) (http.Handler, error)
}

func (o *Options) applyDefaults() {
	if o.ConfigDir == "" {
		o.ConfigDir = "./configs"
	}
	if o.Env == "" {
		o.Env = "local"
	}
	if o.ReadyTimeout <= 0 {
		o.ReadyTimeout = 60 * time.Second
	}
}

// Start brings up the sim store and all three planes, and returns once each
// is accepting connections.
//
// The planes read their infrastructure addresses from the environment, so
// Start sets the cross-plane URLs to the ports it just chose. This mutates
// the process environment by design: the planes are running *in* this
// process, and that is how they are configured.
func Start(ctx context.Context, opts Options) (*Stack, error) {
	opts.applyDefaults()

	ports, err := freePorts(ctx, 4)
	if err != nil {
		return nil, fmt.Errorf("reserve ports: %w", err)
	}
	simPort, syncPort, servingPort, controlPort := ports[0], ports[1], ports[2], ports[3]

	s := &Stack{
		SimURL:     fmt.Sprintf("http://localhost:%d", simPort),
		SyncURL:    fmt.Sprintf("http://localhost:%d/v1", syncPort),
		ServingURL: fmt.Sprintf("http://localhost:%d/ucp", servingPort),
		ControlURL: fmt.Sprintf("http://localhost:%d/v1", controlPort),
		errs:       make(chan error, 4),
	}

	// The planes discover each other through these, exactly as
	// scripts/dev/up.sh exports them for the out-of-process stack.
	for k, v := range map[string]string{
		"SIM_STORE_BASE_URL":               s.SimURL,
		"SYNC_PLANE_BASE_URL":              s.SyncURL,
		"SERVING_BASE_URL":                 s.ServingURL,
		"CONTROL_BASE_URL":                 fmt.Sprintf("http://localhost:%d", controlPort),
		"VALIDATION_TEST_HARNESS_BASE_URL": fmt.Sprintf("http://localhost:%d", servingPort),
	} {
		if err := os.Setenv(k, v); err != nil {
			return nil, fmt.Errorf("set %s: %w", k, err)
		}
	}

	// KINDE_ISSUER_URL must stay unset: it is what puts the control plane
	// into dev-token mode, which is the only way this tool can authenticate.
	if os.Getenv("KINDE_ISSUER_URL") != "" {
		return nil, fmt.Errorf("KINDE_ISSUER_URL is set, so the control plane will verify real Kinde JWTs; " +
			"this tool mints dev tokens and cannot authenticate against it")
	}

	if err := bridgeInfraEnv(); err != nil {
		return nil, err
	}

	if opts.SimStoreFactory != nil {
		handler, err := opts.SimStoreFactory(s.SimURL)
		if err != nil {
			return nil, fmt.Errorf("build simulated store: %w", err)
		}
		srv := &http.Server{
			Addr:              fmt.Sprintf(":%d", simPort),
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
		}
		s.spawn(func() error {
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		})
		s.shutdown = append(s.shutdown, func() {
			shutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_ = srv.Shutdown(shutCtx)
		})
	}

	// Order matters only for readiness, not correctness: the planes
	// rendezvous through Mongo, Redis and the bus, not through startup
	// sequence.
	s.spawnPlane(servingapi.NewServingPlaneEntry(configFile(opts, "serving")), servingPort, opts.Env)
	s.spawnPlane(syncapi.NewSyncPlaneEntry(configFile(opts, "sync")), syncPort, opts.Env)
	s.spawnPlane(controlapi.NewControlPlaneEntry(configFile(opts, "control")), controlPort, opts.Env)

	ready := []struct {
		label string
		port  int
	}{
		{"serving plane", servingPort},
		{"sync plane", syncPort},
		{"control plane", controlPort},
	}
	if opts.SimStoreFactory != nil {
		ready = append(ready, struct {
			label string
			port  int
		}{"sim store", simPort})
	}

	for _, p := range ready {
		if err := waitTCP(ctx, p.port, opts.ReadyTimeout); err != nil {
			return nil, fmt.Errorf("%s never came up on :%d: %w%s", p.label, p.port, err, s.drainErr())
		}
	}
	return s, nil
}

// bridgeInfraEnv makes the plane-side infrastructure variables agree with
// the test-side ones.
//
// The two halves of this repo name the same things differently. The planes
// read MONGO_CONNECTION_STRING and REDIS_ADDRESS
// (internal/pkg/utils/configurations.go); every test suite, Makefile target
// and CI job sets MONGO_URI and REDIS_ADDR. Out of process that never
// mattered, because scripts/dev/up.sh exports both sets. In process it
// matters a great deal: without the bridge the planes get an empty Mongo
// connection string and fall back to the config file's RedisAddress, which
// in configs/*/configs.local.json points at a three-node cluster on
// :7000-7002 that CI does not run — and the failure surfaces only as a port
// that never opens.
//
// Existing plane-side values always win, so a caller who set them
// deliberately is never overridden.
func bridgeInfraEnv() error {
	for _, b := range []struct{ what, planeVar, testVar string }{
		{"MongoDB", "MONGO_CONNECTION_STRING", "MONGO_URI"},
		{"Redis", "REDIS_ADDRESS", "REDIS_ADDR"},
	} {
		if os.Getenv(b.planeVar) != "" {
			continue
		}
		v := os.Getenv(b.testVar)
		if v == "" {
			return fmt.Errorf("no %s address: set %s (what the planes read) or %s (what the test suites set)",
				b.what, b.planeVar, b.testVar)
		}
		if err := os.Setenv(b.planeVar, v); err != nil {
			return fmt.Errorf("set %s: %w", b.planeVar, err)
		}
	}
	return nil
}

// configFile builds the path a plane entrypoint expects.
//
// The entrypoints take a config *file*, not a directory:
// utils.LoadConfigurations reads the path it is given. cmd/root.go composes
// it as <configPath>/configs.<env>.json (buildConfigPath), and passing the
// directory instead fails at startup with a bare read error that names
// neither the plane nor the missing filename. Mirrored here rather than
// exported from cmd/root.go, which is package main.
func configFile(opts Options, plane string) string {
	name := "configs.json"
	if opts.Env != "" {
		name = fmt.Sprintf("configs.%s.json", opts.Env)
	}
	return fmt.Sprintf("%s/%s/%s", opts.ConfigDir, plane, name)
}

type planeEntry interface {
	Init(tls *bool, port *int, env *string) error
}

func (s *Stack) spawnPlane(entry planeEntry, port int, env string) {
	s.spawn(func() error {
		tls := false
		return entry.Init(&tls, &port, &env)
	})
}

func (s *Stack) spawn(fn func() error) {
	go func() {
		if err := fn(); err != nil {
			s.errs <- err
		}
	}()
}

// drainErr reports the first startup error a plane produced, if any, so a
// readiness timeout can say what actually went wrong rather than only that
// a port stayed shut.
func (s *Stack) drainErr() string {
	select {
	case err := <-s.errs:
		return fmt.Sprintf(" (plane reported: %v)", err)
	default:
		return ""
	}
}

// freePorts reserves n ports by binding and immediately releasing them.
//
// There is a window between release and re-bind, which is why all n are
// held open at once and closed together: that at least stops this function
// from handing out the same port twice.
func freePorts(ctx context.Context, n int) ([]int, error) {
	listeners := make([]net.Listener, 0, n)
	ports := make([]int, 0, n)
	defer func() {
		for _, l := range listeners {
			_ = l.Close()
		}
	}()

	var lc net.ListenConfig
	for range n {
		l, err := lc.Listen(ctx, "tcp", "localhost:0")
		if err != nil {
			return nil, err
		}
		listeners = append(listeners, l)
		addr, ok := l.Addr().(*net.TCPAddr)
		if !ok {
			return nil, fmt.Errorf("listener returned a %T, want *net.TCPAddr", l.Addr())
		}
		ports = append(ports, addr.Port)
	}
	return ports, nil
}

func waitTCP(ctx context.Context, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	dialer := net.Dialer{Timeout: time.Second}
	for {
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
