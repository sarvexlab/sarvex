// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"os"

	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	var storeID string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show what a simulated store currently holds, and its pairing state (all stores if --store omitted)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := context.Background()
			d, err := newDeps(ctx)
			if err != nil {
				return err
			}

			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")

			if storeID == "" {
				summaries, err := d.cli.StatusAll(ctx)
				if err != nil {
					return err
				}
				return enc.Encode(summaries)
			}

			report, err := d.cli.Status(ctx, storeID)
			if err != nil {
				return err
			}
			return enc.Encode(report)
		},
	}
	cmd.Flags().StringVar(&storeID, "store", "", "store_id (omit to list every simulated store)")
	return cmd
}
