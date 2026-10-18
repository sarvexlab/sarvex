// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package onboard drives a merchant from nothing to a live UCP store.
//
// The whole point is that a caller supplies one thing — the merchant's name
// — and everything else is derived (package synth) or read back from the
// control plane. Every step below is an ordinary call to an endpoint the
// console itself uses; nothing here reaches into a database or shortcuts a
// lifecycle.
//
// Two independent lifecycles run in parallel and both must land:
//
//	activation: draft → connector_linked → profile_published → verified → active
//	journey:    prospect → connected → synced → ready → live
//
// Activation is the store's own UCP readiness; journey is the merchant's
// engagement stage. Neither derives from the other, so Run waits on both.
package onboard

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/sarvexlab/sarvex/internal/platform"

	"github.com/sarvexlab/sarvex/cmd/sarvex/internal/client"
	"github.com/sarvexlab/sarvex/cmd/sarvex/internal/synth"
	pkgdomain "github.com/sarvexlab/sarvex/internal/pkg/domain"
	"github.com/sarvexlab/sarvex/internal/pkg/simstore"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/paymenthandler"
)

// CatalogSeeder writes a starting catalog into a store's simulated backend.
//
// This must happen before pairing: the first import runs as soon as pairing
// completes, and products added afterwards do not retroactively appear in
// an import that already ran (scripts/dev/up.sh says the same thing to
// anyone doing this by hand).
//
// An interface rather than a concrete sim dependency so that a caller
// driving a real merchant backend — where Sarvex does not own the catalog —
// can pass nil and skip the step.
type CatalogSeeder interface {
	Seed(ctx context.Context, storeID string, products int, messy bool) error
}

// Options configure a run. Only Name is required.
type Options struct {
	// Name is the merchant's display name — the one argument a user types.
	Name string
	// Seed overrides the fixture seed derived from Name. Pass the seed a
	// previous run reported to rebuild that exact fixture.
	Seed int64
	// Stores is how many stores to create for the merchant (default 1).
	Stores int
	// Platform is a supported platform (internal/platform); the control plane
	// rejects others. Empty means platform.Default().
	Platform string
	// Products is how many catalog items to seed per store.
	Products int
	// Messy seeds deliberate readiness problems, so the run exercises the
	// remediation path rather than only the happy one.
	Messy bool
	// SimStoreBaseURL is where the simulated store backend serves, used to
	// derive each store's storefront URL. Defaults to simstore.DefaultBaseURL.
	SimStoreBaseURL string
	// Timeout bounds the wait for each lifecycle to settle.
	Timeout time.Duration
	// PollInterval is how often lifecycle state is re-read.
	PollInterval time.Duration
	// Seeder writes the starting catalog. Nil skips seeding.
	Seeder CatalogSeeder
	// Progress receives human-readable step lines. Nil discards them.
	Progress io.Writer

	// deadline is when the whole run gives up, set once by start().
	//
	// Timeout is a budget for the run, not for each phase. It used to be
	// the latter, because poll computed its own deadline on every call:
	// three sequential waits (connector, verification, journey) each took
	// the full Timeout, so --timeout 4m permitted twelve minutes and
	// whatever bounded the caller's context cancelled the run mid-report
	// instead of letting it say where the store actually stopped.
	deadline time.Time
}

// start applies defaults and opens the run's single deadline.
func (o *Options) start() {
	o.applyDefaults()
	o.deadline = time.Now().Add(o.Timeout)
}

func (o *Options) applyDefaults() {
	if o.Stores <= 0 {
		o.Stores = 1
	}
	if o.Platform == "" {
		o.Platform = string(platform.Default())
	}
	if o.Products <= 0 {
		o.Products = 12
	}
	if o.SimStoreBaseURL == "" {
		o.SimStoreBaseURL = simstore.DefaultBaseURL
	}
	if o.Timeout <= 0 {
		o.Timeout = 3 * time.Minute
	}
	if o.PollInterval <= 0 {
		o.PollInterval = 2 * time.Second
	}
	if o.Progress == nil {
		o.Progress = io.Discard
	}
}

// Result is what a run produced, whether or not every store went live.
type Result struct {
	MerchantID string        `json:"merchant_id"`
	Name       string        `json:"name"`
	Seed       int64         `json:"seed"`
	Stores     []StoreResult `json:"stores"`
}

// StoreResult is one store's final position in both lifecycles.
type StoreResult struct {
	StoreID       string `json:"store_id"`
	DisplayName   string `json:"display_name"`
	Platform      string `json:"platform"`
	StorefrontURL string `json:"storefront_url"`
	UCPState      string `json:"ucp_state"`
	JourneyState  string `json:"journey_state"`
	// Blocking names what is stopping the store, when it did not reach
	// active — read from the activation step the store is working toward.
	Blocking string `json:"blocking,omitempty"`
}

