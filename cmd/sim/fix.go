// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func newFixCmd() *cobra.Command {
	var storeID, sku, field string
	cmd := &cobra.Command{
		Use:   "fix",
		Short: "Set a product field back to a good value and signal the change, so its task clears",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if storeID == "" || sku == "" || field == "" {
				return fmt.Errorf("--store, --sku, and --field are required")
			}
			ctx := context.Background()
			d, err := newDeps(ctx)
			if err != nil {
				return err
			}
			if _, err := d.cli.Fix(ctx, storeID, sku, field); err != nil {
				return err
			}
			fmt.Printf("store %s sku %s: fixed %s (signal sent)\n", storeID, sku, field)
			return nil
		},
	}
	cmd.Flags().StringVar(&storeID, "store", "", "store_id (required)")
	cmd.Flags().StringVar(&sku, "sku", "", "product sku (required)")
	cmd.Flags().StringVar(&field, "field", "", "field to fix: title, price, stock, or currency")
	return cmd
}
