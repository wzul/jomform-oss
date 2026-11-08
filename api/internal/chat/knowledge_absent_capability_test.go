// The guide must never claim a capability the code does not have.
//
// WHY THIS IS A SEPARATE GUARD FROM THE PROVENANCE ONE. Provenance answers "does
// some file say this?" — which is the right question for a fact about how a
// shipped feature behaves. It is the WRONG question for a NEGATIVE, because the
// documentation does not describe features that do not exist, and the honest
// source for "JomForm has no coupon codes" is the code that would have them. So
// each entry below states a capability the assistant must NOT promise, pins the
// sentence in the prompt that refuses it, and asks the CODE whether the
// capability has quietly landed:
//
//   - present() is the probe. It must be false. When a feature lands, this test
//     fails and names the entry, because the guide's "does not exist" is now a
//     lie — and a merchant told that twice stops believing the rest of the guide.
//   - the probe is written against the thing that would have to change (the
//     builder's block allowlist, the sale's columns, the chat's own tool lists),
//     not against a string in a docs page that nobody updates.
//
// WHAT IS DELIBERATELY NOT IN HERE. The card's own example — a product
// custom_amount that "has not landed yet" — had already landed when this branch
// was cut (api/internal/dbstore/product.go, migration 000084), so the guide says
// the opposite: a custom_amount product EXISTS, and the donation answer depends
// on it. A negative copied from a card into the prompt without checking the tree
// is how the assistant ends up telling a merchant that a feature they can see is
// not there.
package chat

import (
	"reflect"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/formgen"
	"github.com/wzul/jomform/api/internal/mcpsrv"
)

// absentCapability is one thing the assistant must not promise while the code
// does not have it.
type absentCapability struct {
	// id names the entry in a failure message.
	id string
	// ask is a merchant question that would be answered WITH the capability if it
	// existed.
	ask string
	// mustSay are phrases the prompt must carry that state the absence. Checked
	// against the full system prompt (the text the model receives), because the
	// assistant's own limits are stated there and the product gaps in the guide.
	mustSay []string
	// present reports whether the capability has landed in the code. It must be
	// false.
	present func() bool
	// landedFile names what to look at when present() flips true.
	landedFile string
}

// absentCapabilities is the set the acceptance criterion asks for, plus the one
// the chat's own boundary needs. Each probe reads the tree, not this file.
var absentCapabilities = []absentCapability{
	{
		id:  "coupon-codes",
		ask: "boleh saya buat kod diskaun atau kupon?",
		mustSay: []string{
			"no coupon or discount-code feature",
			"withdrawn from the builder",
		},
		present: func() bool {
			// The AI builder's block allowlist is the surface a coupon would have
			// to come back through. The block was withdrawn because nothing on the
			// submit path applies a discount, so an allowed block here means the
			// feature exists again (or was re-added as a dead block, which is the
			// original defect — see api/internal/formgen/coupon_withdrawn_test.go).
			return formgen.ValidBlockTypes[formgen.BlockType("coupon")]
		},
		landedFile: "api/internal/formgen/formgen.go (ValidBlockTypes)",
	},
	{
		id:  "sales-tax",
		ask: "adakah SST atau cukai dikira atas jualan?",
		mustSay: []string{
			"does not calculate SST or any other tax on a sale",
			"with no tax line",
		},
		present: func() bool {
			// A tax line would have to be a column on the sale, because the sale
			// total is the one number a receipt prints. Probing the struct rather
			// than a migration file means the probe follows the scan, which is what
			// the money rule requires of every column.
			t := reflect.TypeOf(dbstore.Sale{})
			for i := 0; i < t.NumField(); i++ {
				name := strings.ToLower(t.Field(i).Name)
				if strings.Contains(name, "tax") || strings.Contains(name, "sst") {
					return true
				}
			}
			return false
		},
		landedFile: "api/internal/dbstore/sale.go (Sale)",
	},
	{
		id:  "chat-refunds",
		ask: "boleh saya refund jualan dari chat ini?",
		mustSay: []string{
			"You cannot delete, archive, refund, cancel, publish, invite, revoke or configure anything",
		},
		present: func() bool {
			// The assistant's whole claim is that a refund is not reachable from
			// the conversation: not as a dispatch, not as a proposal. If a refund
			// or cancel tool ever joins either list, the prompt's sentence above
			// becomes false and this probe says so.
			names := append(mcpsrv.ChatReadTools(), mcpsrv.ChatProposableTools()...)
			for _, n := range names {
				l := strings.ToLower(n)
				if strings.Contains(l, "refund") || strings.Contains(l, "cancel") {
					return true
				}
			}
			return false
		},
		landedFile: "api/internal/mcpsrv/chat_tools.go (chatReadTools / chatProposableTools)",
	},
}

// absenceProblems reports every entry whose sentence is missing or whose
// capability has landed. Pure over (entries, prompt) so the bite proof can flip a
// probe and require the same judgement.
//
// The prompt is compared with runs of whitespace collapsed: the instruction it
// looks for is a sentence inside a re-wrapped paragraph, and a guard that pinned
// the template's line breaks would report a re-wrap as a missing refusal.
func absenceProblems(entries []absentCapability, prompt string) []string {
	flat := strings.Join(strings.Fields(prompt), " ")
	var problems []string
	for _, e := range entries {
		if e.present() {
			problems = append(problems, e.id+": the capability has LANDED ("+e.landedFile+") but the prompt still "+
				"tells the model it does not exist — update the guide fact and this entry in the same change, or "+
				"the assistant will deny a feature the merchant can see")
		}
		if len(e.mustSay) == 0 {
			problems = append(problems, e.id+": states no refusal sentence, so nothing stops the model promising it")
		}
		for _, want := range e.mustSay {
			if !strings.Contains(flat, strings.Join(strings.Fields(want), " ")) {
				problems = append(problems, e.id+": the prompt no longer contains \""+want+"\" — the model is free "+
					"to answer this question with the capability invented")
			}
		}
	}
	return problems
}

func TestThePromptNeverClaimsACapabilityTheCodeLacks(t *testing.T) {
	for _, p := range absenceProblems(absentCapabilities, systemPromptText()) {
		t.Error(p)
	}
}

// TestTheAbsenceCheckBites flips one probe and requires the check to report it.
// The mutation is a local copy of the entry, never the shipped table, and the
// prompt is the REAL one — so the proof exercises the same judgement the live
// test makes.
func TestTheAbsenceCheckBites(t *testing.T) {
	if len(absentCapabilities) < 2 {
		t.Fatal("fewer than two absence entries: the card's criterion is two caught examples")
	}
	for _, e := range absentCapabilities {
		mutated := make([]absentCapability, 0, len(absentCapabilities))
		for _, other := range absentCapabilities {
			if other.id == e.id {
				landed := other
				landed.present = func() bool { return true }
				mutated = append(mutated, landed)
				continue
			}
			mutated = append(mutated, other)
		}
		problems := absenceProblems(mutated, systemPromptText())
		found := false
		for _, p := range problems {
			if strings.HasPrefix(p, e.id+":") && strings.Contains(p, "has LANDED") {
				found = true
			}
		}
		if !found {
			t.Fatalf("with %s's probe forced true, the check did not report it — a probe that cannot fire is "+
				"a comment, and the feature would land with the guide still denying it. Problems seen: %v",
				e.id, problems)
		}
	}
}
