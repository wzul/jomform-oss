package httpapi

// Card t_28baedff — the renderer and the judge must agree on which fields are
// bookkeeping.
//
// The honeypot (`website`) and the render timestamp (`jf_ts`) are emitted into
// every public form page by antiSpamRenderFields, and the judge has to drop
// exactly those names before it asks the decision model. The two lists live in
// different packages — spamjudge cannot import httpapi (httpapi imports
// spamjudge) — so the literals are necessarily repeated, and this test is the
// join that keeps them from drifting.
//
// It BITES in the direction that costs money: renaming the honeypot in the
// renderer without teaching the judge to drop it means the new name is fed to
// the model as if it were an answer, which is precisely the defect this card
// fixes (two hidden lines moved a genuine Malay enquiry from 0.3739 to 0.5168).
// The reverse direction — a name dropped from the judge that the renderer no
// longer emits — is equally refused, because it silently deletes a real field
// from the judged state if a merchant ever uses that id.

import (
	"regexp"
	"sort"
	"testing"

	"github.com/wzul/jomform/api/internal/spamjudge"
)

// TestRendereredAntiSpamFieldsAreExactlyTheJudgesBookkeeping extracts the
// `name` of every control antiSpamRenderFields emits and requires that set to be
// exactly spamjudge.BookkeepingFields.
func TestRendereredAntiSpamFieldsAreExactlyTheJudgesBookkeeping(t *testing.T) {
	html := antiSpamRenderFields()
	re := regexp.MustCompile(`name="([^"]+)"`)
	var rendered []string
	for _, m := range re.FindAllStringSubmatch(html, -1) {
		rendered = append(rendered, m[1])
	}
	if len(rendered) == 0 {
		t.Fatalf("antiSpamRenderFields emitted no named controls, so this guard would pass vacuously:\n%s", html)
	}
	sort.Strings(rendered)

	dropped := append([]string(nil), spamjudge.BookkeepingFields...)
	sort.Strings(dropped)

	if len(rendered) != len(dropped) {
		t.Fatalf("renderer emits %v as hidden anti-spam controls but the judge drops %v — the two "+
			"lists must match exactly (a control the judge does not drop is sent to the model as "+
			"an answer)", rendered, dropped)
	}
	for i := range dropped {
		if rendered[i] != dropped[i] {
			t.Fatalf("renderer emits %v as hidden anti-spam controls but the judge drops %v", rendered, dropped)
		}
	}
	// The named constants are what the submit path reads, so they must be the
	// rendered names too — otherwise the honeypot is checked by the submit path
	// and the timestamp by nobody.
	for _, want := range []string{antiSpamHoneypotField, antiSpamRenderField} {
		if !spamjudge.IsBookkeepingField(want) {
			t.Errorf("%q is a submit-path anti-spam constant but the judge would send it to the model", want)
		}
	}
}
