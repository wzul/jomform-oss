package chat

// The confirmation card's ONE model-authored sentence follows the DASHBOARD
// language (card t_ec87d6c7).
//
// THE DEFECT, measured on staging before this change (English dashboard, the
// merchant asked in Malay):
//
//	NEEDS YOUR CONFIRMATION
//	Create or update form
//	A new form will be CREATED (or the existing one updated, …)
//	Action        Create a new form
//	Title         Customer Feedback
//	Configuration 1 field
//	Borang baharu "Customer Feedback" dengan satu medan teks panjang untuk
//	mesej pelanggan, tanpa medan bayaran.        ← the model's `reason`, in Malay
//	upsert_form
//
// The card's chrome, title, summary, labels and values were already dictionary
// keys (t_30ffa28a); the ONE string on it that is prose is propose_write's
// `reason`, which the model writes and the card prints under its fields. The
// prompt called that "the language of the conversation", so the sentence followed
// the MESSAGE, not the card — and the card is the surface where a merchant is
// asked to approve a change, so half of it in a language they did not choose is
// exactly Wan's rule ("if the UI is English, this cannot be Malay").
//
// THE FIX IS IN THE PROMPT, because the sentence does not exist until the model
// writes it: there is nothing to look up in the dictionary. The dashboard
// language (users.lang) rides the REQUEST and is interpolated into the system
// prompt, and the model is told the card is dashboard UI — the reply keeps
// following the merchant's message, only the card follows the dashboard.
//
// WHAT THIS FILE PINS:
//
//  1. The prompt NAMES the dashboard language, for every profile code, and falls
//     back to English for an unset/unknown one (the dashboard's own default).
//  2. The prompt says the card's reason follows the dashboard, and no longer says
//     it follows the conversation — the sentence that produced the defect.
//  3. The language actually REACHES the model: a Runner given a context with
//     Malay hands the model a system turn naming Malay (a prompt helper that
//     nobody passes the language to would pass tests 1 and 2 and still ship the
//     bug).
//  4. propose_write's own `reason` description says the same thing, because the
//     model reads its tool definitions as instructions.

import (
	"context"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/llm"
)

// TestDashboardLangNameResolvesTheFiveLocalesAndDefaultsToEnglish pins the code →
// prompt-name mapping, including the two cases that are not a locale at all:
// "" (the column is unset until a merchant picks a language) and a value the
// dashboard does not know.
func TestDashboardLangNameResolvesTheFiveLocalesAndDefaultsToEnglish(t *testing.T) {
	for code, want := range map[string]string{
		"en": "English", "ms": "Malay", "zh": "Chinese", "ta": "Tamil", "ar": "Arabic",
		// Case and stray whitespace come from a column an operator can edit.
		"MS": "Malay", " ar ": "Arabic",
		// Not a dashboard locale: the dashboard renders English in both cases.
		"": "English", "fr": "English", "xx": "English",
	} {
		if got := dashboardLangName(code); got != want {
			t.Errorf("dashboardLangName(%q) = %q, want %q", code, got, want)
		}
	}
}

// TestSystemPromptNamesTheDashboardLanguageForTheCard is the text half: the
// system prompt must state which language the card is displayed in, for each of
// the five locales, and the CARD LANGUAGE section must be the thing that says so.
func TestSystemPromptNamesTheDashboardLanguageForTheCard(t *testing.T) {
	for code, want := range map[string]string{
		"en": "English", "ms": "Malay", "zh": "Chinese", "ta": "Tamil", "ar": "Arabic",
	} {
		prompt := systemPromptText(code)
		section := cardLanguageSection(prompt)
		if section == "" {
			t.Fatalf("the system prompt has no CARD LANGUAGE section for %q — the model is never "+
				"told which language the card it is filling in is displayed in, and it will write "+
				"propose_write's reason in whatever language it thinks the conversation is in "+
				"(the defect this card fixes)", code)
		}
		if !strings.Contains(section, want) {
			t.Errorf("the CARD LANGUAGE section for %q does not name %q:\n%s", code, want, section)
		}
		if strings.Contains(prompt, "{{DASHBOARD_LANG}}") {
			t.Errorf("the prompt still carries the raw {{DASHBOARD_LANG}} placeholder for %q — the "+
				"model would be told to write in the literal string", code)
		}
	}

	// The two prompts must not be identical, or the interpolation is cosmetic.
	if systemPromptText("ms") == systemPromptText("en") {
		t.Error("the Malay and English prompts are byte-identical — the dashboard language is not " +
			"reaching the prompt text at all")
	}
}

