package dbstore_test

// A guard beside the three STORE packages whose sentences the REST door ships
// verbatim: dbstore, formfields and payments. api/internal/httpapi has its own
// guard for its own prose (refusal_text_english_guard_test.go, card t_5244358b);
// this one covers the sentences that are WRITTEN here and RETURNED there, so
// translating the door while the store keeps answering in Malay would leave the
// same 4xx body half-translated.
//
// WHY THE STORE'S TEXT IS ON THE WIRE AT ALL: the API does not know the caller's
// language (that is a per-profile UI preference it never receives on a JSON
// call), so the prose in these packages is read as-is by API-key clients, curl,
// the buyer form's JavaScript and the dashboard's fallback. It must be English.
//
// WHAT IS DELIBERATELY OUT OF SCOPE (and why), so a future reader does not
// "fix" a legitimate Malay string:
//
//   - the language dictionaries: any literal that is the VALUE of a map entry
//     keyed "ms"/"zh"/"ta"/"ar". Those are translations, selected by a language,
//     and must stay Malay (the built-in email/confirmation templates live in
//     api/internal/emailtext, which is entirely a catalogue and is not scanned);
//   - markup/CSS/JS templates, which are layout rather than prose;
//   - struct tags;
//   - a DISPLAY string in payments that is not a refusal, and only through
//     paymentsDisplayTextAllowlist. That list is EMPTY today and that is the
//     correct state: the processing-fee consent sentence it used to permit is
//     ENGLISH (payments/fee.go, and it must stay the same sentence as the
//     dashboard's `en` arm — web/src/lib/errorMessageI18n.test.ts fails if the
//     two drift), the receipt PDF prints its Malay labels from the
//     language-keyed receiptText table (translations, skipped above), and the
//     two CHIP checkout titles are English. Malay written in payments is a
//     defect again, not a language decision waiting on a card.
//
// The payments rows that were once needed for the receipt's labels, the
// attachment name and the two CHIP checkout titles are gone with the text they
// allowed. A row that no longer matches is itself a failure below, so an
// allowlist cannot rot into a list of permissions for text that is no longer
// there — it either permits a literal the walk reached, or it fails.
//
// The second test pins the LEAD-INS the httpapi error-code table keys on. A code
// is attached by matching the sentence, so a translated message with an
// untranslated key silently ships `request.badRequest` — the dashboard then
// prints its generic copy and the merchant never learns the cause.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/formfields"
	"github.com/wzul/jomform/api/internal/httpapi"
	"github.com/wzul/jomform/api/internal/payments"
)

// storeMalayWords is the evidence list: function words and domain vocabulary an
// English sentence here cannot contain. Homographs shared with English or with a
// machine token are deliberately ABSENT ("sen" is the money unit, "dan" is a
// name, "sah" is a three-letter token, "ms" is a language code).
var storeMalayWords = strings.Fields(`
ada adalah akan anda antara apa apabila atau bagaimana bagi bahawa belum bukan boleh
borang cuba dahulu dalam dari dengan dia diperlukan digunakan dihapus dihantar dijumpai
dikemas dikonfigurasi dikunci dibuka ditutup disekat dilangkau dibenarkan dibuang
diminta dijana diimport dieksport diproses ditetapkan disimpan dipadam ditolak
diterima dimusnahkan dipulihkan diterbitkan dicapai dibatalkan dimohon dinyahaktifkan
emel fail gagal harga hanya hari hingga imej ini itu juga jika jumlah kandungan
kadar kata kepada kemudian kecil kekal kerja kunci kurang lagi lama laporan lebih
lepas laluan melalui memerlukan mempunyai masih mata medan mesej mesti minit muat
nama negara negeri nilai nombor pada oleh pautan padam panjang pengguna
pengurus penghantaran peranan percuma perkhidmatan pernah pesanan pilih produk
rahsia ralat ringgit ruang sahaja sahkan saiz saya sebelum sejarah setiap selepas
semak semua senarai sendiri seperti sesi sila simpan sangat sudah syarikat tahun
tajuk tambah tanpa tapis tarikh telefon telah tetapi tidak tiada tipu tong
tukar untuk wajib yang
jualan langganan bayaran pembayaran tetapan zon poskod daerah berat pelanggan
kad pesakit kadar medan pilihan blok lampiran resit pemindahan
`)

