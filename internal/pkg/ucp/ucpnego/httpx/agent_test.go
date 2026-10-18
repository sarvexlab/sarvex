// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package httpx

import "testing"

func TestParseUCPAgent(t *testing.T) {
	tests := []struct {
		name    string
		header  string
		wantURL string
		wantErr bool
	}{
		{
			name:    "simple",
			header:  `profile="https://platform.example/.well-known/ucp"`,
			wantURL: "https://platform.example/.well-known/ucp",
		},
		{
			name:    "with whitespace",
			header:  `  profile = "https://platform.example/profile.json"  `,
			wantURL: "https://platform.example/profile.json",
		},
		{
			name:    "with extras",
			header:  `profile="https://platform.example/p", region="au"`,
			wantURL: "https://platform.example/p",
		},
		{name: "empty header", header: "", wantErr: true},
		{name: "no profile member", header: `region="au"`, wantErr: true},
		{name: "non-https rejected", header: `profile="http://platform.example/p"`, wantErr: true},
		{name: "unquoted value", header: `profile=https://platform.example/p`, wantErr: true},
		{name: "malformed", header: `profile`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseUCPAgent(tt.header)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if err == nil && got.ProfileURL != tt.wantURL {
				t.Errorf("got %q, want %q", got.ProfileURL, tt.wantURL)
			}
		})
	}
}

func TestFormatUCPAgent(t *testing.T) {
	got, err := FormatUCPAgent("https://platform.example/.well-known/ucp")
	if err != nil {
		t.Fatal(err)
	}
	want := `profile="https://platform.example/.well-known/ucp"`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if _, err := FormatUCPAgent("http://insecure"); err == nil {
		t.Error("expected error for http://")
	}
}
