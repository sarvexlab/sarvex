// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func newMutateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mutate",
		Short: "Mutate a product in a simulated store and emit the matching connector signal",
	}
	cmd.AddCommand(newMutatePriceCmd(), newMutateStockCmd(), newMutateAttrCmd())
	return cmd
}

func newMutatePriceCmd() *cobra.Command {
	var storeID, sku string
	var to float64
	cmd := &cobra.Command{
		Use:   "price",
		Short: "Change a product's price and signal a price change",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if storeID == "" || sku == "" {
				return fmt.Errorf("--store and --sku are required")
			}
			ctx := context.Background()
			d, err := newDeps(ctx)
			if err != nil {
				return err
			}
			p, err := d.cli.MutatePrice(ctx, storeID, sku, int64(to*100))
			if err != nil {
				return err
			}
			fmt.Printf("store %s sku %s: price -> %.2f (signal sent)\n", storeID, sku, float64(p.PriceMinor)/100)
			return nil
		},
	}
	cmd.Flags().StringVar(&storeID, "store", "", "store_id (required)")
	cmd.Flags().StringVar(&sku, "sku", "", "product sku (required)")
	cmd.Flags().Float64Var(&to, "to", 0, "new price, e.g. 19.99 (0 = missing price, blocks checkout)")
	return cmd
}

func newMutateStockCmd() *cobra.Command {
	var storeID, sku string
	var to int
	cmd := &cobra.Command{
		Use:   "stock",
		Short: "Change a product's stock quantity and signal an inventory change",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if storeID == "" || sku == "" {
				return fmt.Errorf("--store and --sku are required")
			}
			ctx := context.Background()
			d, err := newDeps(ctx)
			if err != nil {
				return err
			}
			p, err := d.cli.MutateStock(ctx, storeID, sku, to)
			if err != nil {
				return err
			}
			fmt.Printf("store %s sku %s: stock -> %d (in_stock=%v, signal sent)\n", storeID, sku, to, p.InStock)
			return nil
		},
	}
	cmd.Flags().StringVar(&storeID, "store", "", "store_id (required)")
	cmd.Flags().StringVar(&sku, "sku", "", "product sku (required)")
	cmd.Flags().IntVar(&to, "to", 0, "new stock quantity (0 = out of stock)")
	return cmd
}

func newMutateAttrCmd() *cobra.Command {
	var storeID, sku, breakField string
	cmd := &cobra.Command{
		Use:   "attr",
		Short: "Break a product attribute to a rubric-visible bad value and signal a product change",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if storeID == "" || sku == "" || breakField == "" {
				return fmt.Errorf("--store, --sku, and --break are required")
			}
			ctx := context.Background()
			d, err := newDeps(ctx)
			if err != nil {
				return err
			}
			if _, err := d.cli.MutateAttr(ctx, storeID, sku, breakField); err != nil {
				return err
			}
			fmt.Printf("store %s sku %s: broke %s (signal sent)\n", storeID, sku, breakField)
			return nil
		},
	}
	cmd.Flags().StringVar(&storeID, "store", "", "store_id (required)")
	cmd.Flags().StringVar(&sku, "sku", "", "product sku (required)")
	cmd.Flags().StringVar(&breakField, "break", "", "field to break: title, price, or currency")
	return cmd
}
