// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func newPickupCmd() *cobra.Command {
	var storeID string
	var off bool
	cmd := &cobra.Command{
		Use:   "pickup",
		Short: "Turn a simulated store's Local Pickup shipping method on (or --off)",
		Long: "Enables in-store pickup on the simulated store, on every platform's surface. The next " +
			"sync-plane sweep (or first import) detects it and the store's profile starts " +
			"advertising the pickup extension; --off reverses it.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if storeID == "" {
				return fmt.Errorf("--store is required")
			}
			ctx := context.Background()
			d, err := newDeps(ctx)
			if err != nil {
				return err
			}
			if err := d.cli.SetLocalPickup(ctx, storeID, !off); err != nil {
				return err
			}
			fmt.Printf("store %s: local pickup %s\n", storeID, map[bool]string{true: "enabled", false: "disabled"}[!off])
			return nil
		},
	}
	cmd.Flags().StringVar(&storeID, "store", "", "store_id (required)")
	cmd.Flags().BoolVar(&off, "off", false, "disable pickup instead of enabling it")
	return cmd
}
