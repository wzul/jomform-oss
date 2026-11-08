// The product questions the guide must be able to answer.
//
// WHAT THIS PINS, AND WHAT IT DOES NOT. The chat answers with a model, which no
// unit test can run; what a deterministic test CAN prove is that the material for
// the answer is in the prompt the model receives. Each question below is a real
// merchant question (Malay and English, the two ways Wan and his testers ask), it
// names the FACT entries that must cover it, and it lists phrases that must be
// readable in the rendered guide. If a fact is deleted, reworded into something
// that no longer says the thing, or never rendered, the question fails with its
// own sentence — which is the difference between "the guide shrank" and "the
// assistant can no longer answer how to pass the processing fee to a buyer".
//
// The model's ANSWER is verified live, on staging, not here. This file is the
// half that can be wrong silently for months: the guide is what the model reads,
// and a guide that lost a fact looks exactly like one that never had it.
package chat

import (
	"strings"
	"testing"
)

// minProductFacts and minProductQuestions are the floor the guide and the table
// must not fall below. They exist because a test that asserts over a shorter
// table is a test that got weaker in the same commit that made it weaker: deleting
// a fact AND its question would leave every assertion above green.
const (
	minProductFacts     = 20
	minProductQuestions = 14
)

// productQuestion is one question a merchant asks, the facts that must cover it,
// and the phrases that prove they are reachable in the rendered guide.
type productQuestion struct {
	// ask is the question as a merchant would type it.
	ask string
	// facts are productFact IDs that must cover it. Naming a fact (rather than
	// only phrases) is what ties the question to a provenance-checked entry.
	facts []string
	// wants are phrases the rendered guide must carry, matched case-insensitively
	// after whitespace is collapsed. They are deliberately short and factual.
	wants []string
}

// productQuestions is the acceptance set: at least ten product questions answered
// from the repo's own documentation. The list is longer than ten on purpose —
// the criterion is a floor, not a target, and the topics are the ones a merchant
// actually opens a chat for.
var productQuestions = []productQuestion{
	{
		ask:   "macam mana nak tetapkan caj pemprosesan kepada pembeli?",
		facts: []string{"payments.fee-setup", "payments.fee-grossup"},
		wants: []string{
			"pass the chip processing fee to the buyer",
			"settings → payment fees",
			"100 = 1%",
		},
	},
	{
		ask:   "kalau harga RM1,000 dan kadar 1%, pembeli bayar berapa?",
		facts: []string{"payments.fee-grossup"},
		wants: []string{"rm1,010.10", "smallest total"},
	},
	{
		ask:   "boleh saya terima derma?",
		facts: []string{"products.donation", "products.custom-amount"},
		wants: []string{"donation template", "pay-what-you-want", "appears in sales"},
	},
	{
		ask:   "boleh pembeli tentukan sendiri jumlah bayaran?",
		facts: []string{"products.custom-amount"},
		wants: []string{"minimum of at least rm1.00", "cannot have variations"},
	},
	{
		ask:   "boleh saya guna kurier untuk penghantaran?",
		facts: []string{"shipping.couriers"},
		wants: []string{"lalamove", "easyparcel", "courier quote"},
	},
	{
		ask:   "macam mana nak tetapkan kos penghantaran ikut negeri?",
		facts: []string{"shipping.zones"},
		wants: []string{"postcode range", "never silently zero"},
	},
	{
		ask:   "boleh saya buat refund separuh?",
		facts: []string{"sales.refunds"},
		wants: []string{"partially refunded", "full or in part"},
	},
	{
		ask:   "macam mana nak buat langganan bulanan?",
		facts: []string{"subscriptions.basics", "subscriptions.failed-charge"},
		wants: []string{"bills monthly or yearly", "checkout payment counts as charge 1"},
	},
	{
		ask:   "kalau kad pelanggan gagal, berapa kali retry?",
		facts: []string{"subscriptions.failed-charge"},
		wants: []string{"three times, one a day", "seven-day grace period", "never revokes the customer's saved card token"},
	},
	{
		ask:   "boleh saya buat kod diskaun atau kupon?",
		facts: []string{"absent.coupons"},
		wants: []string{"no coupon or discount-code feature", "withdrawn from the builder"},
	},
	{
		ask:   "adakah SST atau cukai dikira atas jualan?",
		facts: []string{"payments.no-tax-line"},
		wants: []string{"does not calculate sst", "no tax line"},
	},
	{
		ask:   "boleh saya guna domain sendiri?",
		facts: []string{"storefront.domains"},
		wants: []string{"cname to custom.jomform.com", "{slug}.jomform.com"},
	},
	{
		ask:   "macam mana nak sambungkan Claude atau AI agent?",
		facts: []string{"agents.mcp"},
		wants: []string{"mcp.jomform.com/mcp", "oauth 2.1", "no api key is needed"},
	},
	{
		ask:   "boleh saya embed form dalam website saya?",
		facts: []string{"storefront.embed"},
		wants: []string{"iframe", "posts its height"},
	},
	{
		ask:   "pelanggan tak terima emel resit, macam mana?",
		facts: []string{"email.notifications"},
		wants: []string{"built-in default", "{{ref_code}}"},
	},
	{
		ask:   "macam mana nak halang spam dalam submission?",
		facts: []string{"spam.review"},
		wants: []string{"review queue", "un-flagged", "30 days"},
	},
	{
		ask:   "boleh saya guna API key untuk server sendiri?",
		facts: []string{"agents.rest-api"},
		wants: []string{"settings → api keys", "readonly"},
	},
	{
		ask:   "boleh tambah variation untuk produk?",
		facts: []string{"products.variations"},
		wants: []string{"its own name, price, stock and image"},
	},
	{
		ask:   "macam mana nak pasang 2FA?",
		facts: []string{"security.2fa"},
		wants: []string{"profile → security", "6-digit code", "trusted device"},
	},
	{
		ask:   "boleh saya kenakan caj kad kepada pembeli? ada syarat?",
		facts: []string{"payments.fee-acknowledgement"},
		wants: []string{"card-network and acquirer rules", "acknowledgement sentence"},
	},
	{
		ask:   "kalau saya refund, caj pemprosesan dikira semula?",
		facts: []string{"payments.fee-refunds"},
		wants: []string{"never recalculates the processing fee"},
	},
	{
		ask:   "apa jenis produk yang boleh saya jual?",
		facts: []string{"products.types"},
		wants: []string{"one_time", "subscription (recurring)", "custom_amount"},
	},
	{
		ask:   "macam mana saya tahu ada jualan baru masuk?",
		facts: []string{"sales.realtime"},
		wants: []string{"live pill", "sound switch"},
	},
	{
		ask:   "ada template siap yang boleh saya guna?",
		facts: []string{"storefront.templates"},
		wants: []string{"template instead of the drag-and-drop builder"},
	},
	{
		ask:   "form saya boleh jadi borang respons saja tanpa bayaran?",
		facts: []string{"forms.modes"},
		wants: []string{"response-only form", "takes no payment"},
	},
}

