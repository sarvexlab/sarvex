// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func newSeedCmd() *cobra.Command {
	var storeID string
	var products int
	var messy bool
	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Seed (or extend) a simulated store's catalog",
		Long: "Writes products directly into the simulated store's persistent catalog. This is a " +
			"store-side action only — it does not trigger an import; that happens when you click " +
			"\"trigger import\" in the console, which pulls this catalog through the real connector.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if storeID == "" {
				return fmt.Errorf("--store is required")
			}
			ctx := context.Background()
			d, err := newDeps(ctx)
			if err != nil {
				return err
			}
			store, err := d.cli.Seed(ctx, storeID, products, messy)
			if err != nil {
				return err
			}
			fmt.Printf("seeded store %s: %d products total (messy=%v)\n", storeID, len(store.Products), messy)
			return nil
		},
	}
	cmd.Flags().StringVar(&storeID, "store", "", "store_id to seed (required)")
	cmd.Flags().IntVar(&products, "products", 20, "number of products to seed")
	cmd.Flags().BoolVar(&messy, "messy", false, "seed deliberate readiness issues (missing price/title, stock inconsistency)")
	return cmd
}
