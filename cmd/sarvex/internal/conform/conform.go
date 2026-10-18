// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package conform runs the UCP agent surface as an assertion suite.
//
// Two rules shape what is in here, both learned from the suites this
// replaces:
//
// It only asserts against capabilities the serving plane actually
// implements. cmd/ucp-sim carried 85 scenarios driving cart.*, identity.*,
// ap2.* and mcp.* steps against endpoints that have never existed; they
// could not pass, and their failures said nothing about whether Sarvex
// worked. The route table in internal/planes/serving/api/handler.go is the
// list of things worth checking, and it is short: profile discovery,
// catalog search, catalog lookup, and the checkout lifecycle.
//
// It cannot pass by skipping. Both replaced suites reported success while
// running zero assertions, because their infrastructure guard fired and
// nothing distinguished "everything passed" from "nothing ran". Here a run
// with no executed checks is a failure, and Report.Err says so.
package conform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"

	ucpagent "github.com/sarvexlab/sarvex/internal/pkg/ucp/agent"
)

// Outcome is how one check ended.
type Outcome string

const (
	// Pass means the check ran and its assertions held.
	Pass Outcome = "pass"
	// Fail means the check ran and an assertion did not hold.
	Fail Outcome = "fail"
	// Skip means a precondition this check depends on was not met — for
	// example, no catalog item to buy. A skip is reported, never hidden,
	// and a run consisting only of skips does not pass.
	Skip Outcome = "skip"
)

// Result is one check's outcome.
type Result struct {
	ID         string        `json:"id"`
	Capability string        `json:"capability"`
	Outcome    Outcome       `json:"outcome"`
	Detail     string        `json:"detail,omitempty"`
	Duration   time.Duration `json:"duration_ns"`
}

// Report is a whole run.
type Report struct {
	StoreID  string        `json:"store_id"`
	Seed     int64         `json:"seed,omitempty"`
	Results  []Result      `json:"results"`
	Duration time.Duration `json:"duration_ns"`
}

// Counts summarises a report.
func (r Report) Counts() (passed, failed, skipped int) {
	for _, res := range r.Results {
		switch res.Outcome {
		case Pass:
			passed++
		case Fail:
			failed++
		case Skip:
			skipped++
		}
	}
	return passed, failed, skipped
}

// Err returns nil only when the run is genuinely good: at least one check
// executed, and none failed.
//
// The "at least one" clause is the whole point. A suite that reports
// success having run nothing is worse than one that fails, because it keeps
// reporting success while the thing it guards rots.
func (r Report) Err() error {
	passed, failed, skipped := r.Counts()
	if passed == 0 && failed == 0 {
		return fmt.Errorf("no conformance checks executed (%d skipped) — a run that asserts nothing is not a pass", skipped)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d conformance checks failed", failed, len(r.Results))
	}
	return nil
}

// Fault injects a deliberate problem into the store between checks, so the
// suite exercises documented failure paths and not only the happy one.
//
// Implemented by the caller because faults are store-side mutations: for a
// simulated store that means the sim's own mutate/fix operations, which is
// the same path a human uses to reproduce these conditions by hand.
type Fault interface {
	// SetStock forces a SKU's stock quantity, and signals the change the
	// way the real plugin's inventory hook does.
	SetStock(ctx context.Context, storeID, sku string, quantity int) error
	// BreakAttribute clears a required product attribute, producing a
	// catalog-readiness problem.
	BreakAttribute(ctx context.Context, storeID, sku, field string) error
	// Fix restores a broken attribute to a good value.
	Fix(ctx context.Context, storeID, sku, field string) error
}

// Options configure a run.
type Options struct {
	// StoreID is the store to drive.
	StoreID string
	// Seed is recorded in the report so a failing run names the fixture
	// that produced it.
	Seed int64
	// Fault enables the failure-path checks. Nil runs only the happy path,
	// and the fault checks report as skipped rather than silently vanishing.
	Fault Fault
	// Timeout bounds each individual check.
	Timeout time.Duration
	// Progress receives a line per check as it completes. Nil discards.
	Progress func(Result)
}

