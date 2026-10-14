package chat

// THE HARDCODED-LANGUAGE DEFECT (card t_30ffa28a).
//
// WHAT WAS WRONG. The confirmation card's own words were Malay literals built in
// this package — the title, the summary, every field label, and every sentence the
// server COMPOSED out of a value ("3 medan", "tiada had (infinite)", "ya"). The
// card's chrome (the badge, the buttons, the hint) was localized by the dashboard's
// five-locale dictionary, so on an English dashboard the same card read half
// English and half Malay. A merchant who does not read Malay was asked to confirm
// a change described in a language they may not read, on the one surface where the
// wording IS the safety mechanism.
//
// THE RULE THESE TESTS PIN. The API never learns the caller's language (it is not
// sent on a JSON call), so anything the SERVER writes is one language for every
// locale. The card therefore carries KEYS plus the merchant's own DATA:
//
//	title_key / summary_key   chat.proposal.title.*, chat.proposal.summary.*
//	label_key                 chat.proposal.label.*  (label_raw only for an
//	                          argument/theme key the dictionary does not cover)
//	value_key + value_params  chat.proposal.value.*  (a composed sentence)
//	value                     the merchant's own value, shown verbatim
//
// WHAT IS ASSERTED, and why each half is needed:
//
//	1. the KEY is present and looks like a key — an empty key is a card with no
//	   heading, and it renders as nothing at all in every locale;
//	2. NOTHING that reaches the wire is a sentence: the card's JSON has no
//	   `title`, `summary` or `label` field at all. This is the half that makes the
//	   regression unrepresentable rather than merely unlikely — the old shape put
//	   the Malay sentence in exactly those three fields, and a new prose field
//	   would have to be given one of these names to look like a card;
//	3. every `label_raw` really is one of the proposal's own ARGUMENT keys, so the
//	   verbatim-label escape hatch cannot be used to pass a sentence through;
//	4. a composed value is a KEY, not text — pinned on the cases the server used to
//	   write words for (yes/no, counts, the list summary, "no limit", the creation
//	   sentence).
//
// THE DICTIONARY HALF lives on the web side (src/lib/chatProposal.test.ts): it
// reads this package's source, extracts every "chat.proposal.*" literal and fails
// when the dictionary is missing one, and it bans a literal sentence assigned to
// any of the key/value fields here. Two halves of one card drifting apart is the
// defect, so they are pinned from both ends.
//
// THE BITE. `TestProposalProseDetectorCatchesTheOldShape` feeds the detector the
// pre-fix card shape (a literal `title`/`summary`/`label`) and requires it to name
// all three — so the detector cannot pass by looking at nothing.

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/mcpsrv"
)

// maxProposalArgs is a FULL argument set per proposable tool: every key its
// renderer knows, so every row the card can produce is examined. A tool that gains
// an argument key without a label key would otherwise be invisible here.
func maxProposalArgs(t *testing.T, tool string) map[string]any {
	t.Helper()
	const fid = "11111111-2222-3333-4444-555555555555"
	switch tool {
	case "update_product":
		return map[string]any{
			"product_id": fid, "name": "Renamed product", "description": "A description",
			"price_sen": float64(2500), "stock": float64(3), "active": true,
			"type": "recurring", "billing_period": "monthly", "trial_days": float64(14),
			"schedule_mode": "charge_on_signup",
			"images":        []any{"https://cdn.example.com/a.png", "https://cdn.example.com/b.png"},
			"variations": []any{
				map[string]any{"name": "S", "price_sen": float64(1000)},
				map[string]any{"name": "M", "price_sen": float64(2000)},
			},
			"subscription_charges": float64(12),
		}
	case "update_product_variation":
		return map[string]any{
			"product_id": fid, "variation_id": "22222222-3333-4444-5555-666666666666",
			"name": "Size L", "price_sen": float64(3000), "stock": float64(7),
			"billing_period": "monthly", "trial_days": float64(7),
			"subscription_charges":       float64(3),
			"images":                     []any{"https://cdn.example.com/c.png"},
			"clear_billing_period":       true,
			"clear_trial_days":           true,
			"clear_subscription_charges": true,
		}
	case "set_form_theme":
		return map[string]any{
			"form_id": fid, "primary": "#0084d1", "background": "#ffffff",
			"text": "#111111", "card_background": "#f8fafc",
			"card_border_radius": "12", "font_family": "Inter", "layout": "stacked",
			"logo_url": "https://cdn.example.com/logo.png", "spacing": "normal",
			"product_image_size": "medium", "product_image_layout": "top",
			"custom_css": "body{}", "custom_js": "void 0",
			// A theme key the dictionary does not cover: the card must still SHOW it,
			// under its own name, and that name must be one of the arguments.
			"brand_new_theme_key": "value",
		}
	case "upsert_form":
		return map[string]any{
			"form_id": fid, "title": "My form", "slug": "my-form",
			"config":        map[string]any{"registered_fields": []any{map[string]any{"type": "text"}}},
			"theme":         map[string]any{"primary": "#0084d1"},
			"notifications": []any{"a@example.com"},
			"chip_feeds":    []any{map[string]any{"id": "f1"}},
		}
	}
	return nil
}

