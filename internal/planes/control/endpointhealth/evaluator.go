// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package endpointhealth

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/errgroup"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/eventbus"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/monitoring"
)

const (
	taskEvaluateCycle = "endpointhealth:evaluate:cycle"
	evaluatorQueue    = "endpointhealth"
)

// Evaluator folds real-traffic telemetry into per-store rolling windows and
// periodically evaluates every check's threshold, applying the precedence
// rule: real traffic is authoritative for signature/negotiation health
// once a store's 15m window has at least Config.MinSampleSize signed
// requests; below that, the latest synthetic probe result is the fallback
// signal (a low-traffic store's window is too noisy to trust, and probes
// run on a fixed cadence regardless of traffic).
type Evaluator struct {
	bus    eventbus.EventBus
	window *Window
	status *StatusEngine
	stores monitoring.StoreLister
	redis  *redis.Client
	cfg    Config
	log    *slog.Logger

	server    *asynq.Server
	scheduler *asynq.Scheduler
	client    *asynq.Client

	mu   sync.Mutex
	subs []eventbus.Subscription
}

// NewEvaluator constructs an Evaluator.
func NewEvaluator(redisConnOpt asynq.RedisConnOpt, bus eventbus.EventBus, window *Window, status *StatusEngine, stores monitoring.StoreLister, redisClient *redis.Client, cfg Config, log *slog.Logger) *Evaluator {
	if log == nil {
		log = slog.Default()
	}
	return &Evaluator{
		bus:       bus,
		window:    window,
		status:    status,
		stores:    stores,
		redis:     redisClient,
		cfg:       cfg,
		log:       log,
		server:    asynq.NewServer(redisConnOpt, asynq.Config{Concurrency: storeConcurrency, Queues: map[string]int{evaluatorQueue: 1}}),
		scheduler: asynq.NewScheduler(redisConnOpt, nil),
		client:    asynq.NewClient(redisConnOpt),
	}
}

// Start subscribes to telemetry/probe events and starts the periodic
// evaluation cycle (asynq TaskID+Unique dedup, same cross-replica pattern
// as ProbeScheduler — see its doc comment).
func (e *Evaluator) Start(ctx context.Context) error {
	e.mu.Lock()
	telemetrySub, err := e.bus.Subscribe(ctx, ucp.EventTelemetryRecorded, e.handleTelemetry)
	if err != nil {
		e.mu.Unlock()
		return fmt.Errorf("subscribe %s: %w", ucp.EventTelemetryRecorded, err)
	}
	probeSub, err := e.bus.Subscribe(ctx, ucp.EventHealthProbeCompleted, e.handleProbeCompleted)
	if err != nil {
		e.mu.Unlock()
		return fmt.Errorf("subscribe %s: %w", ucp.EventHealthProbeCompleted, err)
	}
	e.subs = append(e.subs, telemetrySub, probeSub)
	e.mu.Unlock()

	mux := asynq.NewServeMux()
	mux.HandleFunc(taskEvaluateCycle, func(ctx context.Context, _ *asynq.Task) error {
		e.RunCycle(ctx)
		return nil
	})
	if err := e.server.Start(mux); err != nil {
		return fmt.Errorf("asynq server start: %w", err)
	}

	interval := e.cfg.RollingWindow / 3
	if interval < time.Minute {
		interval = time.Minute
	}
	if _, err := e.scheduler.Register("@every "+interval.String(),
		asynq.NewTask(taskEvaluateCycle, nil),
		asynq.Queue(evaluatorQueue),
		asynq.TaskID("evaluate:cycle"),
		asynq.Unique(interval),
	); err != nil {
		e.server.Shutdown()
		return fmt.Errorf("register evaluate cycle: %w", err)
	}
	if err := e.scheduler.Start(); err != nil {
		e.server.Shutdown()
		return fmt.Errorf("asynq scheduler start: %w", err)
	}
	return nil
}

// Stop unsubscribes and shuts down the scheduler/server.
func (e *Evaluator) Stop(ctx context.Context) {
	e.scheduler.Shutdown()
	e.server.Shutdown()
	_ = e.client.Close()

	e.mu.Lock()
	defer e.mu.Unlock()
	for _, sub := range e.subs {
		if err := e.bus.Unsubscribe(ctx, sub); err != nil {
			e.log.Error("endpointhealth: unsubscribe failed", "topic", sub.Topic(), "error", err)
		}
	}
	e.subs = nil
}

