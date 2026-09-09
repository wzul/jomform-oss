package httpapi

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// Issue #360: the customizable landing page template renders the merchant's
// config (hero, subtitle, body, theme colors, CTA links) as server-side HTML.
func TestSpaceLandingPageTemplateRenders(t *testing.T) {
	lp := &dbstore.LandingPage{
		Enabled:         true,
		Hero:            "RSVP for Ali & Siti",
		Subtitle:        "1 August 2026 · Kuala Lumpur",
		Body:            "Join us for the biggest day of our lives.",
		ImageURL:        "https://cdn.example.com/banner.jpg",
		PrimaryColor:    "#7c3aed",
		BackgroundColor: "#faf5ff",
		LogoURL:         "https://cdn.example.com/logo.png",
		CTAs: []dbstore.LandingCTA{
			{Label: "RSVP now", FormSlug: "rsvp"},
			{Label: "Hotel info", FormSlug: "hotel"},
		},
	}
	data := spaceLandingData{Slug: "ali-siti", Lp: lp, HasForms: true}
	var buf bytes.Buffer
	if err := landingPageTmpl.Execute(&buf, data); err != nil {
		t.Fatalf("landingPageTmpl.Execute: %v", err)
	}
	body := buf.String()
	for _, want := range []string{
		"RSVP for Ali &amp; Siti", // HTML-escaped
		"1 August 2026 · Kuala Lumpur",
		"Join us for the biggest day of our lives.",
		"https://cdn.example.com/banner.jpg",
		"https://cdn.example.com/logo.png",
		"#7c3aed",
		"#faf5ff",
		`href="/rsvp"`,
		`href="/hotel"`,
		"RSVP now",
		"Hotel info",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("landing page output missing %q\nbody:\n%s", want, body)
		}
	}
	// The "no forms yet" hint must NOT show when forms exist.
	if strings.Contains(body, "Belum ada borang") {
		t.Errorf("landing page shows 'Belum ada borang' hint despite having forms")
	}
}

// Issue #360: a disabled landing page (or one with no config) must not render
// as a landing page — the fallthrough in handleSpaceRoot serves the listing.
func TestSpaceLandingPageTemplateDisabledShowsNoFormsHint(t *testing.T) {
	lp := &dbstore.LandingPage{Enabled: true, Hero: "Only hero"}
	data := spaceLandingData{Slug: "s", Lp: lp, HasForms: false}
	var buf bytes.Buffer
	if err := landingPageTmpl.Execute(&buf, data); err != nil {
		t.Fatalf("landingPageTmpl.Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "Belum ada borang") {
		t.Errorf("landing page with no forms should show 'Belum ada borang' hint")
	}
	if strings.Contains(buf.String(), `"rsvp"`) {
		t.Errorf("no CTA rendered when none configured")
	}
}

// Issue #360: the body is escaped server-side to prevent stored XSS from a
// merchant-authored body value.
func TestSpaceLandingPageTemplateEscapesBody(t *testing.T) {
	lp := &dbstore.LandingPage{
		Enabled: true,
		Hero:    "<script>alert(1)</script>",
		Body:    "<img src=x onerror=alert(2)>",
	}
	data := spaceLandingData{Slug: "s", Lp: lp, HasForms: true}
	var buf bytes.Buffer
	if err := landingPageTmpl.Execute(&buf, data); err != nil {
		t.Fatalf("landingPageTmpl.Execute: %v", err)
	}
	body := buf.String()
	for _, banned := range []string{"<script>alert(1)</script>", "<img src=x onerror=alert(2)>"} {
		if strings.Contains(body, banned) {
			t.Errorf("landing page emitted unescaped %q (XSS)", banned)
		}
	}
	if !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Errorf("hero not HTML-escaped")
	}
}
