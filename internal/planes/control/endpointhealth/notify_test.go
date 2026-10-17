// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package endpointhealth

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/pkg/email"
)

// fakeEmailClient records every send instead of transmitting it.
type fakeEmailClient struct {
	mu   sync.Mutex
	sent []email.Message
}

func (c *fakeEmailClient) Send(_ context.Context, msg email.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, msg)
	return nil
}

func (c *fakeEmailClient) sentCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sent)
}

// fakePreferencesReader returns a fixed Preferences for every merchant.
type fakePreferencesReader struct{ prefs Preferences }

func (r fakePreferencesReader) Get(_ context.Context, _ string) (Preferences, error) {
	return r.prefs, nil
}

func TestEmailAlertNotifier_CriticalSendsImmediately(t *testing.T) {
	client := &fakeEmailClient{}
	sender := email.NewSender(client, "alerts@sarvex.test", "Sarvex")
	prefs := fakePreferencesReader{prefs: Preferences{Recipients: []string{"owner@acme.test"}, SeverityFloor: SeverityCritical}}
	notifier := NewEmailAlertNotifier(sender, prefs, "https://console.sarvex.test", time.Hour, nil)

	alert := &Alert{ID: "alt_1", MerchantID: "merch-1", StoreID: "store-1", CheckType: CheckSignatureRoundtrip, Severity: SeverityCritical, Detail: "signature failing"}
	require.NoError(t, notifier.Notify(context.Background(), alert, false))

	assert.Equal(t, 1, client.sentCount(), "critical alerts send immediately, not batched")
}

func TestEmailAlertNotifier_DegradedIsBatched(t *testing.T) {
	client := &fakeEmailClient{}
	sender := email.NewSender(client, "alerts@sarvex.test", "Sarvex")
	prefs := fakePreferencesReader{prefs: Preferences{Recipients: []string{"owner@acme.test"}, SeverityFloor: SeverityDegraded}}
	notifier := NewEmailAlertNotifier(sender, prefs, "https://console.sarvex.test", 20*time.Millisecond, nil)

	alert := &Alert{ID: "alt_1", MerchantID: "merch-1", StoreID: "store-1", CheckType: CheckCatalogLookup, Severity: SeverityDegraded, Detail: "catalog degraded"}
	require.NoError(t, notifier.Notify(context.Background(), alert, false))

	assert.Equal(t, 0, client.sentCount(), "degraded alerts wait for the batch window, not sent inline")

	require.Eventually(t, func() bool { return client.sentCount() == 1 }, time.Second, 5*time.Millisecond,
		"batched digest sends once the batch window elapses")
}

func TestEmailAlertNotifier_BelowSeverityFloor_NotSent(t *testing.T) {
	client := &fakeEmailClient{}
	sender := email.NewSender(client, "alerts@sarvex.test", "Sarvex")
	prefs := fakePreferencesReader{prefs: Preferences{Recipients: []string{"owner@acme.test"}, SeverityFloor: SeverityCritical}}
	notifier := NewEmailAlertNotifier(sender, prefs, "https://console.sarvex.test", time.Hour, nil)

	alert := &Alert{ID: "alt_1", MerchantID: "merch-1", StoreID: "store-1", CheckType: CheckTimeoutRate, Severity: SeverityDegraded, Detail: "elevated latency"}
	require.NoError(t, notifier.Notify(context.Background(), alert, false))

	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, 0, client.sentCount(), "a degraded alert is dropped when the merchant's floor is critical")
}

func TestEmailAlertNotifier_NoRecipients_NotSent(t *testing.T) {
	client := &fakeEmailClient{}
	sender := email.NewSender(client, "alerts@sarvex.test", "Sarvex")
	prefs := fakePreferencesReader{prefs: Preferences{Recipients: nil, SeverityFloor: SeverityCritical}}
	notifier := NewEmailAlertNotifier(sender, prefs, "https://console.sarvex.test", time.Hour, nil)

	alert := &Alert{ID: "alt_1", MerchantID: "merch-1", StoreID: "store-1", CheckType: CheckSignatureRoundtrip, Severity: SeverityCritical, Detail: "signature failing"}
	require.NoError(t, notifier.Notify(context.Background(), alert, false))
	assert.Equal(t, 0, client.sentCount())
}

func TestQuietHours_InQuietHours(t *testing.T) {
	q := &QuietHours{StartHour: 22, EndHour: 7, Timezone: "UTC"}

	late := time.Date(2026, 1, 1, 23, 0, 0, 0, time.UTC)
	assert.True(t, q.InQuietHours(late), "23:00 is within a 22:00-07:00 window that wraps midnight")

	early := time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC)
	assert.True(t, q.InQuietHours(early))

	midday := time.Date(2026, 1, 1, 14, 0, 0, 0, time.UTC)
	assert.False(t, q.InQuietHours(midday))

	assert.False(t, (*QuietHours)(nil).InQuietHours(late), "no configured quiet hours means never quiet")
}