var storeMalayWordSet = func() map[string]bool {
	m := make(map[string]bool, len(storeMalayWords))
	for _, w := range storeMalayWords {
		m[w] = true
	}
	return m
}()

// malayWordInStoreText returns the first Malay word in s, comparing case- and
// punctuation-insensitively on letter runs.
func malayWordInStoreText(s string) (string, bool) {
	for _, f := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z')
	}) {
		if storeMalayWordSet[f] {
			return f, true
		}
	}
	return "", false
}

// paymentsDisplayTextAllowlist is the Malay in payments that is NOT a refusal
// and still needs a language decision. It is EMPTY, and that is a measurement,
// not an omission: every display string in the package is either English (the
// fee-consent sentence, the two CHIP checkout titles) or the "ms" half of a
// language-keyed table (the receipt's labels), which the walk above already
// treats as a translation. Nothing in payments needs a permission any more.
//
// The mechanism stays for the next genuinely-Malay display string: a row must
// MATCH a literal the walk actually reached, and a row that matches nothing
// fails below. So a permission here cannot outlive the text it was written for,
// and it cannot be added speculatively.
var paymentsDisplayTextAllowlist = []struct {
	file string
	text string
	why  string
}{}

func allowedPaymentsDisplayText(file, val string) bool {
	for _, a := range paymentsDisplayTextAllowlist {
		if a.file == file && a.text == val {
			return true
		}
	}
	return false
}

// isURLOrPath reports whether the literal is a URL, a route or a file name
// rather than a sentence: no whitespace, and it either carries a scheme or a
// path separator.
func isURLOrPath(s string) bool {
	if s == "" || strings.ContainsAny(s, " 	\n") {
		return false
	}
	return strings.Contains(s, "://") || strings.HasPrefix(s, "/") ||
		strings.Contains(s, "%s/") || strings.Contains(s, "&")
}

// storeGuardDirs are scanned relative to this package's directory.
var storeGuardDirs = []string{"../dbstore", "../formfields", "../payments"}

// isLanguageDictionaryValue reports whether the literal is a map VALUE whose key
// is a language code — a translation, not leaked prose.
func isLanguageDictionaryValue(kv *ast.KeyValueExpr) bool {
	bl, ok := kv.Key.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return false
	}
	v, err := strconv.Unquote(bl.Value)
	if err != nil {
		return false
	}
	switch v {
	case "ms", "zh", "ta", "ar":
		return true
	}
	return false
}

func TestStoreRefusalTextIsEnglish(t *testing.T) {
	fset := token.NewFileSet()
	files, literals := 0, 0
	var bad []string
	// Which allowlist rows the walk actually MATCHED. A row is a permission for
	// one exact literal in one file; once that literal is reworded away the row
	// matches nothing and is checked below (see the "matches nothing" assertion).
	usedPaymentsDisplayRows := map[string]bool{}

	for _, dir := range storeGuardDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(dir, name)
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			f, err := parser.ParseFile(fset, path, src, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			files++

			skip := map[ast.Node]bool{}
			ast.Inspect(f, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.Field:
					if x.Tag != nil {
						skip[x.Tag] = true
					}
				case *ast.KeyValueExpr:
					if isLanguageDictionaryValue(x) {
						skip[x.Value] = true
					}
				}
				return true
			})

			ast.Inspect(f, func(n ast.Node) bool {
				if skip[n] {
					return false
				}
				bl, ok := n.(*ast.BasicLit)
				if !ok || bl.Kind != token.STRING {
					return true
				}
				val, err := strconv.Unquote(bl.Value)
				if err != nil {
					return true
				}
				// Markup and CSS/JS templates are layout, not prose; a URL or
				// path is a machine token ("fail" is Malay for "file", and a
				// status segment is not a sentence).
				if strings.Contains(val, "<") || len(val) > 400 || isURLOrPath(val) {
					return true
				}
				literals++
				if dir == "../payments" && allowedPaymentsDisplayText(name, val) {
					usedPaymentsDisplayRows[name+"\x00"+val] = true
					return true
				}
				if w, ok := malayWordInStoreText(val); ok {
					bad = append(bad, fset.Position(bl.Pos()).String()+" ("+w+"): "+val)
				}
				return true
			})
		}
	}

	// A scan that found nothing to scan would pass vacuously, and a renamed
	// package dir would then "pass" while covering nothing.
	if files < 60 || literals < 1800 {
		t.Fatalf("scanned only %d files / %d runtime literals across %v — the walk is broken, not the packages",
			files, literals, storeGuardDirs)
	}
	if len(bad) > 0 {
		t.Errorf("%d runtime string literal(s) in dbstore/formfields/payments still carry Malay text.\n"+
			"These sentences are returned on 4xx bodies the REST door sends verbatim, and the API "+
			"cannot know the caller's language — translate them:\n  %s",
			len(bad), strings.Join(bad, "\n  "))
	}

	// The allowlist may not outlive the text it allows. A row is a permission for
	// ONE exact literal in ONE file: once that literal is reworded or removed,
	// the row matches nothing, and it silently becomes a standing permission for
	// the same value if it ever comes back — while telling every future reader
	// that the package still contains Malay that needs a language decision. The
	// row must go with the text.
	var dead []string
	for _, a := range paymentsDisplayTextAllowlist {
		if !usedPaymentsDisplayRows[a.file+"\x00"+a.text] {
			dead = append(dead, fmt.Sprintf("%s: %q — %s", a.file, a.text, a.why))
		}
	}
	if len(dead) > 0 {
		t.Errorf("%d paymentsDisplayTextAllowlist row(s) no longer match any runtime literal.\n"+
			"The literal they allow was reworded or removed; delete the row (its `why` is now "+
			"stale prose about code that no longer exists):\n  %s",
			len(dead), strings.Join(dead, "\n  "))
	}
}

