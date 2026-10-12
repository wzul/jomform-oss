package httpapi

import (
	"context"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// Issue #57: effective buyer-facing language = form.config.lang ?? workspace
// defaultLang ?? 'en'. These run without a DB: a nil Workspaces store falls back
// to 'en' for the workspace default, while a valid per-form override must still
// win.
func TestEffectiveLangStringsResolution(t *testing.T) {
	h := &PublicHandlers{} // Workspaces == nil → workspace default resolves to "en"

	ctx := context.Background()

	cases := []struct {
		name     string
		config   map[string]any
		wantLang string
		wantSub  string // English submit, so "en" default is observable
	}{
		{
			name:     "form override wins",
			config:   map[string]any{"lang": "ms"},
			wantLang: "ms",
			wantSub:  "Teruskan ke Pembayaran",
		},
		{
			name:     "form override zh",
			config:   map[string]any{"lang": "zh"},
			wantLang: "zh",
			wantSub:  "前往付款",
		},
		{
			name:     "no config -> en fallback",
			config:   map[string]any{},
			wantLang: "en",
			wantSub:  "Proceed to Payment",
		},
		{
			name:     "nil config -> en fallback",
			config:   nil,
			wantLang: "en",
			wantSub:  "Proceed to Payment",
		},
		{
			name:     "invalid lang ignored -> en fallback",
			config:   map[string]any{"lang": "fr"},
			wantLang: "en",
			wantSub:  "Proceed to Payment",
		},
		{
			name:     "ar override",
			config:   map[string]any{"lang": "ar"},
			wantLang: "ar",
			wantSub:  "الانتقال إلى الدفع",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &dbstore.Form{Config: tc.config}
			ls := h.effectiveLangStrings(ctx, f)
			if ls.Lang != tc.wantLang {
				t.Fatalf("lang = %q, want %q", ls.Lang, tc.wantLang)
			}
			if ls.Submit != tc.wantSub {
				t.Errorf("submit = %q, want %q", ls.Submit, tc.wantSub)
			}
		})
	}
}

