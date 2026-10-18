// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/sarvexlab/sarvex/internal/platform"

	"github.com/spf13/cobra"

	"github.com/sarvexlab/sarvex/cmd/sarvex/internal/client"
	"github.com/sarvexlab/sarvex/cmd/sarvex/internal/onboard"
)

func newOnboardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "onboard",
		Short: "Drive a merchant from nothing to a live UCP store",
		Long: "Creates a merchant, its stores, and everything each store needs to serve UCP —\n" +
			"business profile, payment handlers, a seeded catalog, a paired connector — then\n" +
			"drives both lifecycles (activation draft→active, journey prospect→live) to\n" +
			"completion.\n\n" +
			"Every detail is derived from the merchant's name, deterministically, so the same\n" +
			"name always produces the same fixture and a failing run can be rebuilt from the\n" +
			"seed it reports.",
	}
	cmd.AddCommand(newOnboardMerchantCmd(), newOnboardStoreCmd(), newOnboardStatusCmd())
	return cmd
}

func newOnboardMerchantCmd() *cobra.Command {
	var opts onboard.Options
	var noSeed bool

	cmd := &cobra.Command{
		Use:   "merchant <name>",
		Short: "Create a merchant and drive its stores to live — the one-argument path",
		Args:  cobra.ExactArgs(1),
		Example: "  sarvex onboard merchant \"Acme Widgets\"\n" +
			"  sarvex onboard merchant \"Acme Widgets\" --stores 2 --messy\n" +
			"  sarvex onboard merchant \"Acme Widgets\" --seed 4815162342 -o json",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateOutput(); err != nil {
				return err
			}
			opts.Name = args[0]
			opts.Timeout = global.timeout
			opts.Progress = progressWriter()

			ctx, cancel := context.WithTimeout(cmd.Context(), global.timeout+30*time.Second)
			defer cancel()

			operator, err := client.NewOperator(global.controlURL, 30*time.Second)
			if err != nil {
				return err
			}

			if !noSeed {
				deps, err := newSimDeps(ctx)
				if err != nil {
					return fmt.Errorf("%w\n\nSeeding writes to the simulated store's own database. Start the local "+
						"stack with `make dev-up`, or pass --no-seed to onboard a store whose catalog is "+
						"managed elsewhere", err)
				}
				defer deps.close()
				opts.Seeder = deps.seeder
				opts.SimStoreBaseURL = deps.baseURL
			}

			result, err := onboard.Run(ctx, operator, opts)
			if err != nil {
				return err
			}
			return reportOnboard(result)
		},
	}

	cmd.Flags().IntVar(&opts.Stores, "stores", 1, "Number of stores to create for the merchant")
	cmd.Flags().StringVar(&opts.Platform, "platform", string(platform.Default()), "Commerce platform: "+platformChoices())
	cmd.Flags().IntVar(&opts.Products, "products", 12, "Catalog products to seed per store")
	cmd.Flags().BoolVar(&opts.Messy, "messy", false, "Seed deliberate readiness problems, exercising the remediation path")
	cmd.Flags().Int64Var(&opts.Seed, "seed", 0, "Fixture seed (0 derives it from the name)")
	cmd.Flags().BoolVar(&noSeed, "no-seed", false, "Skip catalog seeding — for a store whose backend Sarvex does not own")
	return cmd
}

func newOnboardStoreCmd() *cobra.Command {
	var merchantID string
	var opts onboard.Options
	var noSeed bool

	cmd := &cobra.Command{
		Use:   "store <name>",
		Short: "Add a store to an existing merchant and drive it to live",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateOutput(); err != nil {
				return err
			}
			if merchantID == "" {
				return fmt.Errorf("--merchant is required")
			}
			opts.Name = args[0]
			opts.Stores = 1
			opts.Timeout = global.timeout
			opts.Progress = progressWriter()

			ctx, cancel := context.WithTimeout(cmd.Context(), global.timeout+30*time.Second)
			defer cancel()

			merchant, err := client.NewMerchant(global.controlURL, merchantID, 30*time.Second)
			if err != nil {
				return err
			}

			if !noSeed {
				deps, err := newSimDeps(ctx)
				if err != nil {
					return err
				}
				defer deps.close()
				opts.Seeder = deps.seeder
				opts.SimStoreBaseURL = deps.baseURL
			}

			result, err := onboard.RunStore(ctx, merchant, merchantID, opts)
			if err != nil {
				return err
			}
			return reportOnboard(result)
		},
	}

	cmd.Flags().StringVar(&merchantID, "merchant", "", "Merchant to add the store to (required)")
	cmd.Flags().StringVar(&opts.Platform, "platform", string(platform.Default()), "Commerce platform: "+platformChoices())
	cmd.Flags().IntVar(&opts.Products, "products", 12, "Catalog products to seed")
	cmd.Flags().BoolVar(&opts.Messy, "messy", false, "Seed deliberate readiness problems")
	cmd.Flags().Int64Var(&opts.Seed, "seed", 0, "Fixture seed (0 derives it from the name)")
	cmd.Flags().BoolVar(&noSeed, "no-seed", false, "Skip catalog seeding")
	return cmd
}

func newOnboardStatusCmd() *cobra.Command {
	var merchantID string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show both lifecycles for every store a merchant owns",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateOutput(); err != nil {
				return err
			}
			if merchantID == "" {
				return fmt.Errorf("--merchant is required")
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
			defer cancel()

			merchant, err := client.NewMerchant(global.controlURL, merchantID, 30*time.Second)
			if err != nil {
				return err
			}

			result, err := onboard.Status(ctx, merchant, merchantID)
			if err != nil {
				return err
			}
			return reportOnboard(result)
		},
	}

	cmd.Flags().StringVar(&merchantID, "merchant", "", "Merchant to report on (required)")
	return cmd
}

// reportOnboard prints a result and fails the command when a store did not
// go live — a store stuck short of active is the outcome a caller most
// needs to notice, so it must not exit 0.
func reportOnboard(result *onboard.Result) error {
	if global.output == outputJSON {
		if err := emitJSON(os.Stdout, result); err != nil {
			return err
		}
	} else {
		printOnboardTable(result)
	}
	if !result.AllLive() {
		return fmt.Errorf("%d of %d store(s) did not reach %s/%s",
			countNotLive(result), len(result.Stores), onboard.StateActive, onboard.JourneyLive)
	}
	return nil
}

func countNotLive(result *onboard.Result) int {
	n := 0
	for _, s := range result.Stores {
		if !s.Live() {
			n++
		}
	}
	return n
}

func printOnboardTable(result *onboard.Result) {
	fmt.Printf("\nmerchant %s — %s (seed %d)\n\n", result.MerchantID, result.Name, result.Seed)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "STORE\tNAME\tACTIVATION\tJOURNEY\t")
	for _, s := range result.Stores {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t\n", s.StoreID, s.DisplayName, s.UCPState, s.JourneyState)
	}
	_ = w.Flush()

	for _, s := range result.Stores {
		if s.Blocking != "" {
			fmt.Printf("\n%s blocked: %s\n", s.StoreID, s.Blocking)
		}
	}
	fmt.Println()
}

// platformChoices lists the supported platforms for a flag's help text.
func platformChoices() string {
	names := platform.Names()
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = string(n)
	}
	return strings.Join(out, " or ")
}
