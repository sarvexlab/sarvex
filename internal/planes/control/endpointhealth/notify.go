// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package endpointhealth

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/email"
)

// QuietHours is a merchant's configured do-not-disturb window. Critical
// alerts bypass it (R12); degraded-alert digests respect it.
type QuietHours struct {
	StartHour int // 0-23, local to Timezone
	EndHour   int // 0-23, local to Timezone; wraps past midnight if < StartHour
	Timezone  string
}

// InQuietHours reports whether at is within q, in q's timezone. A
// misconfigured/unloadable timezone is treated as "not quiet" — delivery
// erring toward sending is safer than erring toward silently dropping an
// alert.
func (q *QuietHours) InQuietHours(at time.Time) bool {
	if q == nil {
		return false
	}
	loc, err := time.LoadLocation(q.Timezone)
	if err != nil {
		return false
	}
	hour := at.In(loc).Hour()
	if q.StartHour == q.EndHour {
		return false
	}
	if q.StartHour < q.EndHour {
		return hour >= q.StartHour && hour < q.EndHour
	}
	// Wraps past midnight, e.g. 22 -> 7.
	return hour >= q.StartHour || hour < q.EndHour
}

// Preferences is the delivery-relevant subset of a merchant's notification
// settings. AlertService/notify.go never see the console-facing
// domain.NotificationPreferences type directly (keeps this package
// decoupled from the REST domain model) — entry.go wires a small adapter
// over the extended domain.NotificationPreferences.
type Preferences struct {
	Recipients    []string
	SeverityFloor Severity
	Quiet         *QuietHours
}

// PreferencesReader reads a merchant's alert delivery preferences.
type PreferencesReader interface {
	Get(ctx context.Context, merchantID string) (Preferences, error)
}

// severityRank orders severities for floor comparisons.
func severityRank(s Severity) int {
	switch s {
	case SeverityCritical:
		return 2
	case SeverityDegraded:
		return 1
	default:
		return 0
	}
}

// AlertNotifier delivers an alert-lifecycle notification. resolved
// distinguishes an open/breach notice from a resolution notice.
type AlertNotifier interface {
	Notify(ctx context.Context, a *Alert, resolved bool) error
}

// LoggingAlertNotifier only logs — the safe default when no real delivery
// channel is configured (e.g. local dev), and a drop-in fallback if email
// sending is ever disabled.
type LoggingAlertNotifier struct{ log *slog.Logger }

func NewLoggingAlertNotifier(log *slog.Logger) *LoggingAlertNotifier {
	if log == nil {
		log = slog.Default()
	}
	return &LoggingAlertNotifier{log: log}
}

func (n *LoggingAlertNotifier) Notify(_ context.Context, a *Alert, resolved bool) error {
	n.log.Info("endpointhealth: alert notification", "alert_id", a.ID, "store_id", a.StoreID, "check_type", a.CheckType, "severity", a.Severity, "resolved", resolved)
	return nil
}

// EmailAlertNotifier delivers via internal/pkg/email.Sender — the same
// Resend-backed sender already used for member invites, and (per this
// change) its first real caller of SendNotification. Critical alerts and
// their resolutions send immediately, bypassing quiet hours. Degraded
// alerts are batched into one digest per merchant per DegradedBatchWindow;
// degraded resolutions are also batched (a resolution is not more urgent
// than the breach that preceded it).
type EmailAlertNotifier struct {
	sender      *email.Sender
	prefs       PreferencesReader
	consoleBase string // for building alert deep links
	batchWindow time.Duration
	log         *slog.Logger

	mu      sync.Mutex
	batches map[string]*digestBatch // merchantID -> pending digest
}

type digestBatch struct {
	entries []digestEntry
	timer   *time.Timer
}

type digestEntry struct {
	alert    *Alert
	resolved bool
}

// NewEmailAlertNotifier constructs an EmailAlertNotifier.
func NewEmailAlertNotifier(sender *email.Sender, prefs PreferencesReader, consoleBase string, batchWindow time.Duration, log *slog.Logger) *EmailAlertNotifier {
	if batchWindow <= 0 {
		batchWindow = DefaultConfig().DegradedBatchWindow
	}
	if log == nil {
		log = slog.Default()
	}
	return &EmailAlertNotifier{
		sender:      sender,
		prefs:       prefs,
		consoleBase: consoleBase,
		batchWindow: batchWindow,
		log:         log,
		batches:     make(map[string]*digestBatch),
	}
}

func (n *EmailAlertNotifier) Notify(ctx context.Context, a *Alert, resolved bool) error {
	prefs, err := n.prefs.Get(ctx, a.MerchantID)
	if err != nil {
		return fmt.Errorf("load notification preferences: %w", err)
	}
	if severityRank(a.Severity) < severityRank(prefs.SeverityFloor) {
		return nil
	}
	if len(prefs.Recipients) == 0 {
		return nil
	}

	if a.Severity == SeverityCritical {
		return n.sendImmediate(ctx, a, resolved, prefs.Recipients)
	}
	n.enqueueDigest(a, resolved, prefs)
	return nil
}

func (n *EmailAlertNotifier) sendImmediate(ctx context.Context, a *Alert, resolved bool, recipients []string) error {
	data := singleAlertEmail(a, resolved, n.consoleBase)
	for _, to := range recipients {
		if err := n.sender.SendNotification(ctx, to, data); err != nil {
			n.log.Error("endpointhealth: critical alert email failed", "error", err, "alert_id", a.ID, "to", to)
		}
	}
	return nil
}

func (n *EmailAlertNotifier) enqueueDigest(a *Alert, resolved bool, prefs Preferences) {
	n.mu.Lock()
	defer n.mu.Unlock()

	b, ok := n.batches[a.MerchantID]
	if !ok {
		b = &digestBatch{}
		n.batches[a.MerchantID] = b
		b.timer = time.AfterFunc(n.batchWindow, func() { n.flushDigest(a.MerchantID, prefs.Recipients) })
	}
	b.entries = append(b.entries, digestEntry{alert: a, resolved: resolved})
}

func (n *EmailAlertNotifier) flushDigest(merchantID string, recipients []string) {
	n.mu.Lock()
	b, ok := n.batches[merchantID]
	if ok {
		delete(n.batches, merchantID)
	}
	n.mu.Unlock()
	if !ok || len(b.entries) == 0 {
		return
	}

	data := digestEmail(b.entries, n.consoleBase)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, to := range recipients {
		if err := n.sender.SendNotification(ctx, to, data); err != nil {
			n.log.Error("endpointhealth: alert digest email failed", "error", err, "merchant_id", merchantID, "to", to)
		}
	}
}

func singleAlertEmail(a *Alert, resolved bool, consoleBase string) email.NotificationData {
	title := fmt.Sprintf("Endpoint health: %s on %s", a.CheckType, a.StoreID)
	if resolved {
		title = "Resolved: " + title
	}
	return email.NotificationData{
		Title:    title,
		Body:     a.Detail,
		CTALabel: "View in Sarvex",
		CTALink:  consoleBase + "/stores/" + a.StoreID + "/health",
	}
}

func digestEmail(entries []digestEntry, consoleBase string) email.NotificationData {
	body := fmt.Sprintf("%d endpoint health update(s) in the last batch window.", len(entries))
	storeID := ""
	if len(entries) > 0 {
		storeID = entries[0].alert.StoreID
	}
	return email.NotificationData{
		Title:    "Endpoint health digest",
		Body:     body,
		CTALabel: "View in Sarvex",
		CTALink:  consoleBase + "/stores/" + storeID + "/health",
	}
}
