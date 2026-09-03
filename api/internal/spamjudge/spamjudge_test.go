package spamjudge

import (
	"strings"
	"testing"
)

// TestBuildPromptIncludesContext verifies the classification prompt carries
// the field values, email, IP and form context so the model can judge.
func TestBuildPromptIncludesContext(t *testing.T) {
	p := buildPrompt(Job{
		FormTitle: "Contact form",
		Email:     "buyer@example.test",
		IP:        "203.0.113.7",
		Data:      map[string]any{"b1": "Ali", "b2": "ali@example.test", "b3": "Great form"},
	})
	for _, want := range []string{"Contact form", "buyer@example.test", "203.0.113.7", "b1: Ali", "b3: Great form", "SPAM or LEGIT"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

// TestBuildPromptEmptyData verifies an empty data map is handled gracefully.
func TestBuildPromptEmptyData(t *testing.T) {
	p := buildPrompt(Job{FormTitle: "F", Data: map[string]any{}})
	if !strings.Contains(p, "(none)") {
		t.Errorf("prompt should note no field values")
	}
}