// handleTelemetry folds one serving.request.telemetry_recorded event into
// its store's rolling window. Synthetic (probe) traffic is excluded —
// real-traffic evaluation must reflect actual agents, not Sarvex's own
// probes (see internal/pkg/ucp/probe.go's ProbeHeader).
func (e *Evaluator) handleTelemetry(ctx context.Context, ev eventbus.DomainEvent) error {
	var payload struct {
		StoreID           string `json:"store_id"`
		RouteTemplate     string `json:"route_template"`
		StatusCode        int    `json:"status_code"`
		LatencyMs         int64  `json:"latency_ms"`
		SignatureResult   string `json:"signature_result"`
		NegotiationResult string `json:"negotiation_result"`
		Synthetic         bool   `json:"synthetic"`
		RequestID         string `json:"request_id"`
	}
	if err := json.Unmarshal(ev.Payload(), &payload); err != nil {
		e.log.Error("endpointhealth: unmarshal telemetry event failed", "error", err)
		return nil
	}
	if payload.Synthetic {
		return nil
	}

	rec := windowRecord{
		RequestID: payload.RequestID,
		Route:     payload.RouteTemplate,
		Status:    payload.StatusCode,
		Signature: payload.SignatureResult,
		Negotiate: payload.NegotiationResult,
		LatencyMs: payload.LatencyMs,
	}
	if err := e.window.Record(ctx, payload.StoreID, rec, ev.OccurredAt()); err != nil {
		e.log.Error("endpointhealth: record window entry failed", "error", err, "store_id", payload.StoreID)
	}
	return nil
}

// handleProbeCompleted evaluates the two probe-only checks
// (profile_reachable, catalog_lookup — no real-traffic
// equivalent, so evaluated straight from the probe result) immediately.
// signature_roundtrip and negotiation_handshake results are cached instead
// of evaluated directly here — RunCycle applies the precedence rule and is
// the sole caller of StatusEngine.Evaluate for those two checks, so a
// probe result and a same-tick real-traffic verdict never race each other
// into conflicting flap-suppression counters for the same check.
func (e *Evaluator) handleProbeCompleted(ctx context.Context, ev eventbus.DomainEvent) error {
	var payload struct {
		StoreID     string      `json:"store_id"`
		MerchantID  string      `json:"merchant_id"`
		ProbeType   ProbeType   `json:"probe_type"`
		Outcome     Outcome     `json:"outcome"`
		FailureCode FailureCode `json:"failure_code"`
		Detail      string      `json:"detail"`
	}
	if err := json.Unmarshal(ev.Payload(), &payload); err != nil {
		e.log.Error("endpointhealth: unmarshal probe event failed", "error", err)
		return nil
	}

	switch payload.ProbeType {
	case ProbeProfileReachable, ProbeCatalogLookup:
		breaching := payload.Outcome == OutcomeFail || payload.Outcome == OutcomeTimeout
		value := 0.0
		if breaching {
			value = 1.0
		}
		check := CheckType(payload.ProbeType)
		if err := e.status.Evaluate(ctx, payload.MerchantID, payload.StoreID, check, breaching, value, payload.Detail, ev.OccurredAt()); err != nil {
			e.log.Error("endpointhealth: evaluate probe check failed", "error", err, "store_id", payload.StoreID, "check", check)
		}
	case ProbeSignatureRoundtrip, ProbeNegotiationHandshake:
		e.cacheLastProbe(ctx, payload.StoreID, CheckType(payload.ProbeType), payload.Outcome == OutcomePass)
	}
	return nil
}

func lastProbeKey(storeID string, check CheckType) string {
	return "health:lastprobe:" + storeID + ":" + string(check)
}

func (e *Evaluator) cacheLastProbe(ctx context.Context, storeID string, check CheckType, passed bool) {
	if e.redis == nil {
		return
	}
	v := "fail"
	if passed {
		v = "pass"
	}
	if err := e.redis.Set(ctx, lastProbeKey(storeID, check), v, 2*e.cfg.ProbeInterval).Err(); err != nil {
		e.log.Warn("endpointhealth: cache last probe result failed", "error", err, "store_id", storeID)
	}
}

func (e *Evaluator) lastProbeBreaching(ctx context.Context, storeID string, check CheckType) (breaching bool, known bool) {
	if e.redis == nil {
		return false, false
	}
	v, err := e.redis.Get(ctx, lastProbeKey(storeID, check)).Result()
	if err != nil {
		return false, false
	}
	return v == "fail", true
}

