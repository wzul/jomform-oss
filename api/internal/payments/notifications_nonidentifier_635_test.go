package payments

// Issue #635: renderNotification PANICKED when a submitted block id was not a
// valid text/template identifier, and that panic escaped the notification
// engine, hit the HTTP recoverer, and turned a public form submission into a
// 500 on production (`event-registration`, log line
// `panic_recovered err="function name \"4nwz0vg\" is not a valid identifier"`).
//
// Why the shipped #597 suite was green: every fixture in it used the synthetic
// ids `b1`/`b2`, which are valid identifiers, so the FuncMap loop never saw a
// bad name. The builder mints ids with
// `Math.random().toString(36).slice(2, 9)` (web/src/app/builder/page.tsx), which
// is base36 — an id leads with a digit about 10 times in 36, and the production
// form carried exactly such an id.
//
// The tests here are the shapes that bite on the pre-fix source:
//
//  1. TestTemplateFuncNameMatchesStdlibGoodName pins the restated identifier
//     rule against the stdlib's OWN behaviour (does Funcs panic on this key?),
//     so the guard cannot drift from text/template.
//  2. TestNotificationWithDigitLeadingBlockIDDoesNotPanic renders a digit-leading
//     id and a real base36 id. Pre-fix this test does not merely fail, it PANICS
//     out of renderNotification — which is the production symptom.
//  3. TestRenderNotificationTemplateRecoversFromFuncMapPanic pins the fallback
//     itself: a template that cannot render must come back as the raw source
//     (the function's documented contract), never as a panic.
//
// Run with: cd api && go test ./internal/payments/ -count=1
import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"text/template"
)

// stdlibFuncsPanics reports whether text/template panics when name is used as a
// funcs() key. It is the authority this package's own check is pinned against:
// the stdlib's `goodName` is unexported, so the only honest way to state "we
// skip exactly what the stdlib refuses" is to ask the stdlib.
func stdlibFuncsPanics(name string) (panicked bool) {
	defer func() {
		if recover() != nil {
			panicked = true
		}
	}()
	_, _ = template.New("probe").
		Funcs(template.FuncMap{name: func() string { return "" }}).
		Parse("")
	return false
}

// TestTemplateFuncNameMatchesStdlibGoodName is the drift guard. Its table mixes
// the production shapes (a digit-leading base36 id) with the valid ones it must
// keep (so a future "skip everything" over-correction is caught too) and the
// shapes that are invalid for a different reason (a hyphen, a space, a dot, a
// leading underscore is VALID, empty is not).
//
// Bite proof: change the rule to `return true` (skip nothing) or `return false`
// (skip everything) and this test goes RED naming the disagreement — see the
// mutation table in the commit.
func TestTemplateFuncNameMatchesStdlibGoodName(t *testing.T) {
	cases := []struct {
		name    string
		valid   bool
		note    string
	}{
		{"b1", true, "the synthetic id every #597 fixture uses — must keep working"},
		{"b_1", true, "underscore and digits inside an id are fine"},
		{"_lead", true, "a leading underscore IS a valid identifier"},
		{"jwms35t", true, "a base36 id that happens to start with a letter"},
		{"4nwz0vg", false, "the id in the production panic — base36, leading digit"},
		{"9e25ae7", false, "second id from the issue's repro"},
		{"0b", false, "leading digit"},
		{"1", false, "a bare digit is a number, not an identifier"},
		{"my-field", false, "hyphen is not allowed in a template identifier"},
		{"my field", false, "space is not allowed"},
		{"a.b", false, "dot is field access, not a name"},
		{"", false, "an empty key panics the stdlib"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isTemplateFuncName(tc.name)
			if got != tc.valid {
				t.Errorf("isTemplateFuncName(%q) = %v, want %v (%s)", tc.name, got, tc.valid, tc.note)
			}
			// The pin: our answer must agree with the stdlib on every input.
			// A name we KEEP must not panic the stdlib; a name we SKIP must be
			// one the stdlib would refuse (or an empty key, which it also
			// refuses).
			if want := !stdlibFuncsPanics(tc.name); got != want {
				t.Errorf("isTemplateFuncName(%q) = %v but text/template Funcs panics=%v — "+
					"the guard has drifted from the stdlib", tc.name, got, !want)
			}
		})
	}
}

