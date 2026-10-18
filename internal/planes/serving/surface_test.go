// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package serving

import "testing"

// TestDeriveSurface_ClassificationMatrix is A4: the classification matrix
// for every case deriveSurface can actually distinguish today. See
// Surface's doc comment (lifecycle_contract.go) for why
// SurfaceOpenAIChatGPT is never produced here — no signal in this
// codebase distinguishes ChatGPT/ACP traffic from any other non-Google
// agent, so a non-empty, unrecognized host is honestly classified
// SurfaceOtherAgent, not guessed as ChatGPT.
func TestDeriveSurface_ClassificationMatrix(t *testing.T) {
	cases := []struct {
		name          string
		agentPlatform string
		want          Surface
	}{
		{"no header at all", "", SurfaceUnknown},
		{"gemini host", "gemini.google.com", SurfaceGoogleGemini},
		{"gemini mixed case", "Gemini.Google.COM", SurfaceGoogleGemini},
		{"google host without gemini", "aimode.google.com", SurfaceGoogleAIMode},
		{"bare google.com", "google.com", SurfaceGoogleAIMode},
		{"unrecognized non-google agent", "some-other-agent.example.com", SurfaceOtherAgent},
		{"chatgpt-looking host still classified other_agent, never guessed", "chatgpt.com", SurfaceOtherAgent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := deriveSurface(tc.agentPlatform)
			if got != tc.want {
				t.Errorf("deriveSurface(%q) = %q, want %q", tc.agentPlatform, got, tc.want)
			}
		})
	}
}

// TestSurfaceEnum_HasFiveMembers is R8: the closed enum's five members
// exist and are distinct strings, including the two added for the
// Channel Dashboard's multi-surface requirement.
func TestSurfaceEnum_HasFiveMembers(t *testing.T) {
	members := map[Surface]bool{
		SurfaceGoogleAIMode:  true,
		SurfaceGoogleGemini:  true,
		SurfaceOpenAIChatGPT: true,
		SurfaceOtherAgent:    true,
		SurfaceUnknown:       true,
	}
	if len(members) != 5 {
		t.Fatalf("expected 5 distinct Surface members, got %d", len(members))
	}
}
