// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package email

import (
	"context"
	"fmt"

	"github.com/resend/resend-go/v2"
)

// resendClient sends email via the Resend API.
type resendClient struct {
	api *resend.Client
}

func newResendClient(cfg Config) (Client, error) {
	if cfg.ResendAPIKey == "" {
		return nil, fmt.Errorf("RESEND_API_KEY is required in resend mode")
	}
	return &resendClient{api: resend.NewClient(cfg.ResendAPIKey)}, nil
}

func (c *resendClient) Send(ctx context.Context, msg Message) error {
	from := msg.From
	if msg.FromName != "" {
		from = fmt.Sprintf("%s <%s>", msg.FromName, msg.From)
	}

	_, err := c.api.Emails.SendWithContext(ctx, &resend.SendEmailRequest{
		From:    from,
		To:      []string{msg.To},
		Subject: msg.Subject,
		Html:    msg.HTML,
		Text:    msg.Text,
	})
	if err != nil {
		return fmt.Errorf("resend: send email: %w", err)
	}
	return nil
}
