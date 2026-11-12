package payments

// THE ACKNOWLEDGEMENT SENTENCE IS ENGLISH, AND IT IS THE ONE THE DOCS QUOTE
// (card t_541a31d5).
//
// THE DEFECT. FeeAcknowledgementText was the MALAY sentence:
//
//	"Saya faham caj pemprosesan ini dipindahkan kepada pembayar dan ia tertakluk
//	 kepada peraturan acquirer/pengeluar kad."
//
// It is served with the settings on BOTH machine surfaces (REST
// GET /v1/rest/settings/payment-fees → acknowledgement_text, MCP
// get_payment_fees → acknowledgement_text), and the dashboard printed whatever
// arrived (`{feeAckText || t("set.feesAck")}`), so an English Settings page showed
// Malay. Meanwhile https://jomform.com/docs/payment-fees already quoted the
// ENGLISH sentence and told the reader "the sentence is served by the API" — the
// docs and the API disagreed on prod.
//
// WHY THE SERVED SENTENCE MUST BE ENGLISH RATHER THAN THE MERCHANT'S LANGUAGE: a
// JSON call never carries the caller's language (issue #6177). A sentence written
// on this side reaches EVERY locale in one language, so the only honest thing it
// can be is the canonical English disclosure — the language the docs, the MCP
// tool description and this constant all agree on. The per-locale sentence the
// MERCHANT reads is the dashboard's own dictionary arm (set.feesAck), pinned
// against THIS constant by web/src/lib/errorMessageI18n.test.ts.
//
// WHAT IS ASSERTED, and why each half is needed:
//
//	1. the constant is the canonical English sentence, exactly — a reword is a
//	   deliberate act in three places (here, the dictionary's `en` arm, the docs),
//	   not a drive-by edit;
//	2. it carries no Malay word. Pinned by LANGUAGE rather than by remembering
//	   the old string, so a reworded Malay sentence cannot slip back in;
//	3. nothing about the RECORD depends on the text: only
//	   (workspace_id, acknowledged_by, acknowledged_at) is stored (migration
//	   000082), so there is no stored copy to migrate and no merchant whose
//	   recorded agreement changes when the sentence is reworded.

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// malayWords are words English does not use — the same list the dashboard's
// dictionary guard uses (web/src/lib/errorMessageI18n.test.ts), so the API and
// the dashboard agree on what "this string is Malay" means. Deliberately
// excludes short words with an English homograph ("dan", "ada", "di", "ke") so
// this cannot fire on a name or an abbreviation.
var malayWords = regexp.MustCompile(`\b(` + strings.Join([]string{
	"yang", "anda", "tidak", "untuk", "dengan", "saya", "boleh", "dalam",
	"adalah", "kepada", "atau", "juga", "sudah", "belum", "kami", "mereka",
	"kita", "jika", "semak", "emel", "bila", "bagi", "oleh", "tersebut",
	"sesuatu", "kerana", "daripada", "sahaja", "tetapi", "selepas", "sebelum",
	"semula", "sila", "mesti", "telah", "pernah", "memang", "semua", "banyak",
	"jualan", "borang", "tetapan", "pesanan", "produk", "kedai", "bahasa",
	"pengguna", "pembayaran", "resit", "caj", "butiran", "medan", "simpan",
	"hantar", "padam", "tambah", "cari", "pilih", "tukar", "muat", "ralat",
}, "|") + `)\b`)

// looksMalay is the language test. Word-bounded, so "caj" cannot match inside an
// English word and "resit" cannot match inside "resitence".
func looksMalay(s string) bool { return malayWords.MatchString(s) }