// validFormLang accepts exactly the five supported codes.
func TestValidFormLang(t *testing.T) {
	ok := map[string]bool{
		"en": true, "ms": true, "zh": true, "ta": true, "ar": true,
		"fr": false, "": false, "EN": false, "de": false,
	}
	for code, want := range ok {
		if got := validFormLang(code); got != want {
			t.Errorf("validFormLang(%q) = %v, want %v", code, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// The buyer-facing dictionary completeness guard.
// ---------------------------------------------------------------------------
//
// WHAT IT IS FOR. formLang.go holds the SYSTEM strings the public form renderer
// serves to a buyer (formLangs — one langStrings per locale). A value left blank
// in a locale renders as EMPTY TEXT on a real page: HTTP 200, no error, no raw
// key, nothing in the log. t_26d52622 is what that looks like. PR 770 added
// MenuStockChanged and did not add it to this guard's field list, so
//
//	formLang.go  MenuStockChanged: ""   // ar
//
// built clean, the whole `go test ./internal/httpapi/` package stayed green
// (326.696s), and the served menu HTML still carried
//
//	data-lbl-stockchanged=""        <-- the notice renders BLANK
//	data-lbl-soldout="نفدت الكمية"  <-- the neighbouring key is fine
//
// A buyer whose basket still held a line that had since sold out was told
// NOTHING while the line was dropped. That is worse than the raw-key fallback
// the dashboard guard was written for: nothing looks broken, so only reading the
// page shows it.
//
// WHY THE COVERED SET IS DERIVED, NOT LISTED. The guard used to check a
// HAND-WRITTEN list of 10 field names against a struct that declares 66
// buyer-facing strings (67 fields including Lang), so the other 56 were unguarded
// and a field added later was unguarded by default — which is exactly how
// MenuStockChanged shipped. langStringFields walks the struct's own string fields
// by reflection, so every field is guarded by CONSTRUCTION and cannot fall out of
// the guard by being forgotten. Two things keep the walk honest instead of
// decorative:
// minLangStringFields floors how many fields it must find (a walk that finds
// nothing fails rather than certifying nothing), and mustCoverLangStringFields
// pins by name the field this guard was widened for.
//
// WHAT THIS GUARD DOES NOT COVER (stated honestly)
//   * Translation QUALITY — whether the Tamil says what the English says. Only a
//     fluent review can judge that.
//   * A string hardcoded in a template that reads no field at all, and a field
//     the renderer never emits. Those are questions about the RENDERER, not
//     about the dictionary this file guards.
//   * The reverse direction: whether every data-lbl-* attribute the menu
//     templates serve is backed by a langStrings field. Also a renderer question.
//   * Any OTHER dictionary in the repo — the dashboard's i18nDict.ts carries its
//     own guard (web/src/lib/i18n.test.ts).

// minLangStringFields floors the derived walk. langStrings declares 66
// buyer-facing string fields today (67 fields; Lang is excluded because it
// carries the language CODE, not a label, and is compared against the dictionary
// key below), so a walk that finds far fewer has lost part of its subject and
// must fail loudly rather than certify a dictionary it never examined.
const minLangStringFields = 60

// mustCoverLangStringFields is pinned BY NAME on top of the walk — the field this
// guard is known to be about. The walk is the mechanism; the pin keeps the
// incident visible, because MenuStockChanged (t_26d52622) shipped blanked and
// rendered an empty notice to the buyer precisely while nothing named it.
var mustCoverLangStringFields = []string{"MenuStockChanged"}

// langStringFields derives the covered field names from the struct type itself,
// so a string field added to langStrings is guarded without anyone remembering
// to add it here. Lang is excluded: it carries the language code.
func langStringFields() []string {
	t := reflect.TypeOf(langStrings{})
	out := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Name == "Lang" || f.Type.Kind() != reflect.String {
			continue
		}
		out = append(out, f.Name)
	}
	sort.Strings(out)
	return out
}

// langProblem is one completeness finding: which locale, which field, what is
// wrong. Field is "Lang" for a key/Lang mismatch and "" for a problem with the
// dictionary itself (a key that is not a supported language).
type langProblem struct {
	lang  string
	field string
	value string
	note  string
}

func (p langProblem) String() string {
	name := p.lang
	if p.field != "" {
		name += "." + p.field
	}
	if p.value != "" {
		name += " = " + strconv.Quote(p.value)
	}
	return name + ": " + p.note
}

// formLangsProblems reports every completeness problem in dict, sorted so the
// message is deterministic. It takes the dictionary as an ARGUMENT rather than
// reading the global, because that is what lets the guard be shown to BITE: the
// test below hands it a copy of the real dictionary with one field blanked at a
// time and requires a finding for each one.
func formLangsProblems(dict map[string]langStrings) []langProblem {
	fields := langStringFields()
	var problems []langProblem
	for code, ls := range dict {
		if !validFormLang(code) {
			problems = append(problems, langProblem{
				lang: code, note: "dictionary key is not a supported language",
			})
		}
		if ls.Lang != code {
			problems = append(problems, langProblem{
				lang: code, field: "Lang", value: ls.Lang,
				note: "Lang does not match the dictionary key",
			})
		}
		v := reflect.ValueOf(ls)
		for _, name := range fields {
			s := v.FieldByName(name).String()
			if strings.TrimSpace(s) == "" {
				problems = append(problems, langProblem{
					lang: code, field: name, value: s,
					note: "blank value — this label renders as empty text to the buyer",
				})
			}
		}
	}
	sort.Slice(problems, func(i, j int) bool { return problems[i].String() < problems[j].String() })
	return problems
}

// renderLangProblems formats findings for an error message.
func renderLangProblems(problems []langProblem) string {
	lines := make([]string, 0, len(problems))
	for _, p := range problems {
		lines = append(lines, "  "+p.String())
	}
	return strings.Join(lines, "\n")
}

// Every dictionary carries all system strings (no empty translations that could
// leak a blank label onto a buyer-facing form). The covered field set is derived
// by langStringFields, not listed — see the block comment above.
func TestFormLangsCompleteness(t *testing.T) {
	if len(formLangs) != 5 {
		t.Fatalf("formLangs has %d entries, want 5", len(formLangs))
	}

	fields := langStringFields()
	if len(fields) < minLangStringFields {
		t.Fatalf("the walk found only %d buyer-facing string field(s) in langStrings, expected "+
			"at least %d (the struct declares 66 today). A walk that stops finding fields "+
			"certifies a dictionary it never examined, so this guard would pass vacuously — "+
			"did langStrings lose its string fields, or did the walk change?", len(fields), minLangStringFields)
	}
	have := make(map[string]bool, len(fields))
	for _, f := range fields {
		have[f] = true
	}
	for _, f := range mustCoverLangStringFields {
		if !have[f] {
			t.Fatalf("the derived field set does not contain %s, so the field this guard was widened "+
				"for is unguarded again — blanked, it ships green AND renders an empty notice to the "+
				"buyer (t_26d52622)", f)
		}
	}

	if problems := formLangsProblems(formLangs); len(problems) > 0 {
		t.Fatalf("%d buyer-facing string(s) in the dictionary are blank or untranslated. Each one "+
			"renders as EMPTY text on a real page (HTTP 200, no error, nothing in the log) — "+
			"blank a locale's MenuStockChanged and data-lbl-stockchanged=\"\" ships silently, "+
			"dropping a basket line with no word to the buyer:\n%s",
			len(problems), renderLangProblems(problems))
	}
	t.Logf("%d buyer-facing fields × %d locales: no blank values", len(fields), len(formLangs))
}

// TestFormLangsCompletenessGuardBitesOnEveryField is the proof that the guard
// above fails CLOSED, and it runs with the suite rather than once by hand: for
// EVERY field the walk covers, in EVERY locale, it blanks that field (and,
// separately, sets it to whitespace) in a copy of the real dictionary and
// requires exactly one finding naming it. A guard whose covered set is still
// hand-written has already failed once (t_26d52622); a guard that does not fail
// on a blank field is worse than none, because it certifies the thing it does
// not check.
func TestFormLangsCompletenessGuardBitesOnEveryField(t *testing.T) {
	fields := langStringFields()
	// The proof is only as wide as its subject: with a walk that finds nothing,
	// the loops below would bite on nothing and pass.
	if len(fields) < minLangStringFields {
		t.Fatalf("the walk found only %d field(s), expected at least %d — the bite proof would "+
			"pass having tested almost nothing", len(fields), minLangStringFields)
	}

	codes := make([]string, 0, len(formLangs))
	for code := range formLangs {
		codes = append(codes, code)
	}
	sort.Strings(codes)

	blanked := 0
	for _, code := range codes {
		for _, field := range fields {
			for _, value := range []string{"", "   "} {
				dict := cloneFormLangs()
				ls := dict[code]
				reflect.ValueOf(&ls).Elem().FieldByName(field).SetString(value)
				dict[code] = ls

				problems := formLangsProblems(dict)
				if len(problems) != 1 || problems[0].lang != code || problems[0].field != field {
					t.Errorf("setting %s.%s = %q in the dictionary produced %d finding(s):\n%s\n"+
						"want exactly one naming %s.%s — this field is NOT guarded",
						code, field, value, len(problems), renderLangProblems(problems), code, field)
				}
				blanked++
			}
		}
	}
	if want := len(fields) * len(codes) * 2; blanked != want {
		t.Fatalf("the bite proof ran %d of the %d field/locale/value combinations it claims "+
			"(one value per field is missing)", blanked, want)
	}

	// The two dictionary-level checks the walk does not reach, so they cannot go
	// silently dead while the fields above stay guarded. The Lang field carries a
	// code, not a label: a mismatch is what it is checked for.
	dict := cloneFormLangs()
	ls := dict["ms"]
	ls.Lang = "en"
	dict["ms"] = ls
	problems := formLangsProblems(dict)
	if len(problems) != 1 || problems[0].field != "Lang" || problems[0].lang != "ms" {
		t.Errorf("a dictionary whose ms entry declares Lang=en produced %d finding(s):\n%s\n"+
			"want exactly one naming ms.Lang", len(problems), renderLangProblems(problems))
	}

	dict = cloneFormLangs()
	dict["fr"] = formLangs["en"]
	problems = formLangsProblems(dict)
	found := false
	for _, p := range problems {
		if p.lang == "fr" && p.field == "" {
			found = true
		}
	}
	if !found {
		t.Errorf("a dictionary carrying an unsupported language key produced no finding for it:\n%s",
			renderLangProblems(problems))
	}

	t.Logf("blanked and whitespace-blanked all %d field/locale pairs — the guard named every one",
		blanked/2)
}

// cloneFormLangs copies the dictionary deeply enough for a mutation: langStrings
// is a value type built of strings, so a per-key copy is a real copy.
func cloneFormLangs() map[string]langStrings {
	out := make(map[string]langStrings, len(formLangs))
	for code, ls := range formLangs {
		out[code] = ls
	}
	return out
}