// TestSystemPromptSaysTheCardFollowsTheDashboardNotTheConversation pins the RULE,
// and pins it against the sentence that shipped the defect: the old prompt said
// the reason is written "in the language of the conversation", which is what an
// English-dashboard merchant who typed in Malay got.
func TestSystemPromptSaysTheCardFollowsTheDashboardNotTheConversation(t *testing.T) {
	prompt := systemPromptText("en")

	for _, want := range []string{
		"A confirmation card is part of that dashboard, not part of this conversation",
		"write that reason in English as well",
		"Do not switch it to the conversation's language",
	} {
		if !strings.Contains(normalizeWS(prompt), normalizeWS(want)) {
			t.Errorf("the system prompt no longer tells the model %q — the card's one "+
				"model-written sentence then follows the message again", want)
		}
	}

	// The PRE-FIX sentence. Restoring it is what produced the Malay card on an
	// English dashboard, so its absence is the fix, not a style choice.
	if strings.Contains(prompt, "write it in the language of the conversation") {
		t.Error("the system prompt again tells the model to write propose_write's reason in the " +
			"language of the CONVERSATION — that is the sentence that put a Malay summary on an " +
			"English confirmation card (t_ec87d6c7)")
	}
}

// TestProposeWriteReasonDescriptionNamesTheDashboardLanguage pins the tool-definition
// half. The model reads its tool schemas as instructions, so the `reason` field's
// description must agree with the system prompt — it said "in the merchant's
// language", which is the conversation's language, not the card's.
func TestProposeWriteReasonDescriptionNamesTheDashboardLanguage(t *testing.T) {
	def := proposeWriteDef()
	schema := string(def.Parameters)
	if !strings.Contains(schema, "DASHBOARD language") {
		t.Errorf("propose_write's `reason` description does not name the DASHBOARD language:\n%s\n"+
			"The model reads this schema as an instruction, and it previously said the reason "+
			"was \"in the merchant's language\" — the conversation's language, which is how a "+
			"Malay sentence reached an English card", schema)
	}
	if strings.Contains(schema, "bahasa pengguna") || strings.Contains(def.Description, "CADANGKAN") {
		t.Errorf("propose_write is described to the model in Malay again — the rest of the lane's "+
			"prompt text is English and a Malay instruction block invites a Malay answer:\n%s\n%s",
			def.Description, schema)
	}
}

// TestRunHandsTheModelTheDashboardLanguage is the wiring half, and the one that
// catches the realistic half-fix: a correct prompt builder that Run never passes
// the request's language to. The model's first round-trip must see a system turn
// naming the language from the context.
func TestRunHandsTheModelTheDashboardLanguage(t *testing.T) {
	caller := &fakeCaller{}
	model := &scriptedLLM{replies: []llm.ChatReply{{Content: "Hello."}}}
	r := newTestRunner(t, caller, model, nil)

	ctx := WithDashboardLang(chatCtx(), "ms")
	if _, err := r.Run(ctx, nil, "berapa banyak borang saya?"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(model.prompts) == 0 || len(model.prompts[0]) == 0 {
		t.Fatal("the model was called with no turns at all — this test would pass vacuously")
	}
	system := model.prompts[0][0]
	if system.Role != "system" {
		t.Fatalf("the first turn the model saw has role %q, want \"system\"", system.Role)
	}
	if !strings.Contains(system.Content, "The merchant's dashboard is set to Malay") {
		t.Errorf("the system turn the model received does not name the request's dashboard "+
			"language (Malay):\n%s", system.Content)
	}

	// And with no language attached (an unset profile, or a caller that did not
	// set one) the card is English — the dashboard's default, never a blank.
	model2 := &scriptedLLM{replies: []llm.ChatReply{{Content: "Hello."}}}
	r2 := newTestRunner(t, caller, model2, nil)
	if _, err := r2.Run(context.Background(), nil, "how many forms do I have?"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := model2.prompts[0][0].Content; !strings.Contains(got, "The merchant's dashboard is set to English") {
		t.Errorf("with no dashboard language set the system turn must fall back to English "+
			"(the dashboard's default); it says:\n%s", got)
	}
}

// TestDashboardLangTravelsInTheRequestContext pins the carrier itself: the value
// is per-REQUEST (one Runner serves every merchant), so it must not be readable
// from a context that never carried it, and it must survive the identity the
// REST edge puts there.
func TestDashboardLangTravelsInTheRequestContext(t *testing.T) {
	if got := DashboardLangFrom(context.Background()); got != "" {
		t.Errorf("DashboardLangFrom(background) = %q, want \"\" (not set)", got)
	}
	//nolint:staticcheck // a nil ctx is a programming error this helper survives on purpose
	if got := DashboardLangFrom(nil); got != "" {
		t.Errorf("DashboardLangFrom(nil) = %q, want \"\" (not set)", got)
	}
	ctx := WithDashboardLang(chatCtx(), "ta")
	if got := DashboardLangFrom(ctx); got != "ta" {
		t.Errorf("DashboardLangFrom(ctx) = %q, want \"ta\"", got)
	}
}

// cardLanguageSection returns the CARD LANGUAGE paragraph of the prompt (up to the
// next blank-line-separated heading), or "" when the prompt has none.
func cardLanguageSection(prompt string) string {
	const marker = "CARD LANGUAGE"
	i := strings.Index(prompt, marker)
	if i < 0 {
		return ""
	}
	rest := prompt[i+len(marker):]
	if j := strings.Index(rest, "\n\n"); j >= 0 {
		rest = rest[:j]
	}
	return marker + rest
}
