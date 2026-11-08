package mcpsrv

// Malay WIRE TEXT guard — the strings an MCP client actually reads.
//
// WHY THIS FILE EXISTS. docs_mcp_page_description_test.go ties the docs page to
// each tool's Description, and the registry prose (the Description literals and
// the `jsonschema:"…"` field descriptions) was translated to English in one
// pass. NONE of that covered the text a caller gets back from a FAILED or
// SUCCESSFUL call: errors.New("tiada …"), fmt.Errorf("… tidak dijumpai"), and
// the `note`/`error`/`hint` fields the tool RESULT carries.
//
// The asymmetry that made it invisible to every other test: an agent that calls
// a tool with a bad argument reads English metadata (name, description, schema)
// and then a Malay refusal. Nothing compared the two, so half the tool surface
// could stay in one language with the whole suite green.
//
// THE GUARD. Parse every non-test file in this package and check each string
// literal that can reach the wire:
//
//   - struct tags are skipped — a `jsonschema:"…"` tag is registry prose, and a
//     `json:"…"` tag is a field name (machine token, never translated);
//   - every literal under a `Description:` key is skipped, including the
//     concatenations and the `str("…")` helper calls that build one — that is
//     tool prose, which has its own docs guard.
//
// Everything else must not contain a Malay word from the refusal vocabulary
// below. Machine tokens (snake_case ids, enum values, ref codes, https URLs)
// are untouched by the check: it only looks at word-shaped tokens, so
// `price_sen`, `one_time|subscription` and `readwrite` can never trip it.
//
// BITES. Re-introducing one translated refusal (e.g. "borang tidak dijumpai" in
// place of "form not found") makes this test fail and names file:line. Verified
// by hand on this branch — see the card's evidence.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// malayWireWords is the refusal vocabulary: Malay function words plus the
// verbs/nouns these refusals are built from. It is deliberately a word list and
// not a language model — a new Malay sentence built from ordinary words such as
// "tidak", "wajib" or "dijumpai" is caught, which is every refusal in this
// package today. Words that are also ordinary English ("sen" in price_sen,
// "as", "air", "bank") are deliberately absent.
var malayWireWords = map[string]struct{}{}

func init() {
	for _, w := range strings.Fields(`
		adalah akan alamat anda antara apa asal atau bagaimana baharu bahawa banyak
		batal batalkan bayar bebas belum berjaya bilangan bila borang boleh bukan
		butiran cadangkan cari cuba dan dari dalam dapat dibenar dibenarkan dibuang
		dibuka dicadangkan diisi dijumpai dijalankan dikembalikan dikonfigurasi
		dilaksanakan dinilai dijangka diperlukan dipadam dipakai dipaparkan diperiksa
		disebabkan disahkan disanitasi disekat disemak disimpan disokong ditolak
		diterbitkan diterima ditutup dihantar draf gagal gambar guna gunakan had
		hantar hari harus halaman harga hendak hilang hanya hantar jika ikut ialah
		ini itu jawapan jenis juga jumlah jika jika kepada kedai kekal kembali
		kerana kesilapan kira kod kosong kunci lagi lalai lebih laksanakan
		maaf maklumat masalah masih mana mana-mana masukkan masa maksimum mesti
		medan melalui memadamkan membaca memerlukan menambah mencipta mengandungi
		mengapa mengemas menggunakan menghantar menunjukkan menyimpan menyokong
		menyebabkan mereka nilai nombor pada padam padan pautan pelanggan pemadaman
		pemilik peniaga penuh perbandingan perlu pernah pesanan pilih pilihan
		pulihkan pulihan ralat ratusan rujukan sahaja sahkan saiz salah sana
		sasaran sedia sebelum sebagai seperti semakan semasa semua semula senarai
		selepas selesai sesi setiap sila simpan soalan sokong supaya taip tambah
		tamat tanpa tapis terkini terakhir terhad terlalu terdapat tersedia tertutup
		tertutup tetapan tidak tiada tolak tutup ubah untuk wang wajib wujud yang
	`) {
		malayWireWords[w] = struct{}{}
	}
}

