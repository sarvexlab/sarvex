// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func newCatalogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "catalog",
		Short: "Browse a store's live catalog via the serving plane, playing the agent role",
		Long: "Runs the same read-only discovery calls a real UCP shopping agent runs before " +
			"building a cart — POST /catalog/search and GET /catalog/{itemId} against the serving " +
			"plane — so a store's catalog surface can be exercised end to end, the same way " +
			"`sim checkout` exercises checkout. Requires SERVING_BASE_URL to point at a running " +
			"serving plane (see `make dev-up`) and the target store to already be live.",
	}
	cmd.AddCommand(newCatalogSearchCmd(), newCatalogLookupCmd())
	return cmd
}

func newCatalogSearchCmd() *cobra.Command {
	var storeID, query, cursor string
	var limit int
	cmd := &cobra.Command{
		Use:   "search",
		Short: "POST /catalog/search: search a store's live catalog",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if storeID == "" {
				return fmt.Errorf("--store is required")
			}
			ctx := context.Background()
			d, err := newDeps(ctx)
			if err != nil {
				return err
			}
			result, err := d.agent.SearchCatalog(ctx, storeID, query, limit, cursor)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%d item(s), has_next_page=%v", len(result.Products), result.Pagination.HasNextPage)
			if result.Pagination.HasNextPage {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), " next_cursor=%s", result.Pagination.Cursor)
			}
			_, _ = fmt.Fprintln(cmd.ErrOrStderr())
			return printJSON(result)
		},
	}
	cmd.Flags().StringVar(&storeID, "store", "", "store_id (required)")
	cmd.Flags().StringVar(&query, "q", "", "search query (omit to list the catalog)")
	cmd.Flags().IntVar(&limit, "limit", 0, "page size (0 = serving plane default)")
	cmd.Flags().StringVar(&cursor, "cursor", "", "pagination cursor from a previous search's next_cursor")
	return cmd
}

func newCatalogLookupCmd() *cobra.Command {
	var storeID, itemID string
	cmd := &cobra.Command{
		Use:   "lookup",
		Short: "GET /catalog/{itemId}: look up a single catalog item by id",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if storeID == "" || itemID == "" {
				return fmt.Errorf("--store and --item are required")
			}
			ctx := context.Background()
			d, err := newDeps(ctx)
			if err != nil {
				return err
			}
			item, err := d.agent.LookupCatalogItem(ctx, storeID, itemID)
			if err != nil {
				return err
			}
			price := item.Price()
			fmt.Fprintf(cmd.ErrOrStderr(), "%s: %s (in_stock=%v, price=%d %s)\n",
				item.SKU(), item.Title, item.InStock(), price.Amount, price.Currency)
			return printJSON(item)
		},
	}
	cmd.Flags().StringVar(&storeID, "store", "", "store_id (required)")
	cmd.Flags().StringVar(&itemID, "item", "", "catalog item id / SKU (required)")
	return cmd
}