// proseKeysInCardJSON returns the prose field names found in a marshalled card.
// See the bite test below: it is proved against the PRE-FIX shape as well, so a
// detector that matches nothing cannot be mistaken for a clean card.
func proseKeysInCardJSON(b []byte) []string {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		return []string{"<unparseable>"}
	}
	var out []string
	for _, k := range []string{"title", "summary", "label"} {
		if _, ok := top[k]; ok {
			out = append(out, k)
		}
	}
	var fields []map[string]json.RawMessage
	if raw, ok := top["fields"]; ok {
		_ = json.Unmarshal(raw, &fields)
	}
	for _, f := range fields {
		for _, k := range []string{"label", "title", "summary"} {
			if _, ok := f[k]; ok {
				out = append(out, "fields[]. "+k)
			}
		}
	}
	return out
}

func TestProposalProseDetectorCatchesTheOldShape(t *testing.T) {
	// The card AS IT SHIPPED before this card: a Malay title, a Malay summary and a
	// Malay field label, straight from the server. The detector must NAME all three,
	// or the assertions below would pass on a card nobody checked.
	preFix := []byte(`{"token":"t","args_hash":"h","tool":"update_product",
		"title":"Kemas kini produk",
		"summary":"Medan produk yang disenaraikan di bawah akan ditukar.",
		"fields":[{"label":"Produk","value":"x"},{"label":"Nama","value":"y"}],
		"expires_at":"2026-01-01T00:00:00Z","reversible":true}`)
	got := proseKeysInCardJSON(preFix)
	for _, want := range []string{"title", "summary", "fields[]. label"} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
			}
		}
		if !found {
			t.Errorf("the prose detector did not report %q on the pre-fix card (it reported %v) — "+
				"every assertion below would then be checking nothing", want, got)
		}
	}
}