// Live reports whether the store completed both lifecycles.
func (s StoreResult) Live() bool {
	return s.UCPState == StateActive && s.JourneyState == JourneyLive
}

// Terminal states this package waits for. Mirrors
// internal/planes/control/domain/{activation,journey}.go — duplicated
// deliberately, since this is an HTTP client and must not import a plane.
const (
	StateActive = "active"
	JourneyLive = "live"
)

// AllLive reports whether every store in the result went live.
func (r Result) AllLive() bool {
	for _, s := range r.Stores {
		if !s.Live() {
			return false
		}
	}
	return len(r.Stores) > 0
}

// Run creates a merchant and drives each of its stores to active/live.
//
// It returns a Result even when stores fall short, so the caller can report
// exactly where each one stopped; err is reserved for failures that make
// the run's outcome unknown, not for a store that simply did not finish.
func Run(ctx context.Context, operator *client.Client, opts Options) (*Result, error) {
	opts.start()
	if opts.Name == "" {
		return nil, errors.New("merchant name is required")
	}

	gen := synth.New(opts.Name, opts.Seed)
	fixture := gen.Merchant(opts.Name)

	progressf(opts, "seed %d (pass --seed %d to rebuild this exact fixture)\n", gen.Seed(), gen.Seed())

	// 1. Create the merchant. This is the only operator-scoped call in the
	//    run: merchant creation is staff-only, and the merchant_id it
	//    returns is what every later call is scoped to.
	account, err := operator.CreateMerchant(ctx, opts.Name, fixture.OwnerEmail, client.BusinessFacts{
		LegalEntityName:    fixture.LegalEntityName,
		SupportEmail:       fixture.SupportEmail,
		SupportPhone:       fixture.SupportPhone,
		FulfillmentRegions: fixture.FulfillmentRegions,
	})
	if err != nil {
		return nil, fmt.Errorf("create merchant: %w", err)
	}
	progressf(opts, "merchant %s created (%s)\n", account.MerchantID, account.Name)

	// 2. Re-scope to the merchant's own principal. Everything after this
	//    is a call the merchant could make themselves from the console.
	merchant, err := operator.AsMerchant(account.MerchantID)
	if err != nil {
		return nil, fmt.Errorf("scope client to merchant: %w", err)
	}

	result := &Result{MerchantID: account.MerchantID, Name: account.Name, Seed: gen.Seed()}
	for i := range opts.Stores {
		sr, err := runStore(ctx, merchant, gen, fixture, i, opts)
		if err != nil {
			return result, err
		}
		result.Stores = append(result.Stores, *sr)
	}
	return result, nil
}

// RunStore adds one store to an existing merchant and drives it to live.
// The store's fixture is derived from its own name, so adding a store later
// produces the same thing as having asked for it up front.
func RunStore(ctx context.Context, merchant *client.Client, merchantID string, opts Options) (*Result, error) {
	opts.start()
	if opts.Name == "" {
		return nil, errors.New("store name is required")
	}

	gen := synth.New(opts.Name, opts.Seed)
	fixture := gen.Merchant(opts.Name)
	progressf(opts, "seed %d (pass --seed %d to rebuild this exact fixture)\n", gen.Seed(), gen.Seed())

	result := &Result{MerchantID: merchantID, Name: opts.Name, Seed: gen.Seed()}
	sr, err := runStore(ctx, merchant, gen, fixture, 0, opts)
	if err != nil {
		return result, err
	}
	result.Stores = append(result.Stores, *sr)
	return result, nil
}

// Status reports both lifecycles for every store a merchant owns, without
// changing anything.
func Status(ctx context.Context, merchant *client.Client, merchantID string) (*Result, error) {
	stores, err := merchant.ListStores(ctx)
	if err != nil {
		return nil, fmt.Errorf("list stores: %w", err)
	}

	result := &Result{MerchantID: merchantID}
	for _, s := range stores {
		sr := StoreResult{StoreID: s.StoreID, DisplayName: s.DisplayName, Platform: s.Platform}
		fillFinalState(ctx, merchant, &sr, Options{})
		result.Stores = append(result.Stores, sr)
	}
	return result, nil
}

