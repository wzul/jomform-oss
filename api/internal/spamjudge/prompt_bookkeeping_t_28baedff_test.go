package spamjudge

// Card t_28baedff — anti-spam bookkeeping must not reach the judge.
//
// The public renderer emits two hidden controls into every form page: the
// `website` honeypot and the `jf_ts` render timestamp. They ride the same
// `fields` map the buyer's answers arrive in, so they were stored on the
// submission and handed to the decision model as if they were answers.
//
// Measured on the live judge (card t_28baedff, clef via /v1/systemone): the same
// genuine Malay enquiry scored 0.3739 bare, 0.5648 with `jf_ts`, 0.5816 with an
// EMPTY `website`, and 0.5168 with both — over the 0.5 default, which is what got
// a real submission flagged and, unreviewed, deleted by the retention sweep.

import (
	"strings"
	"testing"
)

// genuineBMMessage is the exact text of prod submission 77319ed1 (the false
// positive) — the sample the fix exists for.
const genuineBMMessage = "Saya ingin bertanya tentang kelas mengaji untuk anak saya yang berumur 8 tahun. " +
	"Boleh hubungi saya pada waktu pejabat."

// TestBuildPromptDropsAntiSpamBookkeeping is the primary guard. It asserts the
// BYTES of the judged state, not a substring: a prompt that leaks `jf_ts` or the
// honeypot under some other formatting is still a prompt that moves the verdict.
//
// The strongest assertion is equality with the bare prompt: it says the
// bookkeeping cannot change the judged state AT ALL, which is the property that
// was missing. The per-name checks exist so a failure names what leaked.
func TestBuildPromptDropsAntiSpamBookkeeping(t *testing.T) {
	bare := Job{FormTitle: "cuba", Data: map[string]any{"xg43aok": genuineBMMessage}}
	framed := Job{FormTitle: "cuba", Data: map[string]any{
		"xg43aok": genuineBMMessage,
		// The real prod payload: the renderer's two hidden controls, with the
		// honeypot EMPTY (which is what an honest browser sends).
		"jf_ts":   "1793037169",
		"website": "",
	}}

	got := buildPrompt(framed)
	for _, leaked := range []string{"jf_ts", "1793037169", "website"} {
		if strings.Contains(got, leaked) {
			t.Errorf("judged state carries the renderer's bookkeeping %q:\n%s", leaked, got)
		}
	}
	if !strings.Contains(got, genuineBMMessage) {
		t.Errorf("the sender's answer was dropped along with the bookkeeping:\n%s", got)
	}
	if want := buildPrompt(bare); got != want {
		t.Errorf("the bookkeeping changed the judged state.\nbare:\n%s\nframed:\n%s", want, got)
	}
}

// TestJudgeableDataDropsOnlyTheRenderersOwnFields pins the filter's edges:
// an EXACT name match, not a prefix or substring, so a real answer whose field
// id merely resembles a control ("website_url") is still judged; the caller's
// map is left alone (the submission keeps its raw fields); and a value that
// happens to read like a control under a real answer key is still an answer.
func TestJudgeableDataDropsOnlyTheRenderersOwnFields(t *testing.T) {
	in := map[string]any{
		"website":     "",
		"jf_ts":       "1793037169",
		"website_url": "https://contoh.test",
		"jf_ts_note":  "arrived late",
		"b1":          "Saya ingin bertanya",
	}
	out := JudgeableData(in)

	for _, dropped := range []string{"website", "jf_ts"} {
		if _, ok := out[dropped]; ok {
			t.Errorf("bookkeeping %q survived into the judged data: %v", dropped, out)
		}
	}
	for _, kept := range []string{"website_url", "jf_ts_note", "b1"} {
		if _, ok := out[kept]; !ok {
			t.Errorf("a real answer %q was dropped from the judged data: %v", kept, out)
		}
	}
	if len(in) != 5 {
		t.Fatalf("JudgeableData mutated the caller's map: %v", in)
	}
	if v, ok := JudgeableData(map[string]any{"b1": "jf_ts"})["b1"]; !ok || v != "jf_ts" {
		t.Errorf("a control-shaped VALUE under an answer key was dropped: %v", v)
	}
}

// TestBuildPromptAllBookkeepingRendersNone keeps the filter from producing a
// dangling "Submitted field values:" header with no values: a job whose only
// fields are bookkeeping (a renderer-only submit, or a client that sent nothing
// else) reads exactly like a job with no fields at all.
func TestBuildPromptAllBookkeepingRendersNone(t *testing.T) {
	p := buildPrompt(Job{FormTitle: "cuba", Data: map[string]any{"jf_ts": "1", "website": ""}})
	if !strings.Contains(p, "(none)") {
		t.Errorf("a bookkeeping-only job should render no field values:\n%s", p)
	}
	if strings.Contains(p, "- jf_ts") || strings.Contains(p, "- website") {
		t.Errorf("bookkeeping rendered as an answer:\n%s", p)
	}
}