// RunCycle evaluates real-traffic-derived checks for every store, applying
// the precedence rule for signature/negotiation. Exported for tests and
// the periodic asynq handler.
func (e *Evaluator) RunCycle(ctx context.Context) {
	cursor := ""
	for {
		refs, hasMore, err := e.stores.ListAllStores(ctx, cursor, 50)
		if err != nil {
			e.log.Error("endpointhealth: evaluator list stores failed", "error", err)
			return
		}

		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(storeConcurrency)
		for _, ref := range refs {
			g.Go(func() error {
				if gctx.Err() != nil {
					return gctx.Err()
				}
				e.evaluateStore(gctx, ref)
				return nil
			})
		}
		_ = g.Wait()

		if ctx.Err() != nil {
			return
		}
		if !hasMore || len(refs) == 0 {
			return
		}
		cursor = refs[len(refs)-1].StoreID
	}
}

func (e *Evaluator) evaluateStore(ctx context.Context, ref monitoring.StoreRef) {
	records, err := e.window.Snapshot(ctx, ref.StoreID)
	if err != nil {
		e.log.Error("endpointhealth: window snapshot failed", "error", err, "store_id", ref.StoreID)
		return
	}
	metrics := ComputeMetrics(records)
	now := time.Now().UTC()

	e.evaluatePrecedence(ctx, ref, CheckSignatureRoundtrip, metrics.SampleSize, metrics.SignatureFailureRate, e.cfg.SignatureFailureRateThreshold, now)
	e.evaluatePrecedence(ctx, ref, CheckNegotiationHandshake, metrics.SampleSize, metrics.NegotiationFailureRate, e.cfg.NegotiationFailureRateThreshold, now)

	if metrics.SampleSize < e.cfg.MinSampleSize {
		// timeout_rate/error_rate/p95_latency have no probe-based fallback
		// signal at all — with too few real samples there's simply nothing
		// to evaluate this cycle, which is not the same as "healthy".
		return
	}

	if err := e.status.Evaluate(ctx, ref.MerchantID, ref.StoreID, CheckTimeoutRate, metrics.TimeoutRate > e.cfg.TimeoutRateThreshold, metrics.TimeoutRate,
		fmt.Sprintf("timeout rate %.1f%% over last %s (%d samples)", metrics.TimeoutRate*100, e.cfg.RollingWindow, metrics.SampleSize), now); err != nil {
		e.log.Error("endpointhealth: evaluate timeout_rate failed", "error", err, "store_id", ref.StoreID)
	}
	if err := e.status.Evaluate(ctx, ref.MerchantID, ref.StoreID, CheckErrorRate, metrics.ErrorRate > e.cfg.ErrorRateThreshold, metrics.ErrorRate,
		fmt.Sprintf("5xx error rate %.1f%% over last %s (%d samples)", metrics.ErrorRate*100, e.cfg.RollingWindow, metrics.SampleSize), now); err != nil {
		e.log.Error("endpointhealth: evaluate error_rate failed", "error", err, "store_id", ref.StoreID)
	}
	if err := e.status.Evaluate(ctx, ref.MerchantID, ref.StoreID, CheckLatencyP95, metrics.P95LatencyMs > e.cfg.LatencyP95BudgetMs, float64(metrics.P95LatencyMs),
		fmt.Sprintf("p95 latency %dms over last %s (%d samples)", metrics.P95LatencyMs, e.cfg.RollingWindow, metrics.SampleSize), now); err != nil {
		e.log.Error("endpointhealth: evaluate p95_latency failed", "error", err, "store_id", ref.StoreID)
	}
}

// evaluatePrecedence applies the real-traffic-vs-probe precedence rule for
// one merged check and calls StatusEngine.Evaluate exactly once.
func (e *Evaluator) evaluatePrecedence(ctx context.Context, ref monitoring.StoreRef, check CheckType, sampleSize int, rate, threshold float64, now time.Time) {
	var breaching bool
	var value float64
	var detail string
	var source string

	if sampleSize >= e.cfg.MinSampleSize {
		breaching = rate > threshold
		value = rate
		detail = fmt.Sprintf("real-traffic %s %.1f%% over last %s (%d samples)", check, rate*100, e.cfg.RollingWindow, sampleSize)
		source = "real_traffic"
	} else if probeBreaching, known := e.lastProbeBreaching(ctx, ref.StoreID, check); known {
		breaching = probeBreaching
		if breaching {
			value = 1
		}
		detail = fmt.Sprintf("insufficient real traffic (%d samples) — falling back to latest synthetic probe result", sampleSize)
		source = "probe"
	} else {
		// No real-traffic sample and no probe result cached yet — nothing
		// to evaluate.
		return
	}

	if err := e.status.Evaluate(ctx, ref.MerchantID, ref.StoreID, check, breaching, value, detail, now); err != nil {
		e.log.Error("endpointhealth: evaluate check failed", "error", err, "store_id", ref.StoreID, "check", check, "source", source)
	}
}
