package formfields

// A `website` field (the builder block, and a custom-HTML form's registered
// `type:"url"` field, which maps onto it) used to accept only http(s) — while
// the page the buyer fills in is an `<input type="url">`, whose own constraint
// is "the value parses as an absolute URL". So the browser said
// ftp://example.com/x was fine and the server answered 400 `URL tidak sah`.
//
// These cases are the browser's measured verdicts (Chrome 145, quoted in
// ValidWebsiteURL's doc comment), so the rule cannot silently drift back to a
// prefix check.

import (
	"strings"
	"testing"
)

func TestValidWebsiteURLFollowsTheBrowser(t *testing.T) {
	// accepted: every value the browser's <input type=url> reports valid, plus
	// the two shapes that are valid under the same rule (an uppercase scheme and
	// surrounding whitespace, which the browser's value sanitizer strips).
	accepted := []string{
		"https://example.com/x",
		"http://example.com",
		"HTTPS://EXAMPLE.COM/x",
		"  https://example.com/x  ",
		"https://example.com/x?q=1#frag",
		"ftp://example.com/x", // the value this card is about
		"ftp://example.com",
		"ws://example.com",
		"wss://example.com/socket",
		"a://b",              // a custom scheme with a host
		"mailto:a@b",         // absolute URL, no host — the browser accepts it
		"tel:+60123456789",   // ditto
		"file:///etc/passwd", // ditto
		"urn:isbn:0451450523",
	}
	for _, v := range accepted {
		if !ValidWebsiteURL(v) {
			t.Errorf("ValidWebsiteURL(%q) = false, want true — the browser accepts it in <input type=url>", v)
		}
	}

	// rejected: the values the browser ALSO refuses (scheme-less, no host) …
	rejected := []string{
		"",
		"   ",
		"jomform.com",     // the mistake this check exists to catch
		"example.com/x",   // ditto
		"//example.com/x", // scheme-relative: no scheme
		"http://",         // no host
		"ftp://",          // no host
		"https://",        // no host
		"ws://",           // no host
		"...",
	}
	for _, v := range rejected {
		if ValidWebsiteURL(v) {
			t.Errorf("ValidWebsiteURL(%q) = true, want false", v)
		}
	}

	// … and the deliberate divergences: values a WHATWG parser accepts but this
	// check refuses. The first group is the security rule (never store an
	// executable URI as an answer); the second is Go's parser being stricter
	// than the browser's on two malformed shapes, which is acceptable — the
	// server is not a second URL parser, and garbage gets a corrective message.
	for _, v := range []string{
		"javascript:alert(1)",
		"JavaScript:alert(1)",
		" javascript:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"vbscript:msgbox(1)",
		"http:/example.com",   // one slash: a browser parses it, Go refuses
		"http://exa mple.com", // a space in the host: ditto
	} {
		if ValidWebsiteURL(v) {
			t.Errorf("ValidWebsiteURL(%q) = true, want false — see the divergences documented on ValidWebsiteURL", v)
		}
	}
}

// The submit path is what a buyer actually hits: the same values must come back
// as a contract violation naming the field, and the browser-valid ones must pass.
func TestValidateFieldsWebsiteAcceptsWhatTheBrowserAccepts(t *testing.T) {
	regs := []Field{{FieldID: "site", Type: "website", Label: "Website"}}

	if err := ValidateFields(regs, map[string]string{"site": "ftp://example.com/x"}); err != nil {
		t.Errorf("browser-valid ftp:// answer rejected: %v", err)
	}
	if err := ValidateFields(regs, map[string]string{"site": "https://jomform.com"}); err != nil {
		t.Errorf("https answer rejected: %v", err)
	}
	err := ValidateFields(regs, map[string]string{"site": "jomform.com"})
	if err == nil {
		t.Fatal("scheme-less value passed a website field")
	}
	if !strings.Contains(err.Error(), "URL tidak sah") || !strings.Contains(err.Error(), "Website") {
		t.Errorf("error = %q, want it to name the field", err.Error())
	}
	if err := ValidateFields(regs, map[string]string{"site": "javascript:alert(1)"}); err == nil {
		t.Error("javascript: URL passed a website field")
	}
	// An empty/absent optional field is still not an error — the browser's url
	// input accepts the empty string unless `required` is set.
	if err := ValidateFields(regs, map[string]string{}); err != nil {
		t.Errorf("absent optional website field rejected: %v", err)
	}
}