func runStore(ctx context.Context, merchant *client.Client, gen *synth.Generator, fixture synth.Merchant, index int, opts Options) (*StoreResult, error) {
	sf := gen.Store(fixture, index, opts.Platform)

	store, err := merchant.CreateStore(ctx, sf.DisplayName, sf.Platform)
	if err != nil {
		return nil, fmt.Errorf("create store %q: %w", sf.DisplayName, err)
	}
	progressf(opts, "  store %s created (%s)\n", store.StoreID, sf.DisplayName)

	// The storefront URL must point at wherever this store's backend
	// actually serves, because the profile-edge probe fetches
	// /.well-known/ucp from it during verification. For a simulated store
	// that is the sim server's per-store path.
	storefrontURL := simstore.StoreBaseURL(opts.SimStoreBaseURL, store.StoreID)

	if err := merchant.SetStoreProfile(ctx, store.StoreID, client.StoreProfile{
		LegalName:          sf.LegalName,
		SupportEmail:       sf.SupportEmail,
		Domain:             storefrontURL,
		ReturnPolicyURL:    sf.ReturnPolicyURL,
		FulfillmentRegions: sf.Regions,
	}); err != nil {
		return nil, fmt.Errorf("configure store %s: %w", store.StoreID, err)
	}

	handlers, err := defaultHandlers(sf.DisplayName, storefrontURL)
	if err != nil {
		return nil, err
	}
	if err := merchant.SetPaymentConfig(ctx, store.StoreID, handlers); err != nil {
		return nil, fmt.Errorf("configure payments for store %s: %w", store.StoreID, err)
	}
	progressf(opts, "  store %s configured\n", store.StoreID)

	// Seed before pairing — see CatalogSeeder's doc comment for why the
	// order matters.
	if opts.Seeder != nil {
		if err := opts.Seeder.Seed(ctx, store.StoreID, opts.Products, opts.Messy); err != nil {
			return nil, fmt.Errorf("seed catalog for store %s: %w", store.StoreID, err)
		}
		progressf(opts, "  store %s seeded with %d products (messy=%v)\n", store.StoreID, opts.Products, opts.Messy)
	}

	if _, err := merchant.BeginPairing(ctx, store.StoreID, sf.Platform); err != nil {
		return nil, fmt.Errorf("begin pairing for store %s: %w", store.StoreID, err)
	}
	progressf(opts, "  store %s pairing started\n", store.StoreID)

	sr := &StoreResult{
		StoreID:       store.StoreID,
		DisplayName:   sf.DisplayName,
		Platform:      sf.Platform,
		StorefrontURL: storefrontURL,
	}

	// Pairing completes and the first import runs asynchronously; the
	// store cannot verify until its catalog is in. Wait for the connector
	// to land before asking for verification, so a slow import reads as
	// "waiting" rather than as a verification failure.
	if err := waitForConnectorLinked(ctx, merchant, store.StoreID, opts); err != nil {
		// A store whose connector never links is a reportable outcome, not a
		// run failure — the caller prints where it stopped. Surface the
		// reason too: without it a timeout here was indistinguishable from a
		// store that simply had nothing to import.
		progressf(opts, "  store %s connector never linked: %v\n", store.StoreID, err)
		fillFinalState(ctx, merchant, sr, opts)
		return sr, nil
	}
	progressf(opts, "  store %s connector linked\n", store.StoreID)

	if err := merchant.TriggerAudit(ctx, store.StoreID); err != nil {
		return nil, fmt.Errorf("trigger catalog audit for store %s: %w", store.StoreID, err)
	}

	if err := resolveOpenTasks(ctx, merchant, store.StoreID, opts); err != nil {
		return nil, fmt.Errorf("resolve tasks for store %s: %w", store.StoreID, err)
	}

	// Verification is what advances draft → verified → active. It runs the
	// store-level probe suite (profile reachable, signature roundtrip,
	// negotiation handshake, catalog lookup) and applies the outcome.
	if err := driveVerification(ctx, merchant, store.StoreID, opts); err != nil {
		return nil, fmt.Errorf("verify store %s: %w", store.StoreID, err)
	}

	waitForLive(ctx, merchant, store.StoreID, opts)
	fillFinalState(ctx, merchant, sr, opts)
	return sr, nil
}

// defaultHandlers enables Google Pay for the store, in Google's TEST
// environment against the example gateway — this CLI onboards simulated stores,
// which never move real money.
//
// The handler id comes from paymenthandler's spec table, the same table the
// control plane validates against, so the two sides cannot disagree, and the
// parameters are exactly the handler's declared public ones. Enabling a handler
// without its required parameters is rejected by the API (an agent could not
// use it). Nothing here may carry a credential (CLAUDE.md §2.8).
func defaultHandlers(displayName, storefrontURL string) ([]client.EnabledHandler, error) {
	spec, ok := paymenthandler.Lookup(string(pkgdomain.GooglePay))
	if !ok {
		return nil, errors.New("no payment handler spec for Google Pay")
	}
	return []client.EnabledHandler{{
		HandlerID: spec.ID,
		Enabled:   true,
		Parameters: map[string]string{
			"merchant_name":       displayName,
			"merchant_id":         "sim-" + spec.ID,
			"merchant_origin":     storefrontURL,
			"gateway":             "example",
			"gateway_merchant_id": "exampleGatewayMerchantId",
			"environment":         "TEST",
		},
	}}, nil
}