func (o *Options) applyDefaults() {
	if o.Timeout <= 0 {
		o.Timeout = 60 * time.Second
	}
	if o.Progress == nil {
		o.Progress = func(Result) {}
	}
}

// state carries what one check discovers to the checks that follow — a
// catalog item to buy, a checkout to complete. Threading it explicitly
// keeps each check honest about what it depends on.
type state struct {
	handlerID  string
	sku        string
	checkoutID string
	orderID    string
}

// check is one named assertion over the agent surface.
type check struct {
	id         string
	capability string
	// needsFault marks a check that cannot run without a Fault injector.
	needsFault bool
	run        func(ctx context.Context, a *ucpagent.Agent, st *state, opts Options) error
}

// errSkip, returned by a check, reports the check as skipped with a reason
// rather than as a failure.
type errSkip struct{ reason string }

func (e *errSkip) Error() string { return e.reason }

func skipf(format string, args ...any) error {
	return &errSkip{reason: fmt.Sprintf(format, args...)}
}

// Run executes every check in order against one store.
func Run(ctx context.Context, a *ucpagent.Agent, opts Options) (*Report, error) {
	opts.applyDefaults()
	if opts.StoreID == "" {
		return nil, errors.New("store id is required")
	}

	report := &Report{StoreID: opts.StoreID, Seed: opts.Seed}
	st := &state{}
	start := time.Now()

	for _, c := range checks() {
		res := Result{ID: c.id, Capability: c.capability}
		checkStart := time.Now()

		switch {
		case c.needsFault && opts.Fault == nil:
			res.Outcome = Skip
			res.Detail = "no fault injector configured for this run"
		default:
			checkCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
			err := c.run(checkCtx, a, st, opts)
			cancel()

			var skip *errSkip
			switch {
			case err == nil:
				res.Outcome = Pass
			case errors.As(err, &skip):
				res.Outcome = Skip
				res.Detail = skip.reason
			default:
				res.Outcome = Fail
				res.Detail = err.Error()
			}
		}

		res.Duration = time.Since(checkStart)
		report.Results = append(report.Results, res)
		opts.Progress(res)
	}

	report.Duration = time.Since(start)
	return report, nil
}