// TestProposalCardCarriesKeysAndDataOnly is the API-side half of the rule: nothing
// the server puts on the card is a sentence.
func TestProposalCardCarriesKeysAndDataOnly(t *testing.T) {
	tools := mcpsrv.ChatProposableTools()
	if len(tools) < 3 {
		t.Fatalf("mcpsrv.ChatProposableTools() returned %d tool(s) — this guard is not reading the "+
			"proposable set, so it would judge no card at all", len(tools))
	}
	checked := 0
	for _, tool := range tools {
		args := maxProposalArgs(t, tool)
		if args == nil {
			// A proposable tool this test has no argument set for: the card would go
			// unjudged, so refuse rather than pass quietly.
			t.Fatalf("no maximal argument set for the proposable tool %q — add one, or the card it "+
				"renders is never checked against the key rule", tool)
		}
		label := func(string, string) string { return "" }
		titleKey, summaryKey, fields, ok := Describe(tool, args, label)
		if !ok {
			t.Fatalf("Describe(%s) refused its own maximal argument set", tool)
		}
		if !strings.HasPrefix(titleKey, "chat.proposal.title.") {
			t.Errorf("Describe(%s) title key = %q, want a chat.proposal.title.* dictionary key", tool, titleKey)
		}
		if !strings.HasPrefix(summaryKey, "chat.proposal.summary.") {
			t.Errorf("Describe(%s) summary key = %q, want a chat.proposal.summary.* dictionary key", tool, summaryKey)
		}
		if len(fields) == 0 {
			t.Errorf("Describe(%s) produced NO fields for a full argument set — the user is asked to "+
				"confirm an effect the card does not show", tool)
		}
		for i, f := range fields {
			switch {
			case f.LabelKey == "" && f.LabelRaw == "":
				t.Errorf("Describe(%s) field %d carries neither a label key nor a raw key: %+v", tool, i, f)
			case f.LabelKey != "" && !strings.HasPrefix(f.LabelKey, "chat.proposal.label."):
				t.Errorf("Describe(%s) field %d label key = %q, want a chat.proposal.label.* dictionary key "+
					"(a sentence here is shown to every locale in one language)", tool, i, f.LabelKey)
			case f.LabelRaw != "":
				// The verbatim-label escape hatch: it may only carry one of THIS
				// proposal's own argument keys.
				if _, isArg := args[f.LabelRaw]; !isArg {
					t.Errorf("Describe(%s) field %d shows label_raw=%q, which is NOT one of the "+
						"proposal's arguments — that is how a sentence could be smuggled onto the card "+
						"past the key rule", tool, i, f.LabelRaw)
				}
			}
			switch {
			case f.ValueKey == "" && f.Value == "":
				t.Errorf("Describe(%s) field %d carries no value at all: %+v", tool, i, f)
			case f.ValueKey != "" && f.Value != "":
				t.Errorf("Describe(%s) field %d carries BOTH a value key and a literal value: %+v — "+
					"which one the dashboard renders would then be a coincidence of the page's code", tool, i, f)
			case f.ValueKey != "" && !strings.HasPrefix(f.ValueKey, "chat.proposal.value."):
				t.Errorf("Describe(%s) field %d value key = %q, want a chat.proposal.value.* dictionary key",
					tool, i, f.ValueKey)
			}
		}

		// ── 2. NOTHING ON THE WIRE IS A SENTENCE. ──
		card := ProposalCard{
			Token: "t", ArgsHash: "h", Tool: tool,
			TitleKey: titleKey, SummaryKey: summaryKey, Fields: fields,
			ExpiresAt: "2026-01-01T00:00:00Z", Reversible: true,
		}
		b, err := json.Marshal(card)
		if err != nil {
			t.Fatalf("marshal the %s card: %v", tool, err)
		}
		if prose := proseKeysInCardJSON(b); len(prose) > 0 {
			t.Errorf("the marshalled %s card carries prose field(s) %v — the card's wording must be "+
				"KEYS the dashboard resolves, never server sentences: %s", tool, prose, b)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no card was checked — the guard is vacuous")
	}
}

// TestProposalComposedValuesAreDictionaryKeys pins the cases the server used to
// write WORDS for. They are the ones a key-only reading of the card would miss:
// the label was already a label, but "ya", "3 medan" and "tiada had (infinite)"
// were sentences the server composed out of one value.
func TestProposalComposedValuesAreDictionaryKeys(t *testing.T) {
	const fid = "11111111-2222-3333-4444-555555555555"
	label := func(string, string) string { return "" }

	find := func(t *testing.T, tool string, args map[string]any, labelKey string) ProposalField {
		t.Helper()
		_, _, fields, ok := Describe(tool, args, label)
		if !ok {
			t.Fatalf("Describe(%s) refused %+v", tool, args)
		}
		for _, f := range fields {
			if f.LabelKey == labelKey {
				return f
			}
		}
		t.Fatalf("Describe(%s) produced no %s row: %+v", tool, labelKey, fields)
		return ProposalField{}
	}

	// A flag: yes/no is a WORD in every language, so it is a key.
	active := find(t, "update_product", map[string]any{"product_id": fid, "active": true}, keyLabelActive)
	if active.ValueKey != keyValueYes || active.Value != "" {
		t.Errorf("the active row = %+v, want value_key=%q — a boolean was rendered as the literal "+
			"\"ya\"/\"tidak\", which is Malay on every dashboard", active, keyValueYes)
	}
	inactive := find(t, "update_product", map[string]any{"product_id": fid, "active": false}, keyLabelActive)
	if inactive.ValueKey != keyValueNo {
		t.Errorf("the active=false row = %+v, want value_key=%q", inactive, keyValueNo)
	}

	// A COUNT: "12 kali" was a sentence with an embedded number.
	charges := find(t, "update_product",
		map[string]any{"product_id": fid, "subscription_charges": float64(12)}, keyLabelCharges)
	if charges.ValueKey != keyValueChargeCount || charges.ValueParams["n"] != "12" {
		t.Errorf("the subscription_charges row = %+v, want value_key=%q with n=12 — the number alone "+
			"is not the sentence the user reads", charges, keyValueChargeCount)
	}

	// "no limit (infinite)" — words, not a value at all.
	unlimited := find(t, "update_product",
		map[string]any{"product_id": fid, "subscription_charges": nil}, keyLabelCharges)
	if unlimited.ValueKey != keyValueUnlimited {
		t.Errorf("the unlimited-charges row = %+v, want value_key=%q", unlimited, keyValueUnlimited)
	}

	// The creation sentence on an upsert with no id.
	action := find(t, "upsert_form", map[string]any{"title": "New form"}, keyLabelAction)
	if action.ValueKey != keyValueCreateForm {
		t.Errorf("the creation row = %+v, want value_key=%q — \"Cipta borang baharu\" was the last "+
			"Malay sentence on a creation card", action, keyValueCreateForm)
	}

	// A LIST: "1 item: X" / "3 items: X …" is composed around the first element.
	list := find(t, "update_product",
		map[string]any{"product_id": fid, "images": []any{
			"https://cdn.example.com/a.png", "https://cdn.example.com/b.png",
		}}, keyLabelImages)
	if list.ValueKey != keyValueListMany || list.ValueParams["n"] != "2" {
		t.Errorf("the images row = %+v, want value_key=%q with n=2", list, keyValueListMany)
	}

	// The variation-set sentence: "N rows — the full list, replacing the existing one".
	variations := find(t, "update_product", map[string]any{
		"product_id": fid,
		"variations": []any{map[string]any{"name": "S"}, map[string]any{"name": "M"}},
	}, keyLabelVariations)
	if variations.ValueKey != keyValueVariationSet || variations.ValueParams["n"] != "2" {
		t.Errorf("the variations row = %+v, want value_key=%q with n=2", variations, keyValueVariationSet)
	}

	// ── The DATA half stays data. ──
	// A price is the merchant's own number: translating it would be a lie about what
	// gets written, so it is shown verbatim (and money is integer sen everywhere).
	price := find(t, "update_product", map[string]any{"product_id": fid, "price_sen": float64(2500)}, keyLabelPrice)
	if price.Value != "RM25.00" || price.ValueKey != "" {
		t.Errorf("the price row = %+v, want the literal RM25.00 — a value is data, not a sentence", price)
	}
}

// TestProposalKeysCoverEveryProposableTool keeps the key table and the proposable
// set in step: a tool that can be proposed but has no title/summary key would
// render a card with no heading in every locale.
func TestProposalKeysCoverEveryProposableTool(t *testing.T) {
	for _, tool := range mcpsrv.ChatProposableTools() {
		titleKey, summaryKey, ok := ProposalKeys(tool)
		if !ok {
			t.Errorf("%q is proposable but has no card keys — its card would have no heading", tool)
			continue
		}
		if titleKey == "" || summaryKey == "" {
			t.Errorf("%q's card keys are empty (title=%q summary=%q)", tool, titleKey, summaryKey)
		}
	}
	// And nothing else has keys, so the two maps cannot drift apart in the other
	// direction either.
	for tool := range proposalDocs {
		if !mcpsrv.ChatToolIsProposable(tool) {
			t.Errorf("proposalDocs declares %q, which is NOT in the proposable set — a card for a tool "+
				"the server would refuse", tool)
		}
	}
}

// TestProposalCountValuesInflectAtOne pins the COUNT rows at the number ONE.
//
// THE DEFECT (t_212be8da, found while verifying t_8eb63039's card). Every count
// value had ONE form per key — "{n} fields", "{n} items", "{n} times", "{n} rows" —
// so English, which inflects and is the dashboard's DEFAULT language, read
// "Configuration — 1 fields" and "Number of charges — 1 times" on the very card the
// merchant is asked to confirm. Malay, Chinese, Tamil and Arabic do not change the
// noun after a numeral the way English does, and the four of them read correctly at
// n=1; English did not. chat.proposal.value.listOne / .listMany already carried the
// singular case for the LIST row, and the bare counts beside them never got one.
//
// WHAT IS PINNED HERE. The server CHOOSES the key, so the choice is pinned on the
// rows the card really renders: at n=1 the singular key and NOTHING to interpolate;
// at any other n the count key with the number in value_params (English "0 fields"
// and "3 fields" are both correct — the plural form is the right one for zero too).
// The singular keys' WORDS live in the dashboard's dictionary and are pinned on the
// web side (src/lib/chatProposal.test.ts), which reads this file's key literals and
// fails if the dictionary is missing one.
//
// THE BITE. Every case below failed on the pre-fix renderer with
// "want …fieldCountOne, got …fieldCount" — measured by running this test against the
// unpatched call sites before the fix landed, not asserted after the fact.
func TestProposalCountValuesInflectAtOne(t *testing.T) {
	const fid = "11111111-2222-3333-4444-555555555555"
	label := func(string, string) string { return "" }

	// rowFor renders one card row for a tool, by its label key.
	rowFor := func(t *testing.T, tool string, args map[string]any, labelKey string) ProposalField {
		t.Helper()
		_, _, fields, ok := Describe(tool, args, label)
		if !ok {
			t.Fatalf("Describe(%s) refused %+v", tool, args)
		}
		for _, f := range fields {
			if f.LabelKey == labelKey {
				return f
			}
		}
		t.Fatalf("Describe(%s) produced no %s row: %+v", tool, labelKey, fields)
		return ProposalField{}
	}

	// argsAt gives the SAME row a given number of things to count, so the only
	// difference between the three renders of a case is n.
	items := func(n int, build func(int) any) []any {
		out := make([]any, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, build(i))
		}
		return out
	}

	cases := []struct {
		what     string
		tool     string
		labelKey string
		argsAt   func(n int) map[string]any
		oneKey   string
		countKey string
	}{
		{
			what:     "the form's config fields",
			tool:     "upsert_form",
			labelKey: keyLabelConfig,
			argsAt: func(n int) map[string]any {
				cfg := map[string]any{}
				for _, k := range items(n, func(i int) any { return "field-" + string(rune('a'+i)) }) {
					cfg[k.(string)] = 1
				}
				return map[string]any{"title": "My form", "config": cfg}
			},
			oneKey:   keyValueFieldCountOne,
			countKey: keyValueFieldCount,
		},
		{
			what:     "the form's notification entries",
			tool:     "upsert_form",
			labelKey: keyLabelNotifications,
			argsAt: func(n int) map[string]any {
				return map[string]any{
					"title":         "My form",
					"notifications": items(n, func(i int) any { return "owner" + string(rune('a'+i)) + "@example.com" }),
				}
			},
			oneKey:   keyValueItemCountOne,
			countKey: keyValueItemCount,
		},
		{
			what:     "the product's variations",
			tool:     "update_product",
			labelKey: keyLabelVariations,
			argsAt: func(n int) map[string]any {
				return map[string]any{
					"product_id": fid,
					"variations": items(n, func(i int) any { return map[string]any{"name": string(rune('S' + i))} }),
				}
			},
			oneKey:   keyValueVariationSetOne,
			countKey: keyValueVariationSet,
		},
		{
			what:     "the subscription's number of charges",
			tool:     "update_product",
			labelKey: keyLabelCharges,
			argsAt: func(n int) map[string]any {
				return map[string]any{"product_id": fid, "subscription_charges": float64(n)}
			},
			oneKey:   keyValueChargeCountOne,
			countKey: keyValueChargeCount,
		},
	}

	for _, c := range cases {
		one := rowFor(t, c.tool, c.argsAt(1), c.labelKey)
		if one.ValueKey != c.oneKey {
			t.Errorf("at n=1 %s renders value_key=%q, want %q — one form per key made an English "+
				"dashboard read \"1 fields\" / \"1 items\" / \"1 times\" on the card the merchant is "+
				"asked to confirm (t_212be8da)", c.what, one.ValueKey, c.oneKey)
		}
		if n, ok := one.ValueParams["n"]; ok {
			t.Errorf("at n=1 %s carries value_params[n]=%q — the singular key states the one itself, "+
				"and a parameter nothing interpolates is a placeholder waiting to be printed", c.what, n)
		}

		// Zero and the plural are the COUNT form: English "0 fields" / "3 fields".
		for _, n := range []int{0, 2, 3} {
			row := rowFor(t, c.tool, c.argsAt(n), c.labelKey)
			if row.ValueKey != c.countKey {
				t.Errorf("at n=%d %s renders value_key=%q, want %q — only ONE takes the singular form",
					n, c.what, row.ValueKey, c.countKey)
			}
			if row.ValueParams["n"] != strconv.Itoa(n) {
				t.Errorf("at n=%d %s carries value_params=%v, want n=%d — the number alone is not the "+
					"sentence the user reads", n, c.what, row.ValueParams, n)
			}
		}
	}
}

// TestCountedValueChoosesTheSingularKeyAtOne is the boundary in one place: the four
// rows above prove the call sites use it, this proves the choice is exactly n==1 and
// nothing else (a `n <= 1` would put "1 field" on an empty config as well).
func TestCountedValueChoosesTheSingularKeyAtOne(t *testing.T) {
	for _, n := range []int{-1, 0, 2, 12} {
		got := countedValue(keyValueFieldCountOne, keyValueFieldCount, n)
		if got.key != keyValueFieldCount || got.params["n"] != strconv.Itoa(n) {
			t.Errorf("countedValue(n=%d) = %+v, want the count key %q with n=%d", n, got, keyValueFieldCount, n)
		}
	}
	got := countedValue(keyValueFieldCountOne, keyValueFieldCount, 1)
	if got.key != keyValueFieldCountOne {
		t.Errorf("countedValue(n=1) = %+v, want the singular key %q", got, keyValueFieldCountOne)
	}
	if got.params != nil {
		t.Errorf("countedValue(n=1) carries params %v — the singular sentence has nothing to "+
			"interpolate, and an unused {n} would print as a placeholder", got.params)
	}
}
