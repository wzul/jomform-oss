package httpapi

// The REST/HTTP door ships its refusals to the caller verbatim: an API-key
// client, curl, the buyer-form JavaScript and the dashboard's fallback all read
// the `error` prose. The API does not know the caller's language (that is a
// per-profile preference it never receives on a JSON call), so that prose must
// be English — a Malay sentence here is read as-is by every caller.
//
// This guard parses this package and fails on any runtime string literal that
// carries a Malay word, naming file:line, so a new refusal cannot slip in Malay
// while the code table and the docs are all English.
//
// WHAT IS DELIBERATELY OUT OF SCOPE (and why), so a future reader does not
// "fix" a legitimate Malay string:
//
//   - the buyer-form LANGUAGE DICTIONARY (formLang.go's `"ms"` entry and the
//     verify/feedback dictionaries), which must stay Malay: it is the ms
//     translation, selected by the buyer's language;
//   - the buyer-facing HTML/CSS/JS templates, which are layout rather than
//     refusal prose and carry their text through those dictionaries;
//   - struct tags and registry prose.
//
// The same rule is asserted for the lead-in constants this package's error-code
// table keys on, in the packages that own them, so a re-worded message cannot
// stay Malay and silently detach its code.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/customform"
	"github.com/wzul/jomform/api/internal/payments"
	"github.com/wzul/jomform/api/internal/urlguard"
)

// malayWords is the evidence list: function words and domain vocabulary that an
// English sentence in this package cannot contain. Homographs shared with
// English or with a machine token are deliberately ABSENT ("sen" is the money
// unit, "Dan" is a name, "sah" is a three-letter token).
var malayWords = strings.Fields(`
ada adalah akan anda antara apa apabila atau bagaimana bagi bahawa belum bukan boleh
cuba dahulu dalam dari dengan dia diperlukan digunakan dihapus dihantar dijumpai
dikemas dikonfigurasi dikunci dibuka ditutup disekat dilangkau dibenarkan dibuang
diminta dijana diimport dieksport diproses ditetapkan disimpan dipadam ditolak
diterima dimusnahkan dipulihkan diterbitkan dicapai dibatalkan dimohon dinyahaktifkan
emel fail gagal harga hanya hari hingga imej ini itu juga jika jumlah kandungan
kadar kata kepada kemudian kecil kekal kerja kunci kurang lagi lama laporan lebih
lepas laluan melalui memerlukan mempunyai masih mata medan mesej mesti minit muat
nama negara negeri nilai nombor pada oleh pada pautan pada padam panjang pengguna
pengurus penghantaran peranan percuma perkhidmatan pernah pesanan pilih produk
rahsia ralat ringgit ruang sahaja sahkan saiz saya sebelum sejarah setiap selepas
semak semua senarai sendiri seperti sesi sila simpan sangat sudah syarikat tahun
tajuk tambah tanpa tapis tarikh telefon telah tetapi tidak tiada tidak tipu tong
tukar untuk wajib yang
`)

var malayWordSet = func() map[string]bool {
	m := make(map[string]bool, len(malayWords))
	for _, w := range malayWords {
		m[w] = true
	}
	return m
}()

// malayWordIn returns the first Malay word in s. Words are compared case- and
// punctuation-insensitively, on letter runs, so "Borang" and "borang," both
// count.
func malayWordIn(s string) (string, bool) {
	for _, f := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z')
	}) {
		if malayWordSet[f] {
			return f, true
		}
	}
	return "", false
}

// skipLiteralSubtree reports whether a literal is evidence we must not judge:
// a struct tag, the value of a language-dictionary entry ("ms"/"zh"/"ta"/"ar"),
// or a template (markup/CSS/JS) rather than prose.
func isLanguageKey(n ast.Node) bool {
	kv, ok := n.(*ast.KeyValueExpr)
	if !ok {
		return false
	}
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

func TestRuntimeRefusalTextIsEnglish(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	fset := token.NewFileSet()
	files, literals := 0, 0
	var bad []string

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
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
				if isLanguageKey(x) {
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
			// Markup and CSS/JS templates are layout, not refusal prose: they
			// are longer than any sentence this package writes.
			if strings.Contains(val, "<") || len(val) > 400 {
				return true
			}
			literals++
			if w, ok := malayWordIn(val); ok {
				bad = append(bad, fset.Position(bl.Pos()).String()+" ("+w+"): "+val)
			}
			return true
		})
	}

	// A scan that found nothing to scan would pass vacuously. Pin the floor so
	// a broken walk (a renamed dir, a parse change) fails instead.
	if files < 100 || literals < 7000 {
		t.Fatalf("scanned only %d files / %d runtime literals — the walk is broken, not the package", files, literals)
	}
	if len(bad) > 0 {
		t.Errorf("%d runtime string literal(s) in package httpapi still carry Malay text.\n"+
			"The REST door sends this prose to API-key callers, curl and the buyer form "+
			"verbatim, and the API cannot know the caller's language — translate it:\n  %s",
			len(bad), strings.Join(bad, "\n  "))
	}
}

// TestErrcodeLeadInsAreEnglish covers the message PREFIXES the error-code table
// keys on: they are written by other packages but shipped by this door, so a
// Malay lead-in here is Malay on the wire and — worse — a translated row that no
// longer matches its constant.
func TestErrcodeLeadInsAreEnglish(t *testing.T) {
	leadIns := map[string]string{
		"urlguard.InvalidMessage":         urlguard.InvalidMessage("theme.logo_url"),
		"customform.RejectedLeadIn":       customform.RejectedLeadIn,
		"payments.ErrRefundNotRefundable": payments.ErrRefundNotRefundable.Error(),
	}
	for name, msg := range leadIns {
		if w, ok := malayWordIn(msg); ok {
			t.Errorf("%s still carries the Malay word %q: %q", name, w, msg)
		}
	}
}