// readMigration reads a migration by filename, resolved from THIS test file so
// the assertion is about the schema in the tree and not about the working
// directory the test binary happens to run in.
func readMigration(t *testing.T, name string) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve this test file's path")
	}
	path := filepath.Join(filepath.Dir(self), "..", "db", "migrations", name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

const canonicalFeeAck = "I understand this platform fee is charged to the buyer on top of the price and is separate from the fee CHIP charges me."

// TestFeeAcknowledgementTextIsTheCanonicalEnglishSentence is the reported defect.
func TestFeeAcknowledgementTextIsTheCanonicalEnglishSentence(t *testing.T) {
	if FeeAcknowledgementText != canonicalFeeAck {
		t.Fatalf("FeeAcknowledgementText = %q\nwant the canonical English sentence %q\n"+
			"the docs at /docs/payment-fees quote that sentence and say it is served by the API, "+
			"so a different value here makes the docs and the API contradict each other on prod "+
			"(t_541a31d5)", FeeAcknowledgementText, canonicalFeeAck)
	}
	if looksMalay(FeeAcknowledgementText) {
		t.Fatalf("the served acknowledgement sentence carries a Malay word: %q — an English "+
			"dashboard prints it verbatim, which is the defect t_541a31d5 reports",
			FeeAcknowledgementText)
	}
	if strings.Contains(FeeAcknowledgementText, "{") {
		t.Fatalf("the served sentence carries a placeholder: %q — no caller substitutes it, so a "+
			"merchant (and an agent) reads the braces", FeeAcknowledgementText)
	}
}

// TestTheLanguageTestCatchesTheShippedSentence is the BITE. The two assertions
// above are only worth something if the detector can see the sentence that
// actually shipped; a detector that matches nothing is green on any tree.
func TestTheLanguageTestCatchesTheShippedSentence(t *testing.T) {
	const shipped = "Saya faham caj pemprosesan ini dipindahkan kepada pembayar dan ia tertakluk kepada peraturan acquirer/pengeluar kad."
	if !looksMalay(shipped) {
		t.Fatalf("the language test does not flag the Malay sentence that shipped: %q", shipped)
	}
	for _, english := range []string{
		canonicalFeeAck,
		"Card 2.00% + RM1.00",
		"The fee is added on top of the price and the total is grossed up.",
		"Payment receipt — JF-0001",
	} {
		if looksMalay(english) {
			t.Fatalf("the language test flags English text: %q", english)
		}
	}
}

// TestTheRecordDoesNotCarryTheSentence pins the reason no data migration is
// needed: the acknowledgement stores WHO and WHEN, never the bytes they read.
// If a future change starts storing prose, this fails and the migration (and the
// "does an old record still say what the merchant agreed to?" question) becomes
// real work instead of a silent assumption.
func TestTheRecordDoesNotCarryTheSentence(t *testing.T) {
	// The migration is the schema of record; read it rather than the Go struct, so
	// a column added in SQL without a struct change cannot hide.
	sql := readMigration(t, "000082_payment_fees.up.sql")
	const table = "workspace_fee_acknowledgements"
	start := strings.Index(sql, "CREATE TABLE "+table+" (")
	if start < 0 {
		t.Fatalf("migration 000082 no longer creates %s — this test assumed where the record lives", table)
	}
	block := sql[start:]
	if end := strings.Index(block, "\n);"); end > 0 {
		block = block[:end]
	}
	var cols []string
	for _, line := range strings.Split(block, "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		head := strings.ToUpper(fields[0])
		if head == "CONSTRAINT" || head == "PRIMARY" || head == "UNIQUE" || strings.HasPrefix(head, "--") {
			continue
		}
		cols = append(cols, strings.ToLower(fields[0]))
	}
	want := []string{"workspace_id", "acknowledged_by", "acknowledged_at"}
	if strings.Join(cols, ",") != strings.Join(want, ",") {
		t.Fatalf("%s columns = %v, want %v\n"+
			"The acknowledgement stores WHO and WHEN, never the bytes they read. If a prose "+
			"column is added, the served sentence stops being the whole record — and every "+
			"existing row then carries the sentence in the language it was written in, which is "+
			"the migration this test exists to force someone to think about (t_541a31d5).",
			table, cols, want)
	}
}
