// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"time"

	controlapi "github.com/sarvexlabs/sarvex.cp/internal/planes/control/api"
	servingapi "github.com/sarvexlabs/sarvex.cp/internal/planes/serving/api"
	syncapi "github.com/sarvexlabs/sarvex.cp/internal/planes/sync/api"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	// The eventbus package itself stays free of a compile-time NATS
	// dependency (see checkoutescalation's isolation_test.go), so the
	// JetStream backend must be registered explicitly here, once, for this
	// single binary that hosts all three planes (--service selects which
	// one runs).
	_ "github.com/sarvexlabs/sarvex.cp/internal/pkg/eventbus/natsjetstream"
)

const (
	ConfigPath  = "configPath"
	ServiceName = "service"
	TLS         = "tls"
	ENV         = "env"
	ServerPort  = "port"

	// Per-plane ports, used only when ServiceName is All: all three planes
	// bind a listener in the same process, so the single shared --port
	// that single-plane modes use can't serve all of them. Defaults match
	// scripts/dev/lib.sh's split-deployment ports so the monolith and
	// split topologies are interchangeable without reconfiguring.
	ServingPort = "servingPort"
	ControlPort = "controlPort"
	SyncPort    = "syncPort"

	// ProxyPort, when set (non-zero) in All mode, starts a path-routing
	// reverse proxy in front of the three per-plane ports and binds it
	// here instead. A single-container deployment exposes one port, so it
	// can't offer servingPort/controlPort/syncPort directly the way three
	// separate split-deployment pods do — this proxy lets one container
	// still reach all three planes, by reproducing the gateway chart's
	// path-routing rules (see proxyMux) in front of them. Zero (the
	// default) disables it, which is what local dev wants: up-monolith.sh
	// talks to servingPort/controlPort/syncPort directly and has no
	// single-ingress-port constraint to work around.
	ProxyPort = "proxyPort"
)

const (
	Serving = "serving"
	Control = "control"
	Sync    = "sync"

	// All runs every plane in this one process (see runAllPlanes) instead
	// of one plane per process. Same binary, same plane code, same
	// event-bus/Mongo boundaries (internal/planes/doc.go) either way —
	// this only changes how many processes host them, so switching back
	// to one-plane-per-process is just running --service=serving|control|sync
	// again against the same image.
	All = "all"
)

var serviceMap = map[string]func(bool, int, string) error{
	Serving: func(tls bool, port int, env string) error {
		svc := servingapi.NewServingPlaneEntry(buildConfigPath(viper.GetString(ConfigPath), env))
		if err := svc.Init(&tls, &port, &env); err != nil {
			log.Println("something went wrong when starting serving plane: ", err)
			return err
		}
		return nil
	},
	Control: func(tls bool, port int, env string) error {
		svc := controlapi.NewControlPlaneEntry(buildConfigPath(viper.GetString(ConfigPath), env))
		if err := svc.Init(&tls, &port, &env); err != nil {
			log.Println("something went wrong when starting control plane: ", err)
			return err
		}
		return nil
	},
	Sync: func(tls bool, port int, env string) error {
		svc := syncapi.NewSyncPlaneEntry(buildConfigPath(viper.GetString(ConfigPath), env))
		if err := svc.Init(&tls, &port, &env); err != nil {
			log.Println("something went wrong when starting sync plane: ", err)
			return err
		}
		return nil
	},
	All: func(tls bool, _ int, env string) error {
		return runAllPlanes(tls, env)
	},
	"": func(tls bool, port int, env string) error {
		log.Println("unknown service name")
		return errors.New("unknown service name")
	},
}

// runAllPlanes starts all three planes in this one process — the
// "monolith" deployment topology. Each plane keeps its existing entry.go
// unchanged: it builds its config path from configPath/<plane-name> (the
// same configs/{serving,control,sync} layout split deployment already
// uses — pass the parent, e.g. --configPath=./configs) and binds its own
// port (--servingPort/--controlPort/--syncPort). Planes still only ever
// talk to each other over the event bus or read-only Mongo collections
// (internal/planes/doc.go) — nothing here gives one plane a Go import on
// another, so this is purely a process-composition change.
func runAllPlanes(tls bool, env string) error {
	basePath := viper.GetString(ConfigPath)

	planes := []struct {
		name string
		run  func() error
	}{
		{Serving, func() error {
			svc := servingapi.NewServingPlaneEntry(buildConfigPath(filepath.Join(basePath, Serving), env))
			port := viper.GetInt(ServingPort)
			return svc.Init(&tls, &port, &env)
		}},
		{Control, func() error {
			svc := controlapi.NewControlPlaneEntry(buildConfigPath(filepath.Join(basePath, Control), env))
			port := viper.GetInt(ControlPort)
			return svc.Init(&tls, &port, &env)
		}},
		{Sync, func() error {
			svc := syncapi.NewSyncPlaneEntry(buildConfigPath(filepath.Join(basePath, Sync), env))
			port := viper.GetInt(SyncPort)
			return svc.Init(&tls, &port, &env)
		}},
	}

	// Each plane's entry.go already installs its own SIGTERM/interrupt
	// handler and shuts itself down gracefully on receipt — os/signal
	// delivers a received signal to every registered channel, so a real
	// shutdown signal reaches and stops all three without any
	// coordination between them. We just run all three to completion and
	// surface the first error, if any: a plane that fails at startup
	// (e.g. bad config) doesn't take the other two down with it, the same
	// way a split deployment degrades when only one of the three
	// processes is unhealthy.
	type result struct {
		name string
		err  error
	}
	results := make(chan result, len(planes))
	for _, p := range planes {
		go func() { results <- result{name: p.name, err: p.run()} }()
	}

	if proxyPort := viper.GetInt(ProxyPort); proxyPort != 0 {
		mux, err := proxyMux(viper.GetInt(ServingPort), viper.GetInt(ControlPort), viper.GetInt(SyncPort))
		if err != nil {
			return fmt.Errorf("build monolith proxy: %w", err)
		}
		proxySrv := &http.Server{
			Addr:              fmt.Sprintf(":%d", proxyPort),
			Handler:           mux,
			ReadHeaderTimeout: 10 * time.Second,
		}
		go func() {
			log.Printf("starting monolith proxy on port %d (serving=%d control=%d sync=%d)",
				proxyPort, viper.GetInt(ServingPort), viper.GetInt(ControlPort), viper.GetInt(SyncPort))
			if err := proxySrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("monolith proxy exited: %v", err)
			}
		}()
	}

	var firstErr error
	for range planes {
		r := <-results
		if r.err != nil {
			log.Printf("%s plane exited with error: %v", r.name, r.err)
			if firstErr == nil {
				firstErr = r.err
			}
		}
	}
	return firstErr
}