// checks is the suite, in dependency order: discovery first, because
// nothing else is meaningful if a store does not advertise itself; then
// catalog, which supplies the item checkout needs; then checkout; then the
// failure paths.
func checks() []check {
	return []check{
		{
			id:         "profile.discoverable",
			capability: "discovery",
			run: func(ctx context.Context, a *ucpagent.Agent, st *state, opts Options) error {
				profile, err := a.FetchProfile(ctx, opts.StoreID)
				if err != nil {
					return err
				}
				if len(profile.Raw) == 0 {
					return errors.New("profile document is empty")
				}
				if _, ok := profile.Raw["ucp"]; !ok {
					return errors.New(`profile has no "ucp" block`)
				}
				return nil
			},
		},
		{
			id:         "profile.advertises_payment_handler",
			capability: "discovery",
			run: func(ctx context.Context, a *ucpagent.Agent, st *state, opts Options) error {
				profile, err := a.FetchProfile(ctx, opts.StoreID)
				if err != nil {
					return err
				}
				if len(profile.PaymentHandlers) == 0 {
					return errors.New("profile advertises no payment handlers, so no agent could complete a checkout")
				}
				st.handlerID = profile.PaymentHandlers[0]
				return nil
			},
		},
		{
			id:         "catalog.search",
			capability: "catalog.search",
			run: func(ctx context.Context, a *ucpagent.Agent, st *state, opts Options) error {
				result, err := a.SearchCatalog(ctx, opts.StoreID, "", 10, "")
				if err != nil {
					return err
				}
				if len(result.Products) == 0 {
					return errors.New("catalog search returned no products; the store has nothing an agent could buy")
				}
				st.sku = result.Products[0].ID
				return nil
			},
		},
		{
			id:         "catalog.search_pagination",
			capability: "catalog.search",
			run: func(ctx context.Context, a *ucpagent.Agent, st *state, opts Options) error {
				first, err := a.SearchCatalog(ctx, opts.StoreID, "", 1, "")
				if err != nil {
					return err
				}
				if len(first.Products) != 1 {
					return fmt.Errorf("limit=1 returned %d products, want 1", len(first.Products))
				}
				if !first.Pagination.HasNextPage {
					return skipf("store has only one product, so there is no next page to follow")
				}
				if first.Pagination.Cursor == "" {
					return errors.New("has_next_page is true but no cursor was returned, so the next page is unreachable")
				}
				next, err := a.SearchCatalog(ctx, opts.StoreID, "", 1, first.Pagination.Cursor)
				if err != nil {
					return fmt.Errorf("following the cursor failed: %w", err)
				}
				if len(next.Products) > 0 && next.Products[0].ID == first.Products[0].ID {
					return errors.New("the second page repeated the first page's product")
				}
				return nil
			},
		},
		{
			id:         "catalog.lookup",
			capability: "catalog.lookup",
			run: func(ctx context.Context, a *ucpagent.Agent, st *state, opts Options) error {
				if st.sku == "" {
					return skipf("no catalog item discovered by catalog.search")
				}
				item, err := a.LookupCatalogItem(ctx, opts.StoreID, st.sku)
				if err != nil {
					return err
				}
				if item.SKU() != st.sku {
					return fmt.Errorf("looked up %q but got %q back", st.sku, item.SKU())
				}
				price := item.Price()
				if price.Amount <= 0 {
					return fmt.Errorf("item %q has non-positive price %d", item.SKU(), price.Amount)
				}
				if price.Currency == "" {
					return fmt.Errorf("item %q has a price with no currency", item.SKU())
				}
				return nil
			},
		},
		{
			id:         "catalog.lookup_unknown_item",
			capability: "catalog.lookup",
			run: func(ctx context.Context, a *ucpagent.Agent, st *state, opts Options) error {
				_, err := a.LookupCatalogItem(ctx, opts.StoreID, "definitely-not-a-real-sku-0000")
				if err == nil {
					return errors.New("looking up a nonexistent item succeeded; it must not")
				}
				return nil
			},
		},
		{
			id:         "checkout.create",
			capability: "checkout",
			run: func(ctx context.Context, a *ucpagent.Agent, st *state, opts Options) error {
				if st.sku == "" {
					return skipf("no catalog item discovered by catalog.search")
				}
				resp, err := a.CreateCheckout(ctx, opts.StoreID, []ucpagent.LineItemInput{{SKU: st.sku, Quantity: 1}})
				if err != nil {
					return err
				}
				if resp.ID == "" {
					return errors.New("checkout was created without an id")
				}
				if len(resp.LineItems) == 0 {
					return errors.New("checkout came back with no line items")
				}
				st.checkoutID = resp.ID
				return nil
			},
		},
		{
			id:         "checkout.create_rejects_empty_cart",
			capability: "checkout",
			run: func(ctx context.Context, a *ucpagent.Agent, st *state, opts Options) error {
				_, err := a.CreateCheckout(ctx, opts.StoreID, nil)
				if err == nil {
					return errors.New("creating a checkout with no line items succeeded; it must not")
				}
				return nil
			},
		},
		{
			id:         "checkout.update",
			capability: "checkout",
			run: func(ctx context.Context, a *ucpagent.Agent, st *state, opts Options) error {
				if st.checkoutID == "" {
					return skipf("no checkout created")
				}
				resp, err := a.UpdateCheckout(ctx, opts.StoreID, st.checkoutID, buyer(), address())
				if err != nil {
					return err
				}
				if resp.Buyer == nil || resp.Buyer.Email == "" {
					return errors.New("the buyer supplied on update was not reflected back")
				}
				// The spec requires exactly one subtotal and one total
				// entry; the type knows its own invariant, so ask it.
				if err := resp.Totals.Validate(); err != nil {
					return fmt.Errorf("totals violate the spec invariant: %w", err)
				}
				total, ok := totalEntry(resp.Totals)
				if !ok {
					return errors.New("checkout has no total entry after update")
				}
				if total <= 0 {
					return fmt.Errorf("checkout total is %d after update; live pricing did not resolve", total)
				}
				return nil
			},
		},
		{
			id:         "checkout.complete",
			capability: "checkout",
			run: func(ctx context.Context, a *ucpagent.Agent, st *state, opts Options) error {
				if st.checkoutID == "" {
					return skipf("no checkout created")
				}
				if st.handlerID == "" {
					return skipf("store advertises no payment handler")
				}
				resp, err := a.CompleteCheckout(ctx, opts.StoreID, st.checkoutID, st.handlerID)
				if err != nil {
					return err
				}
				if resp.Order == nil || resp.Order.ID == "" {
					return fmt.Errorf("checkout completed in state %q but produced no order", resp.State)
				}
				st.orderID = resp.Order.ID
				return nil
			},
		},
		{
			id:         "checkout.complete_is_idempotent",
			capability: "checkout",
			run: func(ctx context.Context, a *ucpagent.Agent, st *state, opts Options) error {
				if st.orderID == "" {
					return skipf("no completed checkout to replay")
				}
				// The agent sends a per-checkout idempotency key, so
				// replaying complete must return the same order rather than
				// placing a second one. Retries are the normal case at every
				// external edge, not an exception.
				resp, err := a.CompleteCheckout(ctx, opts.StoreID, st.checkoutID, st.handlerID)
				if err != nil {
					return fmt.Errorf("replaying complete with the same idempotency key failed: %w", err)
				}
				if resp.Order == nil {
					return errors.New("the replayed complete returned no order")
				}
				if resp.Order.ID != st.orderID {
					return fmt.Errorf("replaying complete placed a second order: first %q, replay %q", st.orderID, resp.Order.ID)
				}
				return nil
			},
		},
		{
			id:         "checkout.rejects_unknown_payment_handler",
			capability: "checkout",
			run: func(ctx context.Context, a *ucpagent.Agent, st *state, opts Options) error {
				if st.sku == "" {
					return skipf("no catalog item discovered by catalog.search")
				}
				created, err := a.CreateCheckout(ctx, opts.StoreID, []ucpagent.LineItemInput{{SKU: st.sku, Quantity: 1}})
				if err != nil {
					return fmt.Errorf("setting up the check: %w", err)
				}
				if _, err := a.UpdateCheckout(ctx, opts.StoreID, created.ID, buyer(), address()); err != nil {
					return fmt.Errorf("setting up the check: %w", err)
				}
				if _, err := a.CompleteCheckout(ctx, opts.StoreID, created.ID, "dev.example.not_a_real_handler"); err == nil {
					return errors.New("completing with a handler the store does not advertise succeeded; it must not")
				}
				return nil
			},
		},
		{
			id:         "checkout.out_of_stock_is_refused",
			capability: "checkout",
			needsFault: true,
			run: func(ctx context.Context, a *ucpagent.Agent, st *state, opts Options) error {
				if st.sku == "" {
					return skipf("no catalog item discovered by catalog.search")
				}
				if err := opts.Fault.SetStock(ctx, opts.StoreID, st.sku, 0); err != nil {
					return fmt.Errorf("injecting the fault: %w", err)
				}
				// Restore the fixture whatever happens, so a failure here
				// does not poison every later run against this store.
				defer func() {
					_ = opts.Fault.SetStock(context.WithoutCancel(ctx), opts.StoreID, st.sku, 25)
				}()

				created, err := a.CreateCheckout(ctx, opts.StoreID, []ucpagent.LineItemInput{{SKU: st.sku, Quantity: 1}})
				if err != nil {
					// Refusing at create is a valid way to handle this.
					return nil
				}
				updated, err := a.UpdateCheckout(ctx, opts.StoreID, created.ID, buyer(), address())
				if err != nil {
					return nil
				}
				// Otherwise the checkout must not claim it is ready to
				// complete, and must say why.
				if updated.Status == model.CheckoutStatusReadyForComplete {
					return fmt.Errorf("checkout for an out-of-stock item reports %q with messages %q",
						updated.Status, messageText(updated.Messages))
				}
				if len(updated.Messages) == 0 {
					return fmt.Errorf("checkout for an out-of-stock item is %q but carries no message explaining why", updated.Status)
				}
				return nil
			},
		},
		{
			id:         "catalog.broken_attribute_surfaces",
			capability: "catalog.lookup",
			needsFault: true,
			run: func(ctx context.Context, a *ucpagent.Agent, st *state, opts Options) error {
				if st.sku == "" {
					return skipf("no catalog item discovered by catalog.search")
				}
				if err := opts.Fault.BreakAttribute(ctx, opts.StoreID, st.sku, "title"); err != nil {
					return fmt.Errorf("injecting the fault: %w", err)
				}
				defer func() {
					_ = opts.Fault.Fix(context.WithoutCancel(ctx), opts.StoreID, st.sku, "title")
				}()

				// The serving cache is event-driven, so give the change a
				// moment to propagate before reading it back.
				var item *ucpagent.CatalogItem
				deadline := time.Now().Add(15 * time.Second)
				for {
					var err error
					item, err = a.LookupCatalogItem(ctx, opts.StoreID, st.sku)
					if err != nil || item.Title == "" || time.Now().After(deadline) {
						break
					}
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(time.Second):
					}
				}
				if item != nil && item.Title != "" {
					return fmt.Errorf("a product whose title was cleared still serves title %q", item.Title)
				}
				return nil
			},
		},
	}
}