// TestStoreRefusalLeadInsStayKeyedToTheirCodes pins the OTHER direction: these
// sentences are the keys (or key prefixes) of httpapi's errCodeByMessage /
// errCodeByPrefix tables. If a message is translated and the table is not — or
// the reverse — ErrCodeFor answers "" and the 4xx ships with the class code, so
// the dashboard prints its generic sentence instead of the real refusal.
//
// The assertion is by CODE, not by wording, so a later re-wording is free as
// long as the table moves with it.
func TestStoreRefusalLeadInsStayKeyedToTheirCodes(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		want string
	}{
		{"dbstore.ErrWorkspaceSlugTaken", dbstore.ErrWorkspaceSlugTaken.Error(), httpapi.CodeWorkspaceSlugTaken},
		{"dbstore.ErrWorkspaceSlugLength", dbstore.ErrWorkspaceSlugLength.Error(), httpapi.CodeWorkspaceSlugBad},
		{"dbstore.ErrWorkspaceSlugChars", dbstore.ErrWorkspaceSlugChars.Error(), httpapi.CodeWorkspaceSlugBad},
		{"dbstore.ErrWorkspaceSlugReserved", dbstore.ErrWorkspaceSlugReserved.Error(), httpapi.CodeWorkspaceSlugBad},
		{"dbstore.ErrWorkspaceNameLength", dbstore.ErrWorkspaceNameLength.Error(), httpapi.CodeWorkspaceNameTooLong},
		{"dbstore.ErrOutOfStock", dbstore.ErrOutOfStock.Error(), httpapi.CodeProductOutOfStock},
		{"dbstore.ShippingInvalidLeadIn", dbstore.ShippingInvalidLeadIn + "zone", httpapi.CodeShippingInvalid},
		{"dbstore.ShippingSetInUseLeadIn", "zone set " + dbstore.ShippingSetInUseLeadIn, httpapi.CodeShippingSetInUse},
		{"payments.ErrNotConfigured", payments.ErrNotConfigured.Error(), httpapi.CodePaymentNotConfigured},
		{"payments.ErrRefundNotRefundable", payments.ErrRefundNotRefundable.Error(), httpapi.CodeRefundNotAllowed},
		// The publish-time validation refusals: no specific code exists for
		// these (they are 400s the page renders from the body), so the
		// assertion is only that they are English — see the walk above.
		{"formfields.UnsupportedBlockMessage", formfields.UnsupportedBlockMessage("file"), ""},
	}
	for _, c := range cases {
		if w, ok := malayWordInStoreText(c.msg); ok {
			t.Errorf("%s still carries the Malay word %q: %q", c.name, w, c.msg)
		}
		if c.want == "" {
			continue
		}
		if got := httpapi.ErrCodeFor(c.msg); got != c.want {
			t.Errorf("%s = %q — httpapi.ErrCodeFor answered %q, want %q. "+
				"A message and its error-code table key must move together, or the "+
				"dashboard cannot name the refusal.", c.name, c.msg, got, c.want)
		}
	}
}
