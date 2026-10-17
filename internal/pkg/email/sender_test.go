// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package email

import (
	"context"
	"strings"
	"testing"
)

func newTestSender() (*Sender, *mockClient) {
	c := newMockClient()
	return NewSender(c, "notifications@sarvex.example.com", "Sarvex"), c
}

func TestSender_SendInviteMember(t *testing.T) {
	s, c := newTestSender()
	ctx := context.Background()

	err := s.SendInviteMember(ctx, "invitee@example.com", InviteMemberData{
		MerchantName: "Acme Co",
		SignInLink:   "https://merchant.example.com/signin",
	})
	if err != nil {
		t.Fatalf("SendInviteMember: %v", err)
	}

	sent := c.Sent()
	if len(sent) != 1 {
		t.Fatalf("expected 1 message, got %d", len(sent))
	}
	msg := sent[0]

	if msg.To != "invitee@example.com" {
		t.Errorf("To = %q, want invitee@example.com", msg.To)
	}
	if msg.From != "notifications@sarvex.example.com" || msg.FromName != "Sarvex" {
		t.Errorf("From = %q/%q, want notifications@sarvex.example.com/Sarvex", msg.From, msg.FromName)
	}
	if !strings.Contains(msg.Subject, "Acme Co") {
		t.Errorf("Subject = %q, want it to mention the merchant", msg.Subject)
	}
	if !strings.Contains(msg.HTML, "Acme Co") || !strings.Contains(msg.HTML, "https://merchant.example.com/signin") {
		t.Errorf("HTML missing merchant name or sign-in link: %s", msg.HTML)
	}
	if !strings.Contains(msg.Text, "https://merchant.example.com/signin") {
		t.Errorf("Text missing sign-in link: %s", msg.Text)
	}
}

func TestSender_SendTaskNotification(t *testing.T) {
	s, c := newTestSender()
	ctx := context.Background()

	err := s.SendTaskNotification(ctx, "merchant@example.com", TaskNotificationData{
		MerchantName: "Acme Co",
		TaskTitle:    "Fix missing GTIN on 4 products",
		WhyItMatters: "Products without GTINs are excluded from Google Shopping eligibility.",
		TaskLink:     "https://console.sarvex.example.com/tasks/t_123",
	})
	if err != nil {
		t.Fatalf("SendTaskNotification: %v", err)
	}

	msg := c.Sent()[0]
	if !strings.Contains(msg.Subject, "Fix missing GTIN on 4 products") {
		t.Errorf("Subject = %q, want it to mention the task title", msg.Subject)
	}
	if !strings.Contains(msg.HTML, "Fix missing GTIN on 4 products") ||
		!strings.Contains(msg.HTML, "Google Shopping eligibility") ||
		!strings.Contains(msg.HTML, "https://console.sarvex.example.com/tasks/t_123") {
		t.Errorf("HTML missing expected task content: %s", msg.HTML)
	}
}

func TestSender_SendTaskNotification_OmitsEmptyWhyItMatters(t *testing.T) {
	s, c := newTestSender()
	err := s.SendTaskNotification(context.Background(), "m@example.com", TaskNotificationData{
		MerchantName: "Acme Co",
		TaskTitle:    "Do the thing",
		TaskLink:     "https://console.sarvex.example.com/tasks/t_1",
	})
	if err != nil {
		t.Fatalf("SendTaskNotification: %v", err)
	}
	msg := c.Sent()[0]
	if !strings.Contains(msg.HTML, "Do the thing") {
		t.Errorf("HTML missing task title: %s", msg.HTML)
	}
}

func TestSender_SendNotification_WithCTA(t *testing.T) {
	s, c := newTestSender()
	err := s.SendNotification(context.Background(), "m@example.com", NotificationData{
		Title:    "Your store passed validation",
		Body:     "Acme Co is ready for production checkout.",
		CTALabel: "View report",
		CTALink:  "https://console.sarvex.example.com/validation",
	})
	if err != nil {
		t.Fatalf("SendNotification: %v", err)
	}
	msg := c.Sent()[0]
	if msg.Subject != "Your store passed validation" {
		t.Errorf("Subject = %q, want the notification title", msg.Subject)
	}
	if !strings.Contains(msg.HTML, "View report") || !strings.Contains(msg.HTML, "https://console.sarvex.example.com/validation") {
		t.Errorf("HTML missing CTA: %s", msg.HTML)
	}
}

func TestSender_SendNotification_WithoutCTA_OmitsButton(t *testing.T) {
	s, c := newTestSender()
	err := s.SendNotification(context.Background(), "m@example.com", NotificationData{
		Title: "Heads up",
		Body:  "Just an FYI, no action needed.",
	})
	if err != nil {
		t.Fatalf("SendNotification: %v", err)
	}
	msg := c.Sent()[0]
	// The CTA button is the only element styled with the accent background;
	// the footer's support-email link uses a different style, so this
	// substring is unique to a rendered call-to-action.
	if strings.Contains(msg.HTML, "background-color:#5B5BD6;color:#FFFFFF") {
		t.Errorf("HTML should omit the CTA button entirely when CTALink is empty: %s", msg.HTML)
	}
}

// TestSender_EscapesUserSuppliedContent guards against the template
// composition (content template auto-escaped, then embedded as trusted HTML
// into the layout) accidentally losing escaping for attacker-controlled
// fields like a merchant name or task title.
func TestSender_EscapesUserSuppliedContent(t *testing.T) {
	s, c := newTestSender()
	err := s.SendTaskNotification(context.Background(), "m@example.com", TaskNotificationData{
		MerchantName: "Acme Co",
		TaskTitle:    `<script>alert(1)</script>`,
		TaskLink:     "https://console.sarvex.example.com/tasks/t_1",
	})
	if err != nil {
		t.Fatalf("SendTaskNotification: %v", err)
	}
	msg := c.Sent()[0]
	if strings.Contains(msg.HTML, "<script>alert(1)</script>") {
		t.Fatalf("HTML content was not escaped: %s", msg.HTML)
	}
	if !strings.Contains(msg.HTML, "&lt;script&gt;") {
		t.Fatalf("expected escaped task title in HTML: %s", msg.HTML)
	}
}