// proxyMux reproduces modules/global-lb/main.tf's url_map path rules
// (/.well-known/ucp + /ucp/* -> serving, /connector/* -> sync, everything
// else -> control) as an in-process reverse proxy across the three
// localhost ports runAllPlanes just started. Keeping these rules in sync
// by hand across the gateway chart and here is the tradeoff for not having to
// touch any plane's entry.go to get them onto one ingress port.
func proxyMux(servingPort, controlPort, syncPort int) (http.Handler, error) {
	serving, err := reverseProxyTo(servingPort)
	if err != nil {
		return nil, err
	}
	control, err := reverseProxyTo(controlPort)
	if err != nil {
		return nil, err
	}
	sync, err := reverseProxyTo(syncPort)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.Handle("/.well-known/ucp", serving)
	mux.Handle("/ucp/", serving)
	mux.Handle("/connector/", sync)
	mux.Handle("/", control)
	return mux, nil
}

func reverseProxyTo(port int) (*httputil.ReverseProxy, error) {
	target, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	if err != nil {
		return nil, err
	}
	return httputil.NewSingleHostReverseProxy(target), nil
}

func buildConfigPath(path string, env string) string {
	configFileName := "configs.json"
	if env != "" {
		configFileName = fmt.Sprintf("configs.%s.json", env)
	}
	return fmt.Sprintf("%s/%s", path, configFileName)
}

var rootCmd = &cobra.Command{
	Use:   "sarvex",
	Short: "",
	Long:  "",
	RunE: func(cmd *cobra.Command, args []string) error {
		serviceName := viper.GetString(ServiceName)
		tls := viper.GetBool(TLS)
		port := viper.GetInt(ServerPort)
		env := viper.GetString(ENV)
		if err := validateRequiredFlags(ConfigPath, serviceName, env); err != nil {
			log.Println(err)
			return err
		}
		if server, ok := serviceMap[serviceName]; ok {
			return server(tls, port, env)
		}
		return nil
	},
}

func init() {

	rootCmd.PersistentFlags().String(ConfigPath, "", "This parameter is path to store config for application (required)")
	rootCmd.PersistentFlags().String(ServiceName, "", "This parameter use to decide which service you want to start (required). One of: serving, sync, control, all")
	rootCmd.PersistentFlags().Bool(TLS, false, "")
	rootCmd.PersistentFlags().String(ENV, "", "This parameter use to decide which env file is load in runtime (required)")
	rootCmd.PersistentFlags().Int(ServerPort, 8081, "")
	rootCmd.PersistentFlags().Int(ServingPort, 8083, "Serving-plane port when --service=all (ignored otherwise; single-plane modes use --port)")
	rootCmd.PersistentFlags().Int(ControlPort, 8081, "Control-plane port when --service=all (ignored otherwise; single-plane modes use --port)")
	rootCmd.PersistentFlags().Int(SyncPort, 8082, "Sync-plane port when --service=all (ignored otherwise; single-plane modes use --port)")
	rootCmd.PersistentFlags().Int(ProxyPort, 0, "Path-routing reverse-proxy port when --service=all (0 disables it; a single-container deployment sets this to the container's exposed port, since Cloud Run only ingresses one port per service)")

	_ = rootCmd.MarkPersistentFlagRequired(ConfigPath)
	_ = rootCmd.MarkPersistentFlagRequired(ServiceName)
	_ = rootCmd.MarkPersistentFlagRequired(ENV)

	_ = viper.BindPFlag(ConfigPath, rootCmd.PersistentFlags().Lookup(ConfigPath))
	_ = viper.BindPFlag(ServiceName, rootCmd.PersistentFlags().Lookup(ServiceName))
	_ = viper.BindPFlag(TLS, rootCmd.PersistentFlags().Lookup(TLS))
	_ = viper.BindPFlag(ENV, rootCmd.PersistentFlags().Lookup(ENV))
	_ = viper.BindPFlag(ServerPort, rootCmd.PersistentFlags().Lookup(ServerPort))
	_ = viper.BindPFlag(ServingPort, rootCmd.PersistentFlags().Lookup(ServingPort))
	_ = viper.BindPFlag(ControlPort, rootCmd.PersistentFlags().Lookup(ControlPort))
	_ = viper.BindPFlag(SyncPort, rootCmd.PersistentFlags().Lookup(SyncPort))
	_ = viper.BindPFlag(ProxyPort, rootCmd.PersistentFlags().Lookup(ProxyPort))
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		log.Println("application can't start", err)
		os.Exit(1)
	}
}

func validateRequiredFlags(configPath, serviceName, _ string) error {
	if configPath == "" {
		return errors.New("configPath is required")
	}
	if serviceName == "" {
		return errors.New("serviceName is required")
	}
	return nil
}
