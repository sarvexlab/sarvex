// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package account_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/email"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/account"
)

func TestEmailMailer_SendInviteLink(t *testing.T) {
	client, err := email.NewClient(email.Config{}) // mock mode
	require.NoError(t, err)

	mailer := account.NewEmailMailer(email.NewSender(client, "notifications@sarvex.example.com", "Sarvex"))

	err = mailer.SendInviteLink(context.Background(), "invitee@example.com", "Acme Co", "https://merchant.example.com/signin")
	require.NoError(t, err)
}

func TestNopMailer_SendInviteLink(t *testing.T) {
	require.NoError(t, account.NopMailer{}.SendInviteLink(context.Background(), "a@example.com", "Acme Co", "https://x"))
}
