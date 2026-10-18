// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package account

import (
	"context"

	"github.com/sarvexlab/sarvex/internal/pkg/email"
)

// Mailer sends transactional emails. Implementations may be real (Resend)
// or mock (log, in-memory) depending on environment.
type Mailer interface {
	// SendInviteLink sends a sign-in link invitation to the given email address.
	// The link should direct the invited user to sign in with their IdP identity.
	// Implementations should be idempotent — calling twice with the same email is safe.
	SendInviteLink(ctx context.Context, email, merchantName, signInLink string) error
}

// NopMailer is a no-op implementation for environments where email sending is deferred.
type NopMailer struct{}

func (NopMailer) SendInviteLink(_ context.Context, _, _, _ string) error {
	return nil
}

// EmailMailer is the production Mailer, backed by the email package's
// Sender (Resend in production, mock/log in local and test environments —
// see email.NewClient).
type EmailMailer struct {
	sender *email.Sender
}

// NewEmailMailer wraps sender as a Mailer.
func NewEmailMailer(sender *email.Sender) Mailer {
	return &EmailMailer{sender: sender}
}

func (m *EmailMailer) SendInviteLink(ctx context.Context, to, merchantName, signInLink string) error {
	return m.sender.SendInviteMember(ctx, to, email.InviteMemberData{
		MerchantName: merchantName,
		SignInLink:   signInLink,
	})
}
