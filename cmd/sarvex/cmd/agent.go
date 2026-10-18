// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	ucpagent "github.com/sarvexlab/sarvex/internal/pkg/ucp/agent"
)

// newAgentCmd builds the agent half of the tree: thin wrappers over
// internal/pkg/ucp/agent, which already speaks the whole serving-plane
// surface. These commands parse arguments and format output; the protocol
// lives in the package.
func newAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Act as a UCP commerce agent against a live store",
		Long: "Drives a store's serving plane the way a real UCP shopping agent does:\n" +
			"discovery, catalog search and lookup, and the checkout create/update/complete\n" +
			"lifecycle — then reads back what Sarvex recorded for the resulting order.\n\n" +
			"Use `conform run` to drive all of it at once with assertions.",
	}
	cmd.AddCommand(newAgentProfileCmd(), newAgentCatalogCmd(), newAgentCheckoutCmd(), newAgentOrderCmd())
	return cmd
}

// newAgent builds an agent for commands that never track an order, so they
// need no database connection at all — profile, catalog and checkout are
// pure HTTP against the serving plane.
func newAgent() *ucpagent.Agent {
	return ucpagent.NewAgent(global.servingURL, global.controlURL, nil)
}

// newAgentWithMerchantLookup builds an agent that can also resolve a store
// to its merchant, which order tracking needs to mint a scoped token.
func newAgentWithMerchantLookup(ctx context.Context) (*ucpagent.Agent, func(), error) {
	deps, err := newSimDeps(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("order tracking needs the store→merchant lookup: %w", err)
	}
	a := ucpagent.NewAgent(global.servingURL, global.controlURL,
		ucpagent.MerchantResolverFunc(func(ctx context.Context, storeID string) (string, error) {
			model, err := deps.merchants.GetByStoreID(ctx, storeID)
			if err != nil {
				return "", fmt.Errorf("no connector paired for store %s: %w", storeID, err)
			}
			return model.MerchantID, nil
		}))
	return a, deps.close, nil
}

func storeFlag(cmd *cobra.Command, target *string) {
	cmd.Flags().StringVar(target, "store", "", "Store to act against (required)")
}

func requireStore(storeID string) error {
	if storeID == "" {
		return fmt.Errorf("--store is required")
	}
	return nil
}

func newAgentProfileCmd() *cobra.Command {
	var storeID string
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Fetch a store's UCP profile from /.well-known/ucp",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireStore(storeID); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), global.timeout)
			defer cancel()

			profile, err := newAgent().FetchProfile(ctx, storeID)
			if err != nil {
				return err
			}
			if global.output == outputJSON {
				return emitJSON(os.Stdout, profile.Raw)
			}
			fmt.Printf("payment handlers: %v\n", profile.PaymentHandlers)
			return emitJSON(os.Stdout, profile.Raw)
		},
	}
	storeFlag(cmd, &storeID)
	return cmd
}

func newAgentCatalogCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "catalog", Short: "Browse a store's live catalog"}

	var searchStore, query, cursor string
	var limit int
	search := &cobra.Command{
		Use:   "search",
		Short: "POST /catalog/search",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireStore(searchStore); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), global.timeout)
			defer cancel()

			result, err := newAgent().SearchCatalog(ctx, searchStore, query, limit, cursor)
			if err != nil {
				return err
			}
			return emitJSON(os.Stdout, result)
		},
	}
	storeFlag(search, &searchStore)
	search.Flags().StringVar(&query, "query", "", "Free-text search query")
	search.Flags().IntVar(&limit, "limit", 0, "Page size (0 lets the server choose)")
	search.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor from a previous page")

	var lookupStore, itemID string
	lookup := &cobra.Command{
		Use:   "lookup",
		Short: "GET /catalog/{itemId}",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireStore(lookupStore); err != nil {
				return err
			}
			if itemID == "" {
				return fmt.Errorf("--item is required")
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), global.timeout)
			defer cancel()

			item, err := newAgent().LookupCatalogItem(ctx, lookupStore, itemID)
			if err != nil {
				return err
			}
			return emitJSON(os.Stdout, item)
		},
	}
	storeFlag(lookup, &lookupStore)
	lookup.Flags().StringVar(&itemID, "item", "", "Catalog item id / SKU (required)")

	cmd.AddCommand(search, lookup)
	return cmd
}

func newAgentCheckoutCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "checkout", Short: "Drive a checkout session against a store"}

	var storeID, sku, handlerID, email string
	var qty int
	run := &cobra.Command{
		Use:   "run",
		Short: "Create, update and complete a checkout in one shot",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireStore(storeID); err != nil {
				return err
			}
			if sku == "" {
				return fmt.Errorf("--sku is required")
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), global.timeout)
			defer cancel()

			a := newAgent()

			// The handler id has to come from the store's own profile:
			// completing with one the store does not advertise is a
			// protocol error, not a configuration choice this tool makes.
			if handlerID == "" {
				profile, err := a.FetchProfile(ctx, storeID)
				if err != nil {
					return fmt.Errorf("resolve payment handler: %w", err)
				}
				if len(profile.PaymentHandlers) == 0 {
					return fmt.Errorf("store %s advertises no payment handlers; pass --handler to override", storeID)
				}
				handlerID = profile.PaymentHandlers[0]
			}

			created, err := a.CreateCheckout(ctx, storeID, []ucpagent.LineItemInput{{SKU: sku, Quantity: qty}})
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(progressWriter(), "checkout %s created\n", created.ID)

			if _, err := a.UpdateCheckout(ctx, storeID, created.ID,
				&ucpagent.BuyerInput{Email: email, FirstName: "Jane", LastName: "Doe"},
				&ucpagent.AddressInput{
					FirstName: "Jane", LastName: "Doe", Street: "1 Main St",
					City: "Springfield", Region: "IL", PostalCode: "62701", CountryCode: "US",
				}); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(progressWriter(), "checkout %s updated\n", created.ID)

			completed, err := a.CompleteCheckout(ctx, storeID, created.ID, handlerID)
			if err != nil {
				return err
			}
			return emitJSON(os.Stdout, completed)
		},
	}
	storeFlag(run, &storeID)
	run.Flags().StringVar(&sku, "sku", "", "Catalog item to buy (required)")
	run.Flags().IntVar(&qty, "qty", 1, "Quantity")
	run.Flags().StringVar(&handlerID, "handler", "", "Payment handler id (defaults to the store's first advertised handler)")
	run.Flags().StringVar(&email, "email", "buyer@example.com", "Buyer email")

	cmd.AddCommand(run)
	return cmd
}

func newAgentOrderCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "order", Short: "Read back an order Sarvex recorded"}

	var storeID, orderID string
	track := &cobra.Command{
		Use:   "track",
		Short: "GET /stores/{id}/orders/{order_id} — Sarvex's own record of a completed checkout",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireStore(storeID); err != nil {
				return err
			}
			if orderID == "" {
				return fmt.Errorf("--order is required")
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), global.timeout)
			defer cancel()

			a, closeDeps, err := newAgentWithMerchantLookup(ctx)
			if err != nil {
				return err
			}
			defer closeDeps()

			order, err := a.TrackOrder(ctx, storeID, orderID)
			if err != nil {
				return err
			}
			return emitJSON(os.Stdout, order)
		},
	}
	storeFlag(track, &storeID)
	track.Flags().StringVar(&orderID, "order", "", "Order id (required)")

	cmd.AddCommand(track)
	return cmd
}