func buyer() *ucpagent.BuyerInput {
	return &ucpagent.BuyerInput{
		Email:     "conformance@example.com",
		FirstName: "Jane",
		LastName:  "Doe",
		Phone:     "+1-555-0123",
	}
}

func address() *ucpagent.AddressInput {
	return &ucpagent.AddressInput{
		FirstName: "Jane", LastName: "Doe",
		Street: "1 Main St", City: "Springfield",
		Region: "IL", PostalCode: "62701", CountryCode: "US",
	}
}

// totalEntry returns the amount of the single "total" entry the spec
// requires Totals to carry.
func totalEntry(totals model.Totals) (model.SignedAmount, bool) {
	for _, entry := range totals {
		if entry.Type == model.TotalTypeTotal {
			return entry.Amount, true
		}
	}
	return 0, false
}

// messageText renders a checkout's messages for a failure report. Message
// is a discriminated union, so each variant is unwrapped through its own
// accessor rather than reaching for a field that only some variants have.
func messageText(messages []model.Message) string {
	parts := make([]string, 0, len(messages))
	for _, m := range messages {
		switch {
		case hasError(m):
			e, _ := m.AsError()
			parts = append(parts, fmt.Sprintf("error(%s): %s", e.Code, e.Content))
		case hasWarning(m):
			w, _ := m.AsWarning()
			parts = append(parts, fmt.Sprintf("warning(%s): %s", w.Code, w.Content))
		default:
			i, _ := m.AsInfo()
			parts = append(parts, fmt.Sprintf("info(%s): %s", i.Code, i.Content))
		}
	}
	return strings.Join(parts, "; ")
}

func hasError(m model.Message) bool {
	_, ok := m.AsError()
	return ok
}

func hasWarning(m model.Message) bool {
	_, ok := m.AsWarning()
	return ok
}
