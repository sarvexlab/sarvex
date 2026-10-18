// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/sarvexlab/sarvex/internal/platform"

	specall "github.com/sarvexlab/sarvex/internal/spec/all"

	"github.com/spf13/cobra"

	"github.com/sarvexlab/sarvex/cmd/sarvex/internal/client"
	"github.com/sarvexlab/sarvex/cmd/sarvex/internal/conform"
	"github.com/sarvexlab/sarvex/cmd/sarvex/internal/officialsuite"
	"github.com/sarvexlab/sarvex/cmd/sarvex/internal/onboard"
	"github.com/sarvexlab/sarvex/cmd/sarvex/internal/stack"
	ucpagent "github.com/sarvexlab/sarvex/internal/pkg/ucp/agent"
	cataloguev1 "github.com/sarvexlab/sarvex/internal/pkg/ucp/catalogue/v1"
	"github.com/sarvexlab/sarvex/internal/planes/sync/sim"
)

const outputJUnit = "junit"

func newConformCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "conform",
		Short: "Run the UCP agent surface as an assertion suite",
	}
	cmd.AddCommand(newConformRunCmd(), newConformServeCmd())
	return cmd
}

// onboardArgs bundles the CLI-facing knobs onboarding needs, shared by
// `conform run` and `conform serve`.
type onboardArgs struct {
	storeID      string
	merchantName string
	seed         int64
	products     int
	inProcess    bool
	configDir    string
}

// onboardStore starts the stack (if requested) and onboards a fresh
// merchant unless args.storeID already names one, asserting it reaches
// active/live. It returns the deps needed to drive the resulting store
// further and a cleanup func that tears everything down in the same order
// `conform run` always has: deps before the stack, since the stack owns the
// infrastructure deps connects to.
func onboardStore(ctx context.Context, args onboardArgs) (deps *simDeps, storeID string, seed int64, cleanup func(), err error) {
	var st *stack.Stack
	if args.inProcess {
		// Start the planes ourselves on OS-assigned ports, then point this
		// run's URLs at them. Nothing needs to have been running first, and
		// nothing can collide with a dev stack.
		st, err = stack.Start(ctx, stack.Options{
			ConfigDir: args.configDir,
			SimStoreFactory: func(baseURL string) (http.Handler, error) {
				var err error
				deps, err = newSimDeps(ctx)
				if err != nil {
					return nil, err
				}
				deps.baseURL = baseURL
				return deps.newStoreHandler(baseURL), nil
			},
		})
		if err != nil {
			return nil, "", 0, nil, fmt.Errorf("%w\n\nIn-process mode still needs Mongo, Redis, NATS and Vault. "+
				"In CI those are services; locally, `make dev-up` starts them", err)
		}

		global.controlURL = st.ControlURL
		global.servingURL = st.ServingURL
		_, _ = fmt.Fprintf(progressWriter(), "in-process stack: control %s serving %s\n", st.ControlURL, st.ServingURL)
	} else {
		deps, err = newSimDeps(ctx)
		if err != nil {
			return nil, "", 0, nil, fmt.Errorf("%w\n\nThe suite drives a simulated store, which needs Mongo and Redis. "+
				"Start them with `make dev-up`, or pass --in-process to start the whole stack here", err)
		}
	}
	// deps is set by the SimStoreFactory above, which Start always calls
	// when it is non-nil — but only after Start gets far enough to reserve
	// ports, so guard rather than assume.
	if deps == nil {
		return nil, "", 0, nil, fmt.Errorf("simulated store was never built")
	}

	cleanup = func() {
		deps.close()
		if st != nil {
			st.Close()
		}
	}

	runSeed := args.seed
	resultStoreID := args.storeID
	if resultStoreID == "" {
		merchantName := args.merchantName
		if merchantName == "" {
			merchantName = fmt.Sprintf("Conformance %d", time.Now().Unix())
		}
		operator, err := client.NewOperator(global.controlURL, 30*time.Second)
		if err != nil {
			cleanup()
			return nil, "", 0, nil, err
		}
		result, err := onboard.Run(ctx, operator, onboard.Options{
			Name:            merchantName,
			Seed:            args.seed,
			Stores:          1,
			Platform:        string(platform.Default()),
			Products:        args.products,
			SimStoreBaseURL: deps.baseURL,
			Timeout:         global.timeout,
			Seeder:          deps.seeder,
			Progress:        progressWriter(),
		})
		if err != nil {
			cleanup()
			return nil, "", 0, nil, fmt.Errorf("onboarding phase: %w", err)
		}
		if !result.AllLive() {
			// Stop here rather than running protocol checks against a store
			// that never came up: every check would fail, and none of those
			// failures would be about the protocol.
			cleanup()
			return nil, "", 0, nil, fmt.Errorf("onboarding phase: store did not reach %s/%s (seed %d)",
				onboard.StateActive, onboard.JourneyLive, result.Seed)
		}
		resultStoreID = result.Stores[0].StoreID
		runSeed = result.Seed
	}

	return deps, resultStoreID, runSeed, cleanup, nil
}

