// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package billing

import (
	"context"
	"errors"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

var ErrNotImplemented = errors.New("not implemented")

// StubService is a Service that returns ErrNotImplemented for every call.
type StubService struct{}

func (StubService) Get(_ context.Context, _ domain.MerchantID) (domain.Billing, error) {
	return domain.Billing{}, ErrNotImplemented
}
func (StubService) CreatePortalLink(_ context.Context, _ domain.MerchantID) (domain.HostedURL, error) {
	return domain.HostedURL{}, ErrNotImplemented
}
func (StubService) GetNotificationPreferences(_ context.Context, _ domain.MerchantID) (domain.NotificationPreferences, error) {
	return domain.NotificationPreferences{}, ErrNotImplemented
}
func (StubService) SetNotificationPreferences(_ context.Context, _ domain.MerchantID, _ domain.NotificationPreferences) (domain.NotificationPreferences, error) {
	return domain.NotificationPreferences{}, ErrNotImplemented
}
func (StubService) HandleStripeWebhook(_ context.Context, _ []byte, _ string) error {
	return ErrNotImplemented
}
func (StubService) ProvisionTrial(_ context.Context, _ domain.MerchantID, _ string) error {
	return ErrNotImplemented
}
func (StubService) EnsurePortalConfiguration(_ context.Context) (string, error) {
	return "", ErrNotImplemented
}

var _ Service = StubService{}
