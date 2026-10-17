// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	ucpagent "github.com/sarvexlab/sarvex/internal/pkg/ucp/agent"
)

func newCheckoutCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "checkout",
		Short: "Drive a checkout session against a store's live serving plane, playing the agent role",
		Long: "Runs the same create/update/complete lifecycle a real UCP shopping agent runs against " +
			"the serving plane (GET .well-known/ucp, POST/PUT/POST checkout-sessions), so a store's " +
			"live UCP surface can be exercised end to end without Google. Requires SERVING_BASE_URL to " +
			"point at a running serving plane (see `make dev-up`) and the target store to already be " +
			"paired and live — checkout calls the merchant backend live, same as a real agent's would.",
	}
	cmd.AddCommand(newCheckoutCreateCmd(), newCheckoutUpdateCmd(), newCheckoutCompleteCmd(), newCheckoutRunCmd())
	return cmd
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func newCheckoutCreateCmd() *cobra.Command {
	var storeID string
	var skus []string
	var qty int
	cmd := &cobra.Command{
		Use:   "create",
		Short: "POST /checkout-sessions: create a checkout session with the given line items",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if storeID == "" || len(skus) == 0 {
				return fmt.Errorf("--store and at least one --sku are required")
			}
			ctx := context.Background()
			d, err := newDeps(ctx)
			if err != nil {
				return err
			}
			resp, err := d.agent.CreateCheckout(ctx, storeID, lineItemInputs(skus, qty))
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "checkout %s created (status=%s)\n", resp.ID, resp.Status)
			return printJSON(resp)
		},
	}
	cmd.Flags().StringVar(&storeID, "store", "", "store_id (required)")
	cmd.Flags().StringArrayVar(&skus, "sku", nil, "product SKU to buy (repeatable, required)")
	cmd.Flags().IntVar(&qty, "qty", 1, "quantity per SKU")
	return cmd
}

func newCheckoutUpdateCmd() *cobra.Command {
	var storeID, checkoutID string
	var email, firstName, lastName, phone string
	var street, city, region, postal, country string
	cmd := &cobra.Command{
		Use:   "update",
		Short: "PUT /checkout-sessions/{id}: supply buyer + fulfillment address (full-replacement)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if storeID == "" || checkoutID == "" {
				return fmt.Errorf("--store and --checkout are required")
			}
			ctx := context.Background()
			d, err := newDeps(ctx)
			if err != nil {
				return err
			}
			var buyer *ucpagent.BuyerInput
			if email != "" || firstName != "" || lastName != "" || phone != "" {
				buyer = &ucpagent.BuyerInput{Email: email, FirstName: firstName, LastName: lastName, Phone: phone}
			}
			var address *ucpagent.AddressInput
			if street != "" || city != "" {
				address = &ucpagent.AddressInput{
					FirstName: firstName, LastName: lastName,
					Street: street, City: city, Region: region, PostalCode: postal, CountryCode: country,
				}
			}
			resp, err := d.agent.UpdateCheckout(ctx, storeID, checkoutID, buyer, address)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "checkout %s updated (status=%s)\n", resp.ID, resp.Status)
			return printJSON(resp)
		},
	}
	cmd.Flags().StringVar(&storeID, "store", "", "store_id (required)")
	cmd.Flags().StringVar(&checkoutID, "checkout", "", "checkout session id (required)")
	cmd.Flags().StringVar(&email, "email", "buyer@example.com", "buyer email")
	cmd.Flags().StringVar(&firstName, "first-name", "Jane", "buyer/shipping first name")
	cmd.Flags().StringVar(&lastName, "last-name", "Doe", "buyer/shipping last name")
	cmd.Flags().StringVar(&phone, "phone", "", "buyer phone")
	cmd.Flags().StringVar(&street, "street", "1 Main St", "shipping street address")
	cmd.Flags().StringVar(&city, "city", "Springfield", "shipping city")
	cmd.Flags().StringVar(&region, "region", "IL", "shipping state/region")
	cmd.Flags().StringVar(&postal, "postal", "62701", "shipping postal code")
	cmd.Flags().StringVar(&country, "country", "US", "shipping country code")
	return cmd
}

