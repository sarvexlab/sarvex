// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package endpointhealth

import "time"

// ProbeType identifies one of the five synthetic checks run per store.
type ProbeType string

const (
	ProbeProfileReachable     ProbeType = "profile_reachable"
	ProbeSignatureRoundtrip   ProbeType = "signature_roundtrip"
	ProbeNegotiationHandshake ProbeType = "negotiation_handshake"
	ProbeCatalogLookup        ProbeType = "catalog_lookup"
)

// AllProbeTypes lists every probe run per cycle, in run order.
var AllProbeTypes = []ProbeType{
	ProbeProfileReachable,
	ProbeSignatureRoundtrip,
	ProbeNegotiationHandshake,
	ProbeCatalogLookup,
}

// Outcome is the coarse result of a single probe or evaluated check.
type Outcome string

const (
	OutcomePass     Outcome = "pass"
	OutcomeFail     Outcome = "fail"
	OutcomeDegraded Outcome = "degraded"
	OutcomeTimeout  Outcome = "timeout"
)

// FailureCode is a closed enum describing why a probe didn't pass.
type FailureCode string

const (
	FailureNone                       FailureCode = ""
	FailureUnreachable                FailureCode = "unreachable"
	FailureTimeout                    FailureCode = "timeout"
	FailureUnexpectedStatus           FailureCode = "unexpected_status"
	FailureInvalidJSON                FailureCode = "invalid_json"
	FailureSignatureMissing           FailureCode = "signature_missing"
	FailureSignatureInvalid           FailureCode = "signature_invalid"
	FailureEndpointMismatch           FailureCode = "endpoint_mismatch"
	FailureNoCommonCapability         FailureCode = "negotiation_no_common_capability"
	FailureNoCommonPaymentHandler     FailureCode = "negotiation_no_common_payment_handler"
	FailureMissingPriceOrAvailability FailureCode = "missing_price_or_availability"
)

// CheckType is the set of things the evaluator tracks thresholds for —
// a superset of ProbeType, adding real-traffic-only metrics.
type CheckType string

const (
	CheckProfileReachable       CheckType = CheckType(ProbeProfileReachable)
	CheckSignatureRoundtrip     CheckType = CheckType(ProbeSignatureRoundtrip)
	CheckNegotiationHandshake   CheckType = CheckType(ProbeNegotiationHandshake)
	CheckCatalogLookup          CheckType = CheckType(ProbeCatalogLookup)
	CheckSignatureFailureRate   CheckType = "signature_failure_rate"
	CheckNegotiationFailureRate CheckType = "negotiation_failure_rate"
	CheckTimeoutRate            CheckType = "timeout_rate"
	CheckLatencyP95             CheckType = "p95_latency"
	CheckErrorRate              CheckType = "error_rate"
)

// Severity mirrors domain.Severity's three levels for this package's own
// composite status (kept as a local type rather than importing
// control/domain, so this package's public surface has no dependency on
// the console-facing REST domain model — internal/planes/control/api
// composes the two at the HTTP boundary instead).
type Severity string

const (
	SeverityHealthy  Severity = "healthy"
	SeverityDegraded Severity = "degraded"
	SeverityCritical Severity = "critical"
)

// Config is the declarative threshold/interval configuration for probing
// and evaluation. Defaults are applied by DefaultConfig; a per-store
// override (config_test.go / mongo_config.go) may replace any field.
type Config struct {
	ProbeInterval time.Duration
	// CatalogLookupBudget is the max acceptable latency for the signed
	// catalog_lookup probe request.
	CatalogLookupBudget time.Duration
	// CanarySKU, if set, is looked up directly instead of discovering one
	// via a search call first.
	CanarySKU string

	// RollingWindow is the evaluation window for real-traffic rates.
	RollingWindow time.Duration
	// MinSampleSize is the minimum number of signed requests in
	// RollingWindow before real traffic is treated as authoritative over
	// probe results (see evaluator.go's precedence rule).
	MinSampleSize int

	SignatureFailureRateThreshold   float64
	NegotiationFailureRateThreshold float64
	TimeoutRateThreshold            float64
	ErrorRateThreshold              float64
	LatencyP95BudgetMs              int64

	// FlapBreachThreshold consecutive breaching evaluations before an alert
	// opens; FlapRecoveryThreshold consecutive recovering evaluations
	// before it auto-resolves.
	FlapBreachThreshold   int
	FlapRecoveryThreshold int

	// DegradedBatchWindow is how long degraded-alert notifications are
	// batched into a single digest per merchant.
	DegradedBatchWindow time.Duration
}

// DefaultConfig returns the system defaults, applied when no per-store
// override exists. Threshold rationale (documented for the report, not
// re-derived elsewhere): signature/negotiation failures above 5%/10% are
// well outside normal agent-retry noise; a 3s p95 budget is generous
// relative to the 5s probe client timeout so probes and real traffic use
// comparable scales; 48h feed staleness matches the merchant-facing
// "stale feed" language already used by the checklist service.
func DefaultConfig() Config {
	return Config{
		ProbeInterval:                   5 * time.Minute,
		CatalogLookupBudget:             2 * time.Second,
		RollingWindow:                   15 * time.Minute,
		MinSampleSize:                   20,
		SignatureFailureRateThreshold:   0.05,
		NegotiationFailureRateThreshold: 0.10,
		TimeoutRateThreshold:            0.05,
		ErrorRateThreshold:              0.05,
		LatencyP95BudgetMs:              3000,
		FlapBreachThreshold:             3,
		FlapRecoveryThreshold:           2,
		DegradedBatchWindow:             30 * time.Minute,
	}
}
