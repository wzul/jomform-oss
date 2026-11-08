// The guide explains settings; the chat must be able to READ them, and never to
// WRITE them.
//
// WHY THIS IS PINNED WITH THE GUIDE. The card that added the product guide also
// asked for read coverage of settings (card t_1e3af6b8, "bacaan untuk
// forms/sales/products/submissions/settings"). The two halves fail differently:
//
//   - without the read tools, the assistant can explain how the processing fee
//     works but cannot answer "berapa caj yang saya set?" — it would either say
//     it has no tool for that, or (worse) guess, and the guide would look correct
//     in every test while the assistant was useless for the follow-up question;
//   - with the WRITE tool in reach, the same conversation could change a rate
//     with no confirmation card. `set_payment_fees` and `set_shipping_rates`
//     replace whole rate sets, which is not a field edit with an inverse — they
//     are exactly the class the proposal set excludes.
//
// So this file asserts the pair: every settings surface the guide describes is
// readable, and none of its writers is dispatchable or proposable.
package chat

import (
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/mcpsrv"
)

func TestTheChatCanReadTheSettingsTheGuideExplains(t *testing.T) {
	read := map[string]bool{}
	for _, n := range mcpsrv.ChatReadTools() {
		read[n] = true
	}
	for _, want := range []string{
		"get_payment_fees",
		"get_shipping_rates",
		"get_shipping_couriers",
		"get_workspace_settings",
	} {
		if !read[want] {
			t.Errorf("the chat's read allowlist no longer contains %s: the guide explains that setting, so "+
				"the assistant can say how it works but cannot read what this merchant set — and a merchant "+
				"who asks a follow-up gets a refusal or a guess", want)
		}
	}

	// The writers stay out of both lists. A name is judged by its verb, so the
	// check reads the tool name rather than a hand-kept list: the set of settings
	// writers will grow, and each new one must be excluded for the same reason.
	proposable := map[string]bool{}
	for _, n := range mcpsrv.ChatProposableTools() {
		proposable[n] = true
	}
	for _, n := range mcpsrv.ChatReadTools() {
		if !strings.HasPrefix(n, "get_") && !strings.HasPrefix(n, "list_") {
			t.Errorf("%s is in the chat's READ allowlist but is not a read tool by name — the allowlist guard "+
				"polices what it can (no write sink, no write scope), but a name that does not announce itself "+
				"as a read is the shape a writer would sneak in as", n)
		}
	}
	for _, w := range []string{"set_payment_fees", "set_shipping_rates", "set_workspace_settings"} {
		if read[w] {
			t.Errorf("%s is readable from the chat, which is a write tool — the read allowlist must contain no writer", w)
		}
		if proposable[w] {
			t.Errorf("%s is PROPOSABLE from the chat. It replaces a whole settings set, which the proposal card "+
				"cannot preview and the user cannot undo with one click; the settings stay a dashboard action", w)
		}
	}
}

// TestTheGuideDoesNotPromiseASettingsAnswerItCannotGive is the bite in the other
// direction: the guide must not imply the assistant can see a merchant's settings
// anywhere it cannot. The prompt's own rule already says the guide is about the
// product in general and the tools answer account questions, so what this checks
// is that the rule is still there — dropping it is how a guide starts being
// quoted as if it were the merchant's data.
func TestTheGuideDoesNotPromiseASettingsAnswerItCannotGive(t *testing.T) {
	prompt := systemPromptText("")
	flat := strings.Join(strings.Fields(prompt), " ")
	for _, want := range []string{
		"Use the TOOLS, not the guide, for anything about this merchant's own account",
		"it cannot know their forms, prices, customers or settings",
	} {
		if !strings.Contains(flat, strings.Join(strings.Fields(want), " ")) {
			t.Errorf("the prompt no longer says %q. The guide is general product knowledge; without this rule the "+
				"model answers 'what are MY rates?' from the general description, which reads as a confident "+
				"answer about the merchant's account that it never looked up", want)
		}
	}
}
