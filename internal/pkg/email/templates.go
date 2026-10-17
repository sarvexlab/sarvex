// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package email

import (
	"bytes"
	"embed"
	"fmt"
	htmltemplate "html/template"
	texttemplate "text/template"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

var (
	htmlLayout = htmltemplate.Must(htmltemplate.ParseFS(templateFS, "templates/layout.html.tmpl"))
	textLayout = texttemplate.Must(texttemplate.ParseFS(templateFS, "templates/layout.txt.tmpl"))

	inviteMemberHTML     = htmltemplate.Must(htmltemplate.ParseFS(templateFS, "templates/invite_member.html.tmpl"))
	inviteMemberText     = texttemplate.Must(texttemplate.ParseFS(templateFS, "templates/invite_member.txt.tmpl"))
	taskNotificationHTML = htmltemplate.Must(htmltemplate.ParseFS(templateFS, "templates/task_notification.html.tmpl"))
	taskNotificationText = texttemplate.Must(texttemplate.ParseFS(templateFS, "templates/task_notification.txt.tmpl"))
	notificationHTML     = htmltemplate.Must(htmltemplate.ParseFS(templateFS, "templates/notification.html.tmpl"))
	notificationText     = texttemplate.Must(texttemplate.ParseFS(templateFS, "templates/notification.txt.tmpl"))
)

type layoutHTMLData struct {
	Title       string
	PreviewText string
	ContentHTML htmltemplate.HTML
}

type layoutTextData struct {
	Content string
}

// renderHTML executes contentTmpl (auto-escaping its data) and embeds the
// result into the branded layout. Safe: the injected HTML was itself
// produced by html/template, so any user-supplied field within data was
// already escaped before this point.
func renderHTML(contentTmpl *htmltemplate.Template, title string, data any) (string, error) {
	var content bytes.Buffer
	if err := contentTmpl.Execute(&content, data); err != nil {
		return "", fmt.Errorf("render email content: %w", err)
	}

	var out bytes.Buffer
	err := htmlLayout.Execute(&out, layoutHTMLData{
		Title:       title,
		PreviewText: title,
		ContentHTML: htmltemplate.HTML(content.String()),
	})
	if err != nil {
		return "", fmt.Errorf("render email layout: %w", err)
	}
	return out.String(), nil
}

func renderText(contentTmpl *texttemplate.Template, data any) (string, error) {
	var content bytes.Buffer
	if err := contentTmpl.Execute(&content, data); err != nil {
		return "", fmt.Errorf("render email content: %w", err)
	}

	var out bytes.Buffer
	if err := textLayout.Execute(&out, layoutTextData{Content: content.String()}); err != nil {
		return "", fmt.Errorf("render email layout: %w", err)
	}
	return out.String(), nil
}
