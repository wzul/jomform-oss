package httpapi

// The API's prose reaches its caller verbatim: an API-key client, curl, the
// buyer form's JavaScript, an MCP tool result or the dashboard's fallback all
// read the `error` string. The API never receives the caller's language (that
// is a per-profile UI preference, and it is not part of a JSON call), so every
// sentence it writes must be English — a Malay one is read as-is by everyone.
//
// WHY THIS GUARD EXISTS SEPARATELY FROM refusal_text_english_guard_test.go
// (card t_5244358b) AND dbstore's store guard (card t_47403fc3): those two cover
// the door and the stores. The packages scanned HERE are the ones whose
// sentences the door ships without rewriting — the object store's upload
// refusals, the courier connect/amount refusals, the LLM form parser, the
// feedback service, the outbound-URL guard, auth's lockout/link prose, the LLM
// client's deployment-state sentinels, the mail breaker's 503 body and the
// coarse rate limiter's 429 body. They were the remaining Malay on the wire.
//
// WHAT IS DELIBERATELY OUT OF SCOPE (so a later reader does not "fix" it):
//
//   - language dictionaries: a literal that is the VALUE of a map entry keyed
//     "ms"/"zh"/"ta"/"ar" (httpapi/formLang.go and its verify/feedback siblings)
//     is the Malay translation, selected by the caller's language;
//   - markup/CSS/JS templates, struct tags, and URLs/paths — layout and machine
//     tokens, not sentences;
//   - `objstore`'s state tables are not scanned as prose: "negeri sembilan" is a
//     Malaysian state's own name and an INPUT ALIAS a merchant may type, so the
//     word "negeri" is not an evidence word here;
//   - the courier STOP LABELS in shipping/lalamove.go are the text a third-party
//     courier page prints, not a refusal, and are allowed by exact value below;
//   - internal log lines (`slog`/`log.Printf`) in these packages were translated
//     anyway where they were trivial, but they are not what this guard is for.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/objstore"
)

// wireMalayWords is the evidence list: function words and domain vocabulary an
// English sentence in these packages cannot contain. Homographs that are also
// English or machine tokens are deliberately absent ("sen" is the money unit,
// "dan" is a name, "sah" is a token, "fail" is the English verb).
var wireMalayWords = strings.Fields(`
ada adalah akan anda antara apa apabila atau bagaimana bagi bahawa belum bukan boleh
cuba dahulu dalam dari dengan dia diperlukan digunakan dihapus dihantar dijumpai
dikemas dikonfigurasi dikunci dibuka ditutup disekat dilangkau dibenarkan dibuang
diminta dijana diimport dieksport diproses ditetapkan disimpan dipadam ditolak
diterima dimusnahkan dipulihkan diterbitkan dicapai dibatalkan dimohon dinyahaktifkan
dimampatkan dimulakan diisi disimpan
emel gagal harga hanya hari hingga imej ini itu juga jika jumlah kandungan
kadar kata kepada kemudian kecil kekal kerja kunci kurang lagi lama laporan lebih
lepas laluan melalui memerlukan mempunyai masih mata medan mesej mesti minit muat
nama nilai nombor pada oleh pautan padam panjang pengguna
pengurus penghantaran peranan percuma perkhidmatan pernah pesanan pilih produk
rahsia ralat ringgit ruang sahaja sahkan saiz saya sebelum sejarah setiap selepas
semak semua senarai sendiri seperti sesi sila simpan sangat sudah syarikat tahun
tajuk tambah tanpa tapis tarikh telefon telah tetapi tidak tiada tipu tong
tukar untuk wajib yang jualan langganan bayaran pembayaran tetapan poskod
daerah berat pelanggan pilihan blok lampiran resit pemindahan aksara sehari
tamat tempoh papar salah laluan semula dibuang keluar masuk atas bawah
mohon harap maaf kosong penuh terbuka tertutup
`)

var wireMalayWordSet = func() map[string]bool {
	m := make(map[string]bool, len(wireMalayWords))
	for _, w := range wireMalayWords {
		m[w] = true
	}
	return m
}()

// wireMalayWordIn returns the first Malay word in s, comparing case- and
// punctuation-insensitively on letter runs, so "Borang" and "borang," both count.
func wireMalayWordIn(s string) (string, bool) {
	for _, f := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z')
	}) {
		if wireMalayWordSet[f] {
			return f, true
		}
	}
	return "", false
}