// questionProblems reports every question the rendered guide cannot answer.
// Pure over (questions, guide, facts) so the bite proof below can hand it a
// mutated guide and require the same judgement.
func questionProblems(questions []productQuestion, guide string, facts []productFact) []string {
	known := make(map[string]bool, len(facts))
	for _, f := range facts {
		known[f.ID] = true
	}
	flat := strings.ToLower(strings.Join(strings.Fields(guide), " "))
	var problems []string
	for _, q := range questions {
		if len(q.facts) == 0 {
			problems = append(problems, q.ask+": names no fact, so nothing ties the question to a verified entry")
		}
		for _, id := range q.facts {
			if !known[id] {
				problems = append(problems, q.ask+": names the fact "+id+", which does not exist in productFacts")
			}
		}
		if len(q.wants) == 0 {
			problems = append(problems, q.ask+": names nothing the guide must say, so it asserts only that a question exists")
		}
		for _, want := range q.wants {
			if !strings.Contains(flat, strings.ToLower(strings.Join(strings.Fields(want), " "))) {
				problems = append(problems, q.ask+": the guide no longer says \""+want+"\"")
			}
		}
	}
	return problems
}

func TestTenProductQuestionsAreAnsweredFromTheGuide(t *testing.T) {
	if len(productQuestions) < minProductQuestions {
		t.Fatalf("the acceptance set holds %d questions, want at least %d — the card requires at least ten, "+
			"and a table that shrinks to fit its own assertions proves nothing", len(productQuestions), minProductQuestions)
	}
	for _, p := range questionProblems(productQuestions, productGuideText(), productFacts) {
		t.Error(p)
	}
}

// TestTheQuestionsCoverTheGuideBothWays is the other direction: every question
// must be covered (above), and every fact the guide spends tokens on must be
// reachable from at least one question. A fact no question asks about is either
// dead weight in a prompt paid on every round-trip, or a question the set is
// missing — and the second is how the guide silently stops being tested.
func TestTheQuestionsCoverTheGuideBothWays(t *testing.T) {
	asked := map[string]bool{}
	for _, q := range productQuestions {
		for _, id := range q.facts {
			asked[id] = true
		}
	}
	for _, f := range productFacts {
		if !asked[f.ID] {
			t.Errorf("fact %s is in the guide but no product question asks about it — add the question it "+
				"answers, or drop the fact: every fact is paid for on every model call", f.ID)
		}
	}
}

// TestQuestionCoverageBites is the bite proof.
//
// It takes the REAL guide, removes a phrase the fee question depends on, and
// requires the check to name that question. Nothing is written to disk; the
// mutation is a local string. A coverage test that would stay green on a guide
// missing the fact it exists to protect is a test that asserts the table against
// itself.
//
// The removed phrase is the one the QUESTION's own `wants` names (the basis-point
// rule), not a sentence chosen for being long: removing a sentence whose content
// another fact happens to repeat would leave the question answerable and the proof
// would be measuring the wrong thing — which is how the first version of this
// proof passed with nothing removed.
func TestQuestionCoverageBites(t *testing.T) {
	guide := productGuideText()
	const gone = "(100 = 1%)"
	if !strings.Contains(guide, gone) {
		t.Fatalf("the phrase this proof removes (%q) is not in the guide — the proof would test nothing", gone)
	}
	mutated := strings.Replace(guide, gone, "", 1)
	problems := questionProblems(productQuestions, mutated, productFacts)
	want := "macam mana nak tetapkan caj pemprosesan kepada pembeli?"
	found := false
	for _, p := range problems {
		if strings.HasPrefix(p, want+":") {
			found = true
		}
	}
	if !found {
		t.Fatalf("with the basis-point rule removed from the guide, the question about passing the processing "+
			"fee is still reported as answerable — the coverage check is not reading the guide. Problems seen: %v",
			problems)
	}
}
