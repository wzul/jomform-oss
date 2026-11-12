package httpapi

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// Issue #360: the customizable landing page template renders the merchant's
// config (hero, subtitle, body, theme colors, CTA links) as server-side HTML.
func TestWorkspaceLandingPageTemplateRenders(t *testing.T) {
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
	data := workspaceLandingData{Slug: "ali-siti", Lp: lp, Theme: landingThemeFrom(lp), HasForms: true}
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
// as a landing page — the fallthrough in handleWorkspaceRoot serves the listing.
func TestWorkspaceLandingPageTemplateDisabledShowsNoFormsHint(t *testing.T) {
	lp := &dbstore.LandingPage{Enabled: true, Hero: "Only hero"}
	data := workspaceLandingData{Slug: "s", Lp: lp, Theme: landingThemeFrom(lp), HasForms: false}
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
func TestWorkspaceLandingPageTemplateEscapesBody(t *testing.T) {
	lp := &dbstore.LandingPage{
		Enabled: true,
		Hero:    "<script>alert(1)</script>",
		Body:    "<img src=x onerror=alert(2)>",
	}
	data := workspaceLandingData{Slug: "s", Lp: lp, Theme: landingThemeFrom(lp), HasForms: true}
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

// Issue #362: a landing page configured with embedded published forms renders
// each as a same-origin iframe to /{slug}; unknown/draft/private slugs are
// dropped server-side.
func TestWorkspaceLandingPageEmbedsPublishedForms(t *testing.T) {
	lp := &dbstore.LandingPage{
		Enabled: true,
		Hero:    "Ali & Siti wedding",
		Forms: []dbstore.LandingEmbed{
			{Label: "Reply to the RSVP", FormSlug: "rsvp"},
			{Label: "Dinner choice", FormSlug: "dinner"},
		},
	}
	published := []*dbstore.Form{
		{Slug: "rsvp"},
		{Slug: "dinner"},
		{Slug: "hotel"},
	}
	embeds := resolveLandingEmbeds(lp.Forms, published)
	if len(embeds) != 2 {
		t.Fatalf("expected 2 embeds (rsvp + dinner), got %d", len(embeds))
	}
	data := workspaceLandingData{Slug: "ali-siti", Lp: lp, Theme: landingThemeFrom(lp), HasForms: true, Embeds: embeds}
	var buf bytes.Buffer
	if err := landingPageTmpl.Execute(&buf, data); err != nil {
		t.Fatalf("landingPageTmpl.Execute: %v", err)
	}
	body := buf.String()
	for _, want := range []string{
		`<iframe src="/rsvp"`, `title="Reply to the RSVP"`,
		`<iframe src="/dinner"`, `title="Dinner choice"`, `Reply to the RSVP`, `Dinner choice`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("landing page embed output missing %q\nbody:\n%s", want, body)
		}
	}
	// The non-embedded published form must NOT appear as an embed.
	if strings.Contains(body, `<iframe src="/hotel"`) {
		t.Errorf("landing page embedded a form that was not configured (/hotel)")
	}
}

// Issue #362: embedded-form blocks whose slug is not in the published set are
// filtered out by resolveLandingEmbeds so nothing renders for a draft/private/
// unknown slug.
func TestResolveLandingEmbedsDropsUnknownSlugs(t *testing.T) {
	lp := &dbstore.LandingPage{
		Enabled: true,
		Forms: []dbstore.LandingEmbed{
			{Label: "draft", FormSlug: "not-published"},
			{Label: "ok", FormSlug: "rsvp"},
		},
	}
	published := []*dbstore.Form{{Slug: "rsvp"}}
	embeds := resolveLandingEmbeds(lp.Forms, published)
	if len(embeds) != 1 || embeds[0].FormSlug != "rsvp" {
		t.Fatalf("expected only the published 'rsvp' embed, got %+v", embeds)
	}
	// Rendering must not include the dropped block.
	data := workspaceLandingData{Slug: "s", Lp: lp, Theme: landingThemeFrom(lp), HasForms: true, Embeds: embeds}
	var buf bytes.Buffer
	if err := landingPageTmpl.Execute(&buf, data); err != nil {
		t.Fatalf("landingPageTmpl.Execute: %v", err)
	}
	if strings.Contains(buf.String(), "not-published") || strings.Contains(buf.String(), "draft") {
		t.Errorf("landing page rendered a form slug that is not published")
	}
}

// Issue #362: with no embedded forms configured, no embed section is rendered.
func TestWorkspaceLandingPageNoEmbedSectionWhenNoneConfigured(t *testing.T) {
	lp := &dbstore.LandingPage{Enabled: true, Hero: "Only hero"}
	data := workspaceLandingData{Slug: "s", Lp: lp, Theme: landingThemeFrom(lp), HasForms: true}
	var buf bytes.Buffer
	if err := landingPageTmpl.Execute(&buf, data); err != nil {
		t.Fatalf("landingPageTmpl.Execute: %v", err)
	}
	body := buf.String()
	if strings.Contains(body, "class=\"embeds\"") {
		t.Errorf("landing page rendered an embeds section with no embedded forms configured")
	}
	if strings.Contains(body, "<iframe") {
		t.Errorf("landing page rendered an <iframe> with no embedded forms configured")
	}
}
