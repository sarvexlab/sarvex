// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/sarvexlab/sarvex/internal/planes/sync/sim"
)

func newServeCmd() *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the persistent simulated-store REST server for every seeded simulated store",
		Long: "Serves every registered platform's store REST API surface the store-API connectors call " +
			"(products list/get/search/sku/paging, product PUT for remediation write-back, checkout), " +
			"multi-tenant by store_id, backed by the same persistent Mongo state `sim seed/mutate/fix` " +
			"write to. Also serves the plugin's profile doorway at " +
			"/stores/{store_id}/.well-known/ucp — point a paired store's storefront_url config at " +
			"this server's base URL + /stores/{store_id} to exercise the real profile-edge monitoring " +
			"check locally. Keep this running whenever a connector might pull from a simulated store — " +
			"restarting it does not lose any store's catalog (though the profile doorway's " +
			"last-known-good cache is in-memory and does not survive a restart).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := context.Background()
			d, err := newDeps(ctx)
			if err != nil {
				return err
			}

			srv := sim.NewServer(d.stores, d.simStoreBaseURL, d.connectorRepo, d.secretMgmt, d.signer, d.log)
			httpSrv := &http.Server{Addr: addr, Handler: srv.Handler()}

			errCh := make(chan error, 1)
			go func() { errCh <- httpSrv.ListenAndServe() }()
			d.log.Info("sim store server listening", "addr", addr, "base_url", d.simStoreBaseURL)

			quit := make(chan os.Signal, 1)
			signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

			select {
			case err := <-errCh:
				if err != nil && !errors.Is(err, http.ErrServerClosed) {
					return err
				}
				return nil
			case <-quit:
				d.log.Info("sim store server shutting down")
				return httpSrv.Shutdown(context.Background())
			}
		},
	}
	cmd.Flags().StringVar(&addr, "addr", ":4106", "address to listen on")
	return cmd
}