// TestNotificationWithDigitLeadingBlockIDDoesNotPanic is the regression proper.
//
// It renders a notification whose submitted fields map carries the production
// id `4nwz0vg` (and a second base36 id) alongside valid ones. On the pre-fix
// source the digit-leading key reaches `funcs[k] = ...` and text/template
// panics with `function name "4nwz0vg" is not a valid identifier` — the exact
// production error — and the panic propagates out of this test.
func TestNotificationWithDigitLeadingBlockIDDoesNotPanic(t *testing.T) {
	ref := NotificationRef{
		BuyerName: "Siti Aminah",
		FormURL:   "https://wan.jomform.com/event-registration",
		Fields: map[string]string{
			// The shape the builder produces, and the id from the incident.
			"4nwz0vg": "Nasi lemak, tanpa sambal",
			"9e25ae7": "Meja 8",
			// A valid id submitted in the same request must still resolve.
			"b1":    "Siti Aminah",
			"email": "siti@example.test",
		},
	}
	subject := "Pendaftaran baharu"
	body := "Nama: {{full_name}}\nEmel: {{email}}\nCatatan: {{b1}}\nBorang: {{form_url}}"

	// Pre-fix this call panics; the assignment is what the panic would abort.
	gotSubj, gotBody := renderNotification(subject, body, ref)

	if gotSubj != subject {
		t.Errorf("subject = %q, want %q (a digit-leading field id must not disturb "+
			"the rest of the render)", gotSubj, subject)
	}
	for _, want := range []string{"Siti Aminah", "siti@example.test",
		"https://wan.jomform.com/event-registration"} {
		if !strings.Contains(gotBody, want) {
			t.Errorf("rendered body is missing %q:\n%s", want, gotBody)
		}
	}
	// The skip must be a SKIP, not a blanket disable: a valid block id still
	// renders its submitted value.
	if !strings.Contains(gotBody, "Catatan: Siti Aminah") {
		t.Errorf("the valid block id {{b1}} did not render its value — the fix "+
			"skipped more than the unaddressable ids:\n%s", gotBody)
	}
}

// TestRenderNotificationTemplateRecoversFromFuncMapPanic pins the fallback
// itself, independently of the skip. #635's root cause is that text/template
// panics from `Funcs` — BEFORE Parse — so neither the parse-error fallback nor
// the execute-error fallback could see it, and the function's documented
// promise ("a bad merchant template can never block a notification", and here
// "never block the submit that triggered it") was false for a caller that hands
// it a bad funcs map.
//
// Bite proof: delete the `defer recover` and this test does not fail, it PANICS
// out of the test binary.
func TestRenderNotificationTemplateRecoversFromFuncMapPanic(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	// A funcs map with a key text/template refuses — the pre-fix shape.
	bad := template.FuncMap{"4nwz0vg": func() string { return "x" }}
	src := "Pendaftaran {{4nwz0vg}} diterima"

	got := renderNotificationTemplate(src, bad, notificationData{})
	if got != src {
		t.Errorf("renderNotificationTemplate with a panicking funcs map = %q, want the "+
			"raw source %q (the documented fallback)", got, src)
	}
	if !strings.Contains(logs.String(), "notify_render_panic_recovered") {
		t.Errorf("the recovered panic was not logged — a silent fallback hides the "+
			"next occurrence; log:\n%s", logs.String())
	}

	// And the normal path still renders, so the recover did not swallow the
	// function's actual job.
	ok := template.FuncMap{"b1": func() string { return "Siti" }}
	if got := renderNotificationTemplate("Nama: {{b1}}", ok, notificationData{}); got != "Nama: Siti" {
		t.Errorf("a valid template stopped rendering: %q", got)
	}
}
