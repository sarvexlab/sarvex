// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func newOrderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "order",
		Short: "Track an order Sarvex recorded from a completed checkout",
	}
	cmd.AddCommand(newOrderTrackCmd())
	return cmd
}

func newOrderTrackCmd() *cobra.Command {
	var storeID, orderID string
	cmd := &cobra.Command{
		Use:   "track",
		Short: "GET the control plane's record of an order (id, status, sync_state, value, placed_at)",
		Long: "Reads back what Sarvex recorded for a completed checkout, via the same " +
			"GET /stores/{id}/orders/{order_id} the console uses — this is Sarvex's own record, " +
			"not a UCP-agent-facing endpoint (V1 has no order-read capability at the serving " +
			"plane; order status-sync back toward Google is a documented gap, architecture G2), " +
			"so 'tracking' means the order stays at whatever status/sync_state checkout completion " +
			"recorded rather than progressing through shipped/delivered on its own.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if storeID == "" || orderID == "" {
				return fmt.Errorf("--store and --order are required")
			}
			ctx := context.Background()
			d, err := newDeps(ctx)
			if err != nil {
				return err
			}
			order, err := d.agent.TrackOrder(ctx, storeID, orderID)
			if err != nil {
				return err
			}
			fmt.Printf("order %s: status=%s sync_state=%s value=%d %s placed_at=%s\n",
				order.OrderID, order.Status, order.SyncState, order.Value.AmountMinor, order.Value.Currency, order.PlacedAt.Format("2006-01-02T15:04:05Z07:00"))
			return printJSON(order)
		},
	}
	cmd.Flags().StringVar(&storeID, "store", "", "store_id (required)")
	cmd.Flags().StringVar(&orderID, "order", "", "order id, as returned by sim checkout complete/run (required)")
	return cmd
}