func waitForConnectorLinked(ctx context.Context, c *client.Client, storeID string, opts Options) error {
	return poll(ctx, opts, func() (bool, error) {
		a, err := c.GetActivation(ctx, storeID)
		if err != nil {
			// The activation record is not reliably readable while pairing
			// is still in flight. That is "not yet", not a failure, so keep
			// polling until the deadline decides.
			return false, nil //nolint:nilerr // a transient read is a retry, not an error
		}
		return a.UCPState != "draft", nil
	})
}

// resolveOpenTasks clears the remediation tasks the audit raised. A --messy
// seed deliberately produces some, which is the point: the run should
// exercise the remediation path, not route around it.
func resolveOpenTasks(ctx context.Context, c *client.Client, storeID string, opts Options) error {
	tasks, err := c.ListTasks(ctx)
	if err != nil {
		return err
	}
	resolved := 0
	for _, t := range tasks {
		if t.StoreID != storeID || t.Status == "resolved" {
			continue
		}
		if err := c.ResolveTask(ctx, t.TaskID); err != nil {
			// A task that cannot be resolved from here is not fatal: the
			// verification step below is the real gate, and it will report
			// precisely what is still blocking.
			progressf(opts, "  task %s could not be resolved: %v\n", t.TaskID, err)
			continue
		}
		resolved++
	}
	if resolved > 0 {
		progressf(opts, "  store %s resolved %d task(s)\n", storeID, resolved)
	}
	return nil
}

// driveVerification asks for verification until the store reaches active or
// the timeout expires. Repeats rather than asking once because the earlier
// states clear asynchronously — a first run can legitimately report "not
// yet" while the profile is still being published.
func driveVerification(ctx context.Context, c *client.Client, storeID string, opts Options) error {
	var lastDetail string
	err := poll(ctx, opts, func() (bool, error) {
		report, err := c.VerifyActivation(ctx, storeID)
		if err != nil {
			return false, err
		}
		if report.Passed && report.State == StateActive {
			return true, nil
		}
		lastDetail = firstFailure(report)
		return false, nil
	})
	if err != nil && lastDetail != "" {
		progressf(opts, "  store %s not verified: %s\n", storeID, lastDetail)
	}
	// A store that will not verify is a reportable outcome, not a run
	// failure — the caller prints where it stopped.
	return nil
}

func firstFailure(report *client.VerificationReport) string {
	for _, c := range report.Checks {
		if !c.Passed {
			if c.Detail != "" {
				return fmt.Sprintf("%s: %s", c.CheckID, c.Detail)
			}
			return c.CheckID
		}
	}
	return ""
}

func waitForLive(ctx context.Context, c *client.Client, storeID string, opts Options) {
	_ = poll(ctx, opts, func() (bool, error) {
		j, err := c.GetJourney(ctx, storeID)
		if err != nil {
			// Same as waitForConnectorLinked: a read that fails while the
			// journey is still settling is a retry, not a failure.
			return false, nil //nolint:nilerr // a transient read is a retry, not an error
		}
		return j.State == JourneyLive, nil
	})
}

// fillFinalState reads both lifecycles one last time so the result reports
// where the store actually ended up, not where the last poll left it.
func fillFinalState(ctx context.Context, c *client.Client, sr *StoreResult, opts Options) {
	if a, err := c.GetActivation(ctx, sr.StoreID); err == nil {
		sr.UCPState = a.UCPState
		sr.Blocking = blockingStep(a)
	}
	if j, err := c.GetJourney(ctx, sr.StoreID); err == nil {
		sr.JourneyState = j.State
	}
	_ = opts
}

// blockingStep returns the reason the store has not cleared the step it is
// currently working toward. Only the in-progress step carries one.
func blockingStep(a *client.Activation) string {
	for _, s := range a.Steps {
		if !s.Reached && s.Blocking != "" {
			return s.Blocking
		}
	}
	return ""
}

// poll runs fn until it reports done, the context ends, or the timeout
// expires. A returned error from fn aborts immediately; "not yet" is
// (false, nil).
func poll(ctx context.Context, opts Options, fn func() (bool, error)) error {
	deadline := opts.deadline
	if deadline.IsZero() {
		// A caller that built Options by hand rather than through start()
		// still gets a bounded wait.
		deadline = time.Now().Add(opts.Timeout)
	}
	ticker := time.NewTicker(opts.PollInterval)
	defer ticker.Stop()

	for {
		done, err := fn()
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("ran out of the %s budget for the whole onboarding run", opts.Timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func progressf(opts Options, format string, args ...any) {
	_, _ = fmt.Fprintf(opts.Progress, format, args...)
}
