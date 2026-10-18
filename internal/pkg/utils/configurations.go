// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"strings"

	"github.com/tkanos/gonfig"
)

type Configurations struct {
	Env                        string `env:"env"`
	PostgresqlConnectionString string `env:"POSTGRESQL_CONNECTION_STRING"`
	MongoConnectionString      string `env:"MONGO_CONNECTION_STRING"`
	OutboxCycleTime            int32
	LinkValidityTime           int32
	// EmailMode selects the email transport ("mock" logs instead of sending;
	// "resend" sends via the Resend API). Defaults to mock if empty.
	EmailMode          string `env:"EMAIL_MODE"`
	ResendApiKey       string `env:"RESEND_API_KEY"`
	SenderEmailAddress string
	SenderTitle        string
	DashboardAddress   string `env:"DASHBOARD_ADDRESS"`
	RegisterSub        string
	LoginSub           string
	LoginTimeOut       int32
	TokenValidityTime  int32
	AckDeadline        int
	ServiceName        string
	KindeIssuerURL     string `env:"KINDE_ISSUER_URL"`
	KindeAudience      string `env:"KINDE_AUDIENCE"`
	MaxConcurrency     int
	ResentMailTimeOut  string
	MaxSecurityEvents  int
	LogLevel           string
	RedirectUrl        string
	VerifyUrl          string
	// BillingMode selects the billing/Stripe transport ("mock" fabricates
	// hosted URLs instead of calling Stripe; "stripe" creates real Stripe
	// Checkout/Portal sessions). Defaults to mock if empty.
	BillingMode     string `env:"BILLING_MODE"`
	StripeSecretKey string `env:"STRIPE_SECRET_KEY"`
	// NATSURL is the NATS JetStream server address — a region's own regional NATS leaf node, not the core supercluster
	// directly (see deployments/selfhost/scripts/bootstrap-nats-leaf.sh).
	NATSURL string `env:"NATS_URL"`
	// VaultAddr/VaultToken/VaultTransitKeyName configure jwkmanagement's
	// Vault Transit-backed Encryptor (see
	// internal/pkg/jwkmanagement/vault.go). VaultToken is normally left
	// empty in production; the Vault Agent Injector supplies one.
	VaultAddr           string `env:"VAULT_ADDR"`
	VaultToken          string `env:"VAULT_TOKEN"`
	VaultTransitKeyName string `env:"VAULT_TRANSIT_KEY_NAME"`
	// VaultAddr/VaultToken above also back internal/pkg/secretmanagement's KV v2
	// store: one Vault deployment serves both roles (Transit for jwkmanagement's
	// signing keys, KV for connector credentials).
	// StripeWebhookSecret verifies the Stripe-Signature header on
	// POST /billing/webhooks/stripe (the Stripe-issued webhook signing secret).
	StripeWebhookSecret string `env:"STRIPE_WEBHOOK_SECRET"`
	// ContactApiToken gates POST /contact — the shared bearer token issued to
	// the sarvex.example.com marketing website only. Empty disables the route (fail
	// closed, same convention as SIMULATION_SECRET).
	ContactApiToken string `env:"CONTACT_API_TOKEN"`
	// ContactNotifyEmail is where POST /contact forwards submissions.
	// Defaults to hello@sarvex.example.com when unset (see contact.go).
	ContactNotifyEmail string `env:"CONTACT_NOTIFY_EMAIL"`
	// ContactSenderEmail is the From address POST /contact sends as.
	// Defaults to hello@sarvex.example.com when unset (see entry.go).
	ContactSenderEmail string `env:"CONTACT_SENDER_EMAIL"`
	// ContactAllowedOrigins is a comma-separated CORS allow-list for
	// POST /contact (the sarvex.example.com marketing website's own origin(s)).
	// Defaults to "https://sarvex.example.com,https://www.sarvex.example.com" when unset
	// (see entry.go).
	ContactAllowedOrigins            string `env:"CONTACT_ALLOWED_ORIGINS"`
	WebLink                          string
	SystemLink                       string
	FeedbackLink                     string
	BillingInternalBaseUrl           string
	TokenGenKey                      string `env:"TOKEN_GEN_KEY"`
	BundleVerificationKey            string `env:"BUNDLE_VERIFICATION_KEY"`
	FeedbackReceiverEmail            string
	RedisAddress                     string `env:"REDIS_ADDRESS"`
	RedisPassword                    string `env:"REDIS_PASSWORD"`
	RedisDatabase                    int    `env:"REDIS_DATABASE"`
	RedisConnPoolSize                int
	SlackToken                       string `env:"SLACK_APP_TOKEN"`
	SlackChannel                     string `env:"SLACK_CHANNEL"`
	Deployment_HealthCheckTimeoutMs  int
	Deployment_MaxHealthCheckRetries int
	// ServingBaseURL is the serving plane's UCP base *including* the /ucp
	// route prefix — the form cmd/sim and loadtest/ expect, since they append
	// /{platform}/{store_id} to it themselves. Go callers that build a store
	// endpoint with ucp.StoreEndpointURL want the origin instead: see
	// ServingOrigin.
	ServingBaseURL string `env:"SERVING_BASE_URL"`
	// ControlBaseURL is the control plane's own publicly-reachable base URL
	// (no trailing slash). Used only by the endpoint-health probe identity's
	// /_probe/.well-known/ucp profile document — merchants' serving-plane
	// instances must be able to fetch it over the public internet to verify
	// probe requests and negotiate against it.
	ControlBaseURL         string `env:"CONTROL_BASE_URL"`
	TestingEnvironment     string `env:"TESTING_ENVIRONMENT"`
	SimulationSecret       string `env:"SIMULATION_SECRET"`
	JWKManagementMode      string `env:"JWK_MANAGEMENT_MODE"`
	RepositoryType         string `env:"REPOSITORY_TYPE"`
	StripePortalReturnURL  string `env:"STRIPE_PORTAL_RETURN_URL"`
	StripePriceIDSelfServe string `env:"STRIPE_PRICE_ID_SELF_SERVE"`
	StripePriceIDConcierge string `env:"STRIPE_PRICE_ID_CONCIERGE"`
	StripePriceIDScale     string `env:"STRIPE_PRICE_ID_SCALE"`
	// BillingTrialDays is the length of the no-payment-method trial granted on
	// the entry-level (self_serve) plan when a merchant account is created.
	BillingTrialDays int `env:"BILLING_TRIAL_DAYS"`
	// StripePortalConfigurationID is the Stripe Billing Portal Configuration
	// every portal session is scoped to (plan-switch/cancel behavior — see
	// billing.BillingService.EnsurePortalConfiguration). Empty on first boot
	// in a new environment; EnsurePortalConfiguration creates one and logs
	// its ID for an operator to persist here.
	StripePortalConfigurationID  string `env:"STRIPE_PORTAL_CONFIGURATION_ID"`
	ValidationTestHarnessBaseURL string `env:"VALIDATION_TEST_HARNESS_BASE_URL"`
	SpecVersion                  string `env:"SPEC_VERSION"`
	SimStoreBaseURL              string `env:"SIM_STORE_BASE_URL"`
}

func LoadConfigurations(path string) (*Configurations, error) {
	c := Configurations{}

	err := gonfig.GetConf(path, &c)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ServingOrigin returns the serving plane's origin — ServingBaseURL with the
// /ucp route prefix trimmed off.
//
// SERVING_BASE_URL carries the prefix (scripts/dev/up.sh exports
// http://localhost:8083/ucp, and cmd/sim + loadtest/ both depend on that
// form). ucp.StoreEndpointURL adds /ucp/{platform}/{storeID} itself, so
// handing it the raw value builds /ucp/ucp/... and 404s.
//
// That 404 is not cosmetic: the activation verifier and the health probes are
// the only producers of verification_passed and of the passing profile probe
// that promotes a store to active. With both 404ing, every store pins at
// profile_published with no error surfaced anywhere — the checks simply
// report "status 404" and the state machine correctly refuses to advance.
func (c *Configurations) ServingOrigin() string {
	return strings.TrimSuffix(strings.TrimSuffix(c.ServingBaseURL, "/"), "/ucp")
}