func newConformRunCmd() *cobra.Command {
	var storeID, merchantName string
	var seed int64
	var products int
	var noFaults, inProcess bool
	var configDir string

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Drive every implemented capability, including failure paths, and report",
		Long: "Runs in two phases.\n\n" +
			"Phase 1 onboards a fresh merchant from nothing, asserting it reaches active/live.\n" +
			"An onboarding regression fails here, before any protocol check runs. Pass --store\n" +
			"to skip this and drive a store that already exists.\n\n" +
			"Phase 2 drives the agent capability matrix against that store: discovery, catalog\n" +
			"search and lookup, the checkout lifecycle, idempotent replay, and the documented\n" +
			"failure paths (out of stock, unknown payment handler, broken catalog attribute).\n\n" +
			"A run that executes no checks fails. That is deliberate: the suites this replaces\n" +
			"both reported success while asserting nothing.",
		Example: "  sarvex conform run\n" +
			"  sarvex conform run --store <id>\n" +
			"  sarvex conform run --seed 4815162342 -o junit > results.xml",
		RunE: func(cmd *cobra.Command, _ []string) error {
			switch global.output {
			case outputHuman, outputJSON, outputJUnit:
			default:
				return fmt.Errorf("unknown --output %q: want %s, %s or %s",
					global.output, outputHuman, outputJSON, outputJUnit)
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), global.timeout+2*time.Minute)
			defer cancel()

			deps, resolvedStoreID, runSeed, cleanup, err := onboardStore(ctx, onboardArgs{
				storeID:      storeID,
				merchantName: merchantName,
				seed:         seed,
				products:     products,
				inProcess:    inProcess,
				configDir:    configDir,
			})
			if err != nil {
				return err
			}
			defer cleanup()
			storeID = resolvedStoreID

			// Phase 2: drive the protocol against it.
			opts := conform.Options{
				StoreID:  storeID,
				Seed:     runSeed,
				Timeout:  60 * time.Second,
				Progress: conformProgress(),
			}
			if !noFaults {
				opts.Fault = simFault{cli: deps.seeder.cli}
			}

			report, err := conform.Run(ctx, ucpagent.NewAgent(global.servingURL, global.controlURL, nil), opts)
			if err != nil {
				return err
			}

			if err := writeReport(report); err != nil {
				return err
			}
			return report.Err()
		},
	}

	cmd.Flags().StringVar(&storeID, "store", "", "Drive an existing store instead of onboarding a fresh one")
	cmd.Flags().StringVar(&merchantName, "merchant-name", "", "Name for the merchant the run creates (default: timestamped)")
	cmd.Flags().Int64Var(&seed, "seed", 0, "Fixture seed (0 derives it from the merchant name)")
	cmd.Flags().IntVar(&products, "products", 12, "Catalog products to seed")
	cmd.Flags().BoolVar(&noFaults, "no-faults", false, "Skip the failure-path checks (they report as skipped, not hidden)")
	cmd.Flags().BoolVar(&inProcess, "in-process", false, "Start the planes in this process on OS-assigned ports instead of using a running stack")
	cmd.Flags().StringVar(&configDir, "config-dir", "./configs", "Plane config directory, for --in-process")
	return cmd
}

