// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package email

import (
	"context"
	"fmt"
	htmltemplate "html/template"
	texttemplate "text/template"
)

// Sender is the typed, template-driven API callers use to send email. It
// owns the From identity and template rendering; callers never see Message
// or the transport Client directly.
type Sender struct {
	client   Client
	from     string
	fromName string
}

// NewSender creates a Sender that sends through client as fromName <from>.
func NewSender(client Client, from, fromName string) *Sender {
	return &Sender{client: client, from: from, fromName: fromName}
}

// InviteMemberData is the content for a member sign-in invitation.
type InviteMemberData struct {
	MerchantName string
	SignInLink   string
}

// SendInviteMember sends a sign-in link inviting to join a merchant's Sarvex account.
func (s *Sender) SendInviteMember(ctx context.Context, to string, data InviteMemberData) error {
	subject := fmt.Sprintf("You're invited to %s on Sarvex", data.MerchantName)
	return s.render(ctx, to, subject, inviteMemberHTML, inviteMemberText, data)
}

// TaskNotificationData is the content for a merchant-owned task raised in the console.
type TaskNotificationData struct {
	MerchantName string
	TaskTitle    string
	WhyItMatters string
	TaskLink     string
}

// SendTaskNotification tells a merchant a task needs their attention.
func (s *Sender) SendTaskNotification(ctx context.Context, to string, data TaskNotificationData) error {
	subject := fmt.Sprintf("Action needed: %s", data.TaskTitle)
	return s.render(ctx, to, subject, taskNotificationHTML, taskNotificationText, data)
}

// NotificationData is the content for a general-purpose control-plane notification
// (billing, monitoring alerts, etc). CTALabel/CTALink are optional; when CTALink
// is empty, the templates omit the call-to-action button.
type NotificationData struct {
	Title    string
	Body     string
	CTALabel string
	CTALink  string
}

// SendNotification sends a general-purpose notification email.
func (s *Sender) SendNotification(ctx context.Context, to string, data NotificationData) error {
	return s.render(ctx, to, data.Title, notificationHTML, notificationText, data)
}

func (s *Sender) render(ctx context.Context, to, subject string, htmlTmpl *htmltemplate.Template, textTmpl *texttemplate.Template, data any) error {
	html, err := renderHTML(htmlTmpl, subject, data)
	if err != nil {
		return err
	}
	text, err := renderText(textTmpl, data)
	if err != nil {
		return err
	}
	return s.client.Send(ctx, Message{
		To:       to,
		From:     s.from,
		FromName: s.fromName,
		Subject:  subject,
		HTML:     html,
		Text:     text,
	})
}