func newCheckoutCompleteCmd() *cobra.Command {
	var storeID, checkoutID, handlerID string
	cmd := &cobra.Command{
		Use:   "complete",
		Short: "POST /checkout-sessions/{id}/complete: submit payment and place the order",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if storeID == "" || checkoutID == "" {
				return fmt.Errorf("--store and --checkout are required")
			}
			ctx := context.Background()
			d, err := newDeps(ctx)
			if err != nil {
				return err
			}
			if handlerID == "" {
				handlerID, err = defaultHandler(ctx, d, storeID)
				if err != nil {
					return err
				}
			}
			resp, err := d.agent.CompleteCheckout(ctx, storeID, checkoutID, handlerID)
			if err != nil {
				return err
			}
			if resp.Order != nil {
				fmt.Fprintf(os.Stderr, "checkout %s complete: order %s (status=%s)\n", resp.SessionID, resp.Order.ID, resp.Order.Status)
			} else {
				fmt.Fprintf(os.Stderr, "checkout %s: state=%s status=%s\n", resp.SessionID, resp.State, resp.Status)
			}
			return printJSON(resp)
		},
	}
	cmd.Flags().StringVar(&storeID, "store", "", "store_id (required)")
	cmd.Flags().StringVar(&checkoutID, "checkout", "", "checkout session id (required)")
	cmd.Flags().StringVar(&handlerID, "handler", "", "payment handler_id (default: first one the store's profile advertises)")
	return cmd
}

func newCheckoutRunCmd() *cobra.Command {
	var storeID string
	var skus []string
	var qty int
	var email, city string
	var handlerID string
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Create, update, and complete a checkout in one shot, then print the resulting order",
		Long: "Runs the full checkout-to-completion flow against a store's live serving plane: " +
			"create with the given line items, update with a synthetic buyer + shipping address, " +
			"complete with a fabricated payment credential, then look up the resulting order via " +
			"the control plane. This is the one-command way to prove a store's checkout works end to end.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if storeID == "" || len(skus) == 0 {
				return fmt.Errorf("--store and at least one --sku are required")
			}
			ctx := context.Background()
			d, err := newDeps(ctx)
			if err != nil {
				return err
			}

			created, err := d.agent.CreateCheckout(ctx, storeID, lineItemInputs(skus, qty))
			if err != nil {
				return fmt.Errorf("create: %w", err)
			}
			fmt.Fprintf(os.Stderr, "1/3 created checkout %s (status=%s)\n", created.ID, created.Status)

			updated, err := d.agent.UpdateCheckout(ctx, storeID, created.ID,
				&ucpagent.BuyerInput{Email: email, FirstName: "Jane", LastName: "Doe"},
				&ucpagent.AddressInput{FirstName: "Jane", LastName: "Doe", Street: "1 Main St", City: city, Region: "IL", PostalCode: "62701", CountryCode: "US"},
			)
			if err != nil {
				return fmt.Errorf("update: %w", err)
			}
			fmt.Fprintf(os.Stderr, "2/3 updated checkout %s (status=%s)\n", updated.ID, updated.Status)

			if handlerID == "" {
				handlerID, err = defaultHandler(ctx, d, storeID)
				if err != nil {
					return fmt.Errorf("resolve payment handler: %w", err)
				}
			}
			completed, err := d.agent.CompleteCheckout(ctx, storeID, created.ID, handlerID)
			if err != nil {
				return fmt.Errorf("complete: %w", err)
			}
			if completed.Order == nil {
				fmt.Fprintf(os.Stderr, "3/3 checkout %s did not complete: state=%s status=%s\n", completed.SessionID, completed.State, completed.Status)
				return printJSON(completed)
			}
			fmt.Fprintf(os.Stderr, "3/3 completed: order %s (status=%s, total=%d %s)\n",
				completed.Order.ID, completed.Order.Status, completed.Order.Total, completed.Order.Currency)

			return printJSON(completed)
		},
	}
	cmd.Flags().StringVar(&storeID, "store", "", "store_id (required)")
	cmd.Flags().StringArrayVar(&skus, "sku", nil, "product SKU to buy (repeatable, required)")
	cmd.Flags().IntVar(&qty, "qty", 1, "quantity per SKU")
	cmd.Flags().StringVar(&email, "email", "sim-buyer@example.com", "buyer email")
	cmd.Flags().StringVar(&city, "city", "Springfield", "shipping city")
	cmd.Flags().StringVar(&handlerID, "handler", "", "payment handler_id (default: first one the store's profile advertises)")
	return cmd
}

func lineItemInputs(skus []string, qty int) []ucpagent.LineItemInput {
	if qty <= 0 {
		qty = 1
	}
	items := make([]ucpagent.LineItemInput, len(skus))
	for i, sku := range skus {
		items[i] = ucpagent.LineItemInput{SKU: sku, Quantity: qty}
	}
	return items
}

// defaultHandler fetches the store's profile and returns the first
// negotiated payment handler id, so `checkout complete`/`checkout run`
// don't require the caller to already know the store's handler ids.
func defaultHandler(ctx context.Context, d *deps, storeID string) (string, error) {
	profile, err := d.agent.FetchProfile(ctx, storeID)
	if err != nil {
		return "", fmt.Errorf("fetch profile to resolve a payment handler: %w", err)
	}
	if len(profile.PaymentHandlers) == 0 {
		return "", fmt.Errorf("store %s's profile advertises no payment handlers; pass --handler explicitly", storeID)
	}
	return profile.PaymentHandlers[0], nil
}