func newConformServeCmd() *cobra.Command {
	var storeID, merchantName string
	var seed int64
	var products int
	var inProcess bool
	var configDir, outputDir, readyFile, ucpVersions string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Onboard a store and keep it running for the official UCP conformance suite",
		Long: "Runs the same onboarding phase `conform run` does, then — instead of driving\n" +
			"Sarvex's own assertions — writes the fixture files the official UCP conformance\n" +
			"suite (github.com/Universal-Commerce-Protocol/conformance) reads, one\n" +
			"conformance_input.json per --ucp-version and one shared test_fixtures.json, and\n" +
			"blocks until interrupted.\n\n" +
			"That suite is the authority on protocol conformance; this command exists so CI\n" +
			"can point it at a real, running Sarvex instead of Sarvex re-deriving the same\n" +
			"checks. See scripts/ucp-conformance/run.sh for the CI side of this handshake.",
		Example: "  sarvex conform serve --in-process --output-dir /tmp/ucp-fixtures --ready-file /tmp/ucp-fixtures/ready",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if outputDir == "" {
				return fmt.Errorf("--output-dir is required")
			}

			// Advertise the official suite's mock payment handler for this
			// conformance run. Set before any plane (and its catalogue) starts,
			// so the in-process control/serving planes all see it. Never set
			// outside `conform serve`, so production stores never advertise it.
			if err := os.Setenv(cataloguev1.ConformanceMockHandlerEnvVar, "1"); err != nil {
				return fmt.Errorf("enable conformance mock payment handler: %w", err)
			}

			versions := strings.Split(ucpVersions, ",")
			for i, v := range versions {
				versions[i] = strings.TrimSpace(v)
			}

			// No overall timeout: this command serves until the caller (a
			// CI script, or a human) is done with it, not until some fixed
			// deadline. Onboarding itself still respects global.timeout.
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			deps, resolvedStoreID, _, cleanup, err := onboardStore(ctx, onboardArgs{
				storeID:      storeID,
				merchantName: merchantName,
				seed:         seed,
				products:     products,
				inProcess:    inProcess,
				configDir:    configDir,
			})
			if err != nil {
				return err
			}
			defer cleanup()
			storeID = resolvedStoreID

			agent := ucpagent.NewAgent(global.servingURL, global.controlURL, nil)
			input, fixtures, err := officialsuite.Build(ctx, agent, simFault{cli: deps.seeder.cli}, storeID, versions[0])
			if err != nil {
				return fmt.Errorf("build official-suite fixtures: %w", err)
			}
			if _, err := officialsuite.WriteFixtures(outputDir, fixtures); err != nil {
				return err
			}

			// The suite completes a checkout with a payment instrument whose
			// handler_id must be one this store advertises. Read the store's
			// own advertised handler off its profile and write a matching
			// payment_instruments.csv, rather than let the suite fall back to
			// its flower_shop fixture's handler (which this store rejects).
			profile, err := agent.FetchProfile(ctx, storeID)
			if err != nil {
				return fmt.Errorf("fetch profile for payment handler: %w", err)
			}
			if len(profile.PaymentHandlers) == 0 {
				return fmt.Errorf("store %s advertises no payment handler, so the suite could never complete a checkout", storeID)
			}
			if _, err := officialsuite.WritePaymentInstruments(outputDir, profile.PaymentHandlers[0]); err != nil {
				return err
			}
			for _, v := range versions {
				input.UCPVersion = v
				path, err := officialsuite.WriteInput(outputDir, input)
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintf(progressWriter(), "wrote %s\n", path)
			}

			// The official suite fetches /.well-known/ucp straight off the
			// URL it's given — it has no notion of Sarvex's own multi-tenant
			// path layout — so that URL has to be this store's own scoped
			// base (internal/planes/serving/api/entry.go's servingGroupName
			// route: /ucp/:platform/:storeID/...), the same one
			// ucpagent.Agent builds internally for every other call.
			storeServerURL := fmt.Sprintf("%s/%s/%s", global.servingURL, platform.Default(), storeID)
			_, _ = fmt.Fprintf(progressWriter(), "store %s serving at %s (control %s)\n", storeID, storeServerURL, global.controlURL)
			if readyFile != "" {
				if err := os.WriteFile(readyFile, []byte(storeServerURL), 0o600); err != nil {
					return fmt.Errorf("write ready file: %w", err)
				}
			}

			<-ctx.Done()
			_, _ = fmt.Fprintln(progressWriter(), "shutting down")
			return nil
		},
	}

	cmd.Flags().StringVar(&storeID, "store", "", "Serve an existing store instead of onboarding a fresh one")
	cmd.Flags().StringVar(&merchantName, "merchant-name", "", "Name for the merchant this run creates (default: timestamped)")
	cmd.Flags().Int64Var(&seed, "seed", 0, "Fixture seed (0 derives it from the merchant name)")
	cmd.Flags().IntVar(&products, "products", 12, "Catalog products to seed")
	cmd.Flags().BoolVar(&inProcess, "in-process", false, "Start the planes in this process on OS-assigned ports instead of using a running stack")
	cmd.Flags().StringVar(&configDir, "config-dir", "./configs", "Plane config directory, for --in-process")
	cmd.Flags().StringVar(&outputDir, "output-dir", "", "Directory to write conformance_input-<version>.json and test_fixtures.json into (required)")
	cmd.Flags().StringVar(&readyFile, "ready-file", "", "File to write the serving URL to once the store is ready (default: none)")
	cmd.Flags().StringVar(&ucpVersions, "ucp-versions", servableVersions(), "Comma-separated UCP releases to write a conformance_input.json for")
	return cmd
}

