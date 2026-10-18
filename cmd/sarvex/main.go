// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Command sarvex drives Sarvex from the outside: it onboards merchants
// through the control plane's own API, and plays the UCP shopping-agent
// role against the stores that result.
//
// It has two halves, and they are two halves of one thing — a store is only
// really onboarded if an agent can transact against it, and an agent has
// nothing to transact against without a store.
//
//	# Onboard: one argument, everything else derived from it.
//	sarvex onboard merchant "Acme Widgets"
//	sarvex onboard merchant "Acme Widgets" --stores 2 --messy
//	sarvex onboard store "Acme Outlet" --merchant <id>
//	sarvex onboard status --merchant <id>
//
//	# Agent: play the role Google's checkout surface plays in production.
//	sarvex agent profile  --store <id>
//	sarvex agent catalog search --store <id> --query lantern
//	sarvex agent checkout run --store <id> --sku <sku>
//	sarvex agent order track --store <id> --order <id>
//
//	# Conform: the agent half as an assertion suite, failure paths included.
//	sarvex conform run --target local
//	sarvex conform run --in-process
//	sarvex conform run --target local -o junit > results.xml
//
//	# Conform serve: onboard, then hand the store to the official UCP
//	# conformance suite (github.com/Universal-Commerce-Protocol/conformance)
//	# instead of Sarvex's own assertions — see scripts/ucp-conformance/run.sh.
//	sarvex conform serve --in-process --output-dir /tmp/ucp-fixtures --ready-file /tmp/ucp-fixtures/ready
//
// Unlike cmd/sim — which simulates a *merchant's* store for Sarvex to
// consume from — everything here drives Sarvex's own surfaces from the
// outside, over HTTP, exactly as the console and a real agent would. It
// never writes a collection directly and never becomes a second writer.
//
// Configuration is by flag, falling back to the environment variables
// scripts/dev/up.sh exports, so a stack started with `make dev-up` needs no
// flags:
//
//	CONTROL_PLANE_BASE_URL   (default http://localhost:8081/v1)
//	SERVING_BASE_URL         (default http://localhost:8083/ucp)
//	SYNC_PLANE_BASE_URL      (default http://localhost:8082/v1)
//	SIM_STORE_BASE_URL       (default http://localhost:4106)
//	MONGO_CONNECTION_STRING  (default mongodb://admin:password@localhost:27017/?directConnection=true)
//	REDIS_ADDRESS            (default localhost:6379)
//	REDIS_PASSWORD           (default "")
//	VAULT_ADDR               (default http://localhost:8200)
//	VAULT_TOKEN              (default the docker-compose dev token)
//	NATS_URL                 (default nats://localhost:4222)
//
// Authentication is a dev bearer token minted in-process, accepted only
// when the control plane runs with KINDE_ISSUER_URL unset. This tool is for
// development and CI; it cannot authenticate against a real deployment.
package main

import (
	"fmt"
	"os"

	"github.com/sarvexlab/sarvex/cmd/sarvex/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "sarvex:", err)
		os.Exit(1)
	}
}
