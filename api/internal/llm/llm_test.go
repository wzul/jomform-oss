package llm

import (
	"strings"
	"testing"
)

// TestBuildPromptFillsBusinessDetails verifies the prompt template fills in
// the merchant's business details and leaves neutral placeholders for missing
// ones.
func TestBuildPromptFillsBusinessDetails(t *testing.T) {
	p := buildPrompt("privacy", BusinessDetails{
		BusinessName:   "Acme Sdn Bhd",
		RegistrationNo: "202001012345",
		Contact:        "store@acme.my",
		DataCollected:  "name, email, phone",
	})
	for _, want := range []string{"Privacy Policy", "Acme Sdn Bhd", "202001012345", "store@acme.my", "name, email, phone"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	// Missing fields become neutral placeholders.
	if !strings.Contains(p, "[not provided") {
		t.Errorf("prompt should include a neutral placeholder for missing details")
	}
}

// TestBuildPromptPageTypes verifies each page type produces the right title
// and section-specific field.
func TestBuildPromptPageTypes(t *testing.T) {
	cases := []struct {
		pageType string
		title    string
		field    string
	}{
		{"privacy", "Privacy Policy", "What data is collected"},
		{"terms", "Terms of Service", "Payment terms"},
		{"refund", "Refund Policy", "Refund terms"},
	}
	for _, c := range cases {
		p := buildPrompt(c.pageType, BusinessDetails{})
		if !strings.Contains(p, c.title) {
			t.Errorf("%s: missing title %q", c.pageType, c.title)
		}
		if !strings.Contains(p, c.field) {
			t.Errorf("%s: missing field %q", c.pageType, c.field)
		}
	}
}
