// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"encoding/json"
	"fmt"
)

// MessageError is an error-severity Message.
type MessageError struct {
	Code        ErrorCode          `json:"code"`
	Path        string             `json:"path,omitempty"`
	ContentType MessageContentType `json:"content_type,omitempty"`
	Content     string             `json:"content"`
	Severity    MessageSeverity    `json:"severity"`
}

// MessageWarning is a warning-severity Message.
type MessageWarning struct {
	Path        string             `json:"path,omitempty"`
	Code        WarningCode        `json:"code"`
	Content     string             `json:"content"`
	ContentType MessageContentType `json:"content_type,omitempty"`
	// Presentation is the rendering contract; defaults to "notice" when absent.
	Presentation WarningPresentation `json:"presentation,omitempty"`
	ImageURL     string              `json:"image_url,omitempty"`
	URL          string              `json:"url,omitempty"`
}

// MessageInfo is an info-severity Message.
type MessageInfo struct {
	Path        string             `json:"path,omitempty"`
	Code        InfoCode           `json:"code,omitempty"`
	ContentType MessageContentType `json:"content_type,omitempty"`
	Content     string             `json:"content"`
}

// Message is a discriminated union over MessageError, MessageWarning, and
// MessageInfo, dispatched on the wire "type" field ("error", "warning",
// "info"). Exactly one of the As* accessors will report ok == true.
type Message struct {
	kind    string
	err     *MessageError
	warning *MessageWarning
	info    *MessageInfo
}

// NewErrorMessage wraps a MessageError as a Message.
func NewErrorMessage(m MessageError) Message { return Message{kind: "error", err: &m} }

// NewInfoMessage wraps a MessageInfo as a Message.
func NewInfoMessage(m MessageInfo) Message { return Message{kind: "info", info: &m} }

// AsError returns the wrapped MessageError, if this Message holds one.
func (m Message) AsError() (MessageError, bool) {
	if m.err == nil {
		return MessageError{}, false
	}
	return *m.err, true
}

// AsWarning returns the wrapped MessageWarning, if this Message holds one.
func (m Message) AsWarning() (MessageWarning, bool) {
	if m.warning == nil {
		return MessageWarning{}, false
	}
	return *m.warning, true
}

// AsInfo returns the wrapped MessageInfo, if this Message holds one.
func (m Message) AsInfo() (MessageInfo, bool) {
	if m.info == nil {
		return MessageInfo{}, false
	}
	return *m.info, true
}

// Type returns the wire discriminator value ("error", "warning", "info")
// for this Message.
func (m Message) Type() string { return m.kind }

// messageDiscriminator is used only to read the "type" field before
// dispatching to the concrete message type.
type messageDiscriminator struct {
	Type string `json:"type"`
}

// UnmarshalJSON dispatches on the "type" discriminator to populate the
// correct concrete message.
func (m *Message) UnmarshalJSON(data []byte) error {
	var disc messageDiscriminator
	if err := json.Unmarshal(data, &disc); err != nil {
		return fmt.Errorf("ucp: message: %w", err)
	}
	switch disc.Type {
	case "error":
		var e MessageError
		if err := json.Unmarshal(data, &e); err != nil {
			return fmt.Errorf("ucp: message: error: %w", err)
		}
		*m = Message{kind: "error", err: &e}
	case "warning":
		var w MessageWarning
		if err := json.Unmarshal(data, &w); err != nil {
			return fmt.Errorf("ucp: message: warning: %w", err)
		}
		*m = Message{kind: "warning", warning: &w}
	case "info":
		var i MessageInfo
		if err := json.Unmarshal(data, &i); err != nil {
			return fmt.Errorf("ucp: message: info: %w", err)
		}
		*m = Message{kind: "info", info: &i}
	default:
		return fmt.Errorf("ucp: message: unknown type discriminator %q", disc.Type)
	}
	return nil
}

// MarshalJSON round-trips the wrapped concrete message, re-adding the
// "type" discriminator field.
func (m Message) MarshalJSON() ([]byte, error) {
	switch m.kind {
	case "error":
		if m.err == nil {
			return nil, fmt.Errorf("ucp: message: kind is error but no MessageError set")
		}
		return marshalWithType(m.err, "error")
	case "warning":
		if m.warning == nil {
			return nil, fmt.Errorf("ucp: message: kind is warning but no MessageWarning set")
		}
		return marshalWithType(m.warning, "warning")
	case "info":
		if m.info == nil {
			return nil, fmt.Errorf("ucp: message: kind is info but no MessageInfo set")
		}
		return marshalWithType(m.info, "info")
	default:
		return nil, fmt.Errorf("ucp: message: empty or unknown kind %q", m.kind)
	}
}

// marshalWithType marshals v (a struct without a "type" field of its own
// on the Go side) and injects a "type" key set to typ into the resulting
// JSON object.
func marshalWithType(v any, typ string) ([]byte, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	typJSON, err := json.Marshal(typ)
	if err != nil {
		return nil, err
	}
	m["type"] = typJSON
	return json.Marshal(m)
}