// servableVersions is every UCP release Sarvex serves, comma-separated: the
// default set to write a conformance_input.json for.
func servableVersions() string {
	servable := specall.Default().Servable()
	vs := make([]string, len(servable))
	for i, v := range servable {
		vs[i] = v.String()
	}
	return strings.Join(vs, ",")
}

// conformProgress streams a line per check in human mode. In JSON and JUnit
// mode it stays quiet, so stdout holds exactly one document.
func conformProgress() func(conform.Result) {
	if global.output != outputHuman {
		return nil
	}
	return func(res conform.Result) {
		fmt.Printf("  %-6s %s\n", res.Outcome, res.ID)
	}
}

func writeReport(report *conform.Report) error {
	switch global.output {
	case outputJSON:
		return conform.WriteJSON(os.Stdout, report)
	case outputJUnit:
		return conform.WriteJUnit(os.Stdout, report)
	default:
		return conform.WriteHuman(os.Stdout, report)
	}
}

// simFault injects failure conditions through the simulated store's own
// mutation path — the same one `sim mutate` uses, so a failure the suite
// produces can be reproduced by hand with a documented command.
type simFault struct {
	cli *sim.CLI
}

func (f simFault) SetStock(ctx context.Context, storeID, sku string, quantity int) error {
	_, err := f.cli.MutateStock(ctx, storeID, sku, quantity)
	return err
}

func (f simFault) BreakAttribute(ctx context.Context, storeID, sku, field string) error {
	_, err := f.cli.MutateAttr(ctx, storeID, sku, field)
	return err
}

func (f simFault) Fix(ctx context.Context, storeID, sku, field string) error {
	_, err := f.cli.Fix(ctx, storeID, sku, field)
	return err
}