// wireDisplayTextAllowlist is the Malay in these packages that is NOT refusal
// prose. Each entry is allowed BY EXACT VALUE AND FILE, so any other Malay
// literal in the same package still fails.
var wireDisplayTextAllowlist = []struct {
	file string
	text string
	why  string
}{
	{"lalamove.go", "Penghantaran",
		"courier stop label printed on the provider's own page, not a refusal"},
	{"lalamove.go", "Penerima",
		"courier stop label printed on the provider's own page, not a refusal"},
	{"shipping.go", "negeri sembilan", "Malaysian state name (an input alias), not prose"},
	{"shipping.go", "n sembilan", "Malaysian state name (an input alias), not prose"},
	{"shipping.go", "semenanjung", "Malaysian region alias a merchant may type, not prose"},
	{"shipping.go", "malaysia timur", "Malaysian region alias a merchant may type, not prose"},
}

func wireAllowedDisplayText(file, val string) bool {
	for _, a := range wireDisplayTextAllowlist {
		if a.file == file && a.text == val {
			return true
		}
	}
	return false
}

// wireIsURLOrPath reports whether the literal is a URL, a route or a file name
// rather than a sentence.
func wireIsURLOrPath(s string) bool {
	if s == "" || strings.ContainsAny(s, " \t\n") {
		return false
	}
	return strings.Contains(s, "://") || strings.HasPrefix(s, "/") ||
		strings.Contains(s, "%s/") || strings.Contains(s, "&")
}

// wireIsLanguageDictionaryValue reports whether the literal is a map VALUE whose
// key is a language code — a translation, not leaked prose.
func wireIsLanguageDictionaryValue(kv *ast.KeyValueExpr) bool {
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

// wireGuardDirs are scanned relative to this package's directory.
var wireGuardDirs = []string{
	"../objstore",
	"../shipping",
	"../formgen",
	"../feedbacksvc",
	"../ssrfguard",
	"../auth",
	"../llm",
	"../email",
	"./middleware",
}

func TestWireTextTheDoorShipsIsEnglish(t *testing.T) {
	fset := token.NewFileSet()
	files, literals := 0, 0
	var bad []string

	for _, dir := range wireGuardDirs {
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
					if wireIsLanguageDictionaryValue(x) {
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
				// path is a machine token.
				if strings.Contains(val, "<") || len(val) > 400 || wireIsURLOrPath(val) {
					return true
				}
				literals++
				if wireAllowedDisplayText(name, val) {
					return true
				}
				if w, ok := wireMalayWordIn(val); ok {
					bad = append(bad, fset.Position(bl.Pos()).String()+" ("+w+"): "+val)
				}
				return true
			})
		}
	}

	// A scan that found nothing to scan would pass vacuously, and a renamed
	// package dir would then "pass" while covering nothing.
	if files < 40 || literals < 800 {
		t.Fatalf("scanned only %d files / %d runtime literals across %v — the walk is broken, not the packages",
			files, literals, wireGuardDirs)
	}
	if len(bad) > 0 {
		t.Errorf("%d runtime string literal(s) in the packages this door ships still carry Malay text.\n"+
			"The API cannot know the caller's language, so this prose is read as-is by API-key clients, "+
			"curl, the buyer form and MCP callers — translate it:\n  %s",
			len(bad), strings.Join(bad, "\n  "))
	}
}

// TestSVGRefusalStaysKeyedToItsCode pins the one message in these packages that
// httpapi's error-code table keys on. The code is attached by matching the
// sentence PREFIX, so translating the sentence without the table (or the table
// without the sentence) would silently ship `request.badRequest` and the
// dashboard would print its generic copy instead of the upload refusal.
func TestSVGRefusalStaysKeyedToItsCode(t *testing.T) {
	msg := objstore.SVGNotXMLError([]byte("/* banner */<svg/>"))
	if msg == "" {
		t.Fatal("SVGNotXMLError accepted a document with text before the root element")
	}
	if w, ok := wireMalayWordIn(msg); ok {
		t.Errorf("the SVG refusal still carries the Malay word %q: %q", w, msg)
	}
	if got := ErrCodeFor(msg); got != CodeUploadSVGInvalid {
		t.Errorf("ErrCodeFor(%q) = %q, want %q — the refusal and the code table must move together",
			msg, got, CodeUploadSVGInvalid)
	}
}