// wireWords lowercases and splits a literal into letter runs. Digits and
// punctuation are separators, so `price_sen` yields {price, sen} and
// `readwrite` stays one token; neither is in the vocabulary.
func wireWords(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return r < 'a' || r > 'z'
	})
}

func TestRuntimeWireTextIsEnglish(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob *.go: %v", err)
	}
	fset := token.NewFileSet()
	var offenders []string
	checked := 0
	parsed := 0

	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		parsed++

		skip := map[*ast.BasicLit]bool{}
		// Struct tags: type tags (`json:"…"`) and registry field descriptions.
		ast.Inspect(af, func(n ast.Node) bool {
			if f, ok := n.(*ast.Field); ok && f.Tag != nil {
				skip[f.Tag] = true
			}
			return true
		})
		// Schema-description builders: a local helper that stores its argument
		// as a jsonschema Description, e.g.
		//
		//	str := func(desc string) *jsonschema.Schema {
		//		return &jsonschema.Schema{Type: "string", Description: desc}
		//	}
		//
		// The literals handed to it ("password": str("Account password, …")) are
		// registry prose in a call argument, not wire text — the Description
		// subtree skip below cannot see them, because the value there is a param.
		descBuilders := map[string]bool{}
		ast.Inspect(af, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
				return true
			}
			name, ok := as.Lhs[0].(*ast.Ident)
			if !ok {
				return true
			}
			fn, ok := as.Rhs[0].(*ast.FuncLit)
			if !ok {
				return true
			}
			params := map[string]bool{}
			for _, fld := range fn.Type.Params.List {
				for _, nm := range fld.Names {
					params[nm.Name] = true
				}
			}
			ast.Inspect(fn.Body, func(m ast.Node) bool {
				kv, ok := m.(*ast.KeyValueExpr)
				if !ok {
					return true
				}
				if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Description" {
					if v, ok := kv.Value.(*ast.Ident); ok && params[v.Name] {
						descBuilders[name.Name] = true
					}
				}
				return true
			})
			return true
		})
		if len(descBuilders) > 0 {
			ast.Inspect(af, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				fn, ok := call.Fun.(*ast.Ident)
				if !ok || !descBuilders[fn.Name] {
					return true
				}
				for _, a := range call.Args {
					if lit, ok := a.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						skip[lit] = true
					}
				}
				return true
			})
		}
		// Tool prose: the whole value under `Description:` — concatenations and
		// the str("…") helper calls included.
		ast.Inspect(af, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			if id, ok := kv.Key.(*ast.Ident); !ok || id.Name != "Description" {
				return true
			}
			ast.Inspect(kv.Value, func(m ast.Node) bool {
				if lit, ok := m.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					skip[lit] = true
				}
				return true
			})
			return true
		})

		ast.Inspect(af, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING || skip[lit] {
				return true
			}
			checked++
			txt, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				txt = lit.Value
			}
			for _, w := range wireWords(txt) {
				if _, bad := malayWireWords[w]; bad {
					body := txt
					if len(body) > 140 {
						body = body[:140] + "…"
					}
					offenders = append(offenders, fmt.Sprintf("%s:%d: Malay word %q in %q",
						name, fset.Position(lit.Pos()).Line, w, body))
					break
				}
			}
			return true
		})
	}

	// A scan that finds nothing to scan would pass vacuously — e.g. if the
	// package were moved, or a parse started skipping files.
	if parsed < 20 || checked < 400 {
		t.Fatalf("guard scanned too little (files=%d, literals=%d) — the scan is broken, not the corpus", parsed, checked)
	}

	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Fatalf("%d agent-visible string literal(s) still carry Malay wire text:\n%s\n\n"+
			"Tool metadata (Description, jsonschema) is English; the text a caller gets back must be too.",
			len(offenders), strings.Join(offenders, "\n"))
	}
}
