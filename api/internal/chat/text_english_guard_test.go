package chat

// Malay WIRE/PROMPT TEXT guard for the chat lane (card t_02e83dc7).
//
// WHY THIS FILE EXISTS. The dashboard renders its own five-locale sentences for
// everything a PERSON reads (lib/chatConfirm.ts branches on a machine code, the
// refusal block carries `chat.refused.*` keys, the assistant's empty answer is
// `chat.reply.noAnswer`). The text THIS package writes is therefore read by two
// other audiences:
//
//   - the MODEL, as a tool result: `dispatch`/`proposeWrite` return the refusal
//     sentence so the model can correct a mis-keyed call, and the MCP tools it
//     wraps answer in English since t_67f858f1;
//   - an OPERATOR or an API caller reading a log or a 4xx body: the error
//     strings here are wrapped by httpapi (`chat: …`) and by the confirm route's
//     fallback arm.
//
// Both audiences read English everywhere else in this lane — the system prompt is
// English, and every MCP tool description/refusal is English. A Malay sentence
// left here is the same asymmetry t_67f858f1 closed in mcpsrv: half the surface
// speaks one language and nothing compares the two.
//
// `proposal.go`'s confirm-side wrap is the one that was visibly mixed: it is
// `fmt.Errorf("server refused: %s", detail)` around an MCP refusal that is now
// English, and the web fixtures that document the same wire body
// (web/src/lib/chatConfirm.test.ts) quote that prefix.
//
// WHAT IS CHECKED. Every non-test file in this package is parsed (comments are
// not part of the AST, so a comment quoting a pre-fix Malay sentence is allowed),
// and every string literal that is RUNTIME text is checked against the Malay
// vocabulary below. Skipped, because they are not text:
//
//   - struct tags (`json:"…"`, `jsonschema:"…"`) — machine tokens.
//
// THE TOOL PROSE IS IN SCOPE (card t_ec87d6c7, which removed the two exceptions
// this guard used to carry). `Description:` values and the `json.RawMessage`
// input schemas of submit_feedback / propose_write were left out as "registry
// prose", and that is exactly where the lane's last Malay text lived: the model
// read a Malay `reason` description telling it to write "in the merchant's
// language" and a Malay propose_write description, one line away from an English
// system prompt. A model treats its tool definitions as instructions, so leaving
// them out of this check let an English prompt and a Malay one ship side by side.
//
// Everything else must be free of the vocabulary. Machine tokens are untouched by
// the check: it only looks at word-shaped tokens, so `price_sen`, `totp_code` and
// `one_time|subscription` can never trip it.
//
// BITES. Putting one translated refusal back to Malay (`"server refused: %s"` →
// `"server menolak: %s"`) makes this test fail and names file:line. Verified by
// hand on this branch — see the card's evidence.
//
// WHY A WORD LIST AND NOT A LANGUAGE MODEL. The check has to be deterministic and
// offline. The list below is Malay function words plus the verbs/nouns these
// refusals are built from; words that are also ordinary English ("had", "sen",
// "air", "bank", "as") are deliberately absent so a legitimate English sentence
// cannot be flagged.

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

// malayChatWords is the vocabulary this lane's refusals are built from.
var malayChatWords = map[string]struct{}{}

func init() {
	for _, w := range strings.Fields(`
		adalah akan alamat anda antara apa asal atau bagaimana baharu bahawa banyak
		batal batalkan bayar bebas belum berjaya bilangan bila borang boleh bukan
		butiran cadangkan cadangan cari cuba dan dari dalam dapat dibenar dibenarkan
		dibuang dibuka dicadangkan diisi dijumpai dijalankan dikembalikan dikonfigurasi
		dilaksanakan dinilai dijangka diperlukan dipadam dipakai dipaparkan diperiksa
		disebabkan disahkan disanitasi disekat disemak disimpan disokong ditolak
		diterbitkan diterima ditutup dihantar draf gagal gambar guna gunakan hari
		harus halaman harga hendak hilang hanya hantar jika ikut ialah ini itu jawapan
		jenis juga jumlah kepada kedai kekal kembali kerana kesilapan kira kod kosong
		kunci lagi lalai lebih laksanakan maaf maklumat masalah masih mana mana-mana
		masukkan masa maksimum mesti medan melalui memadamkan membaca memerlukan
		menambah mencipta mengandungi mengapa mengemas menggunakan menghantar
		menunjukkan menyimpan menyokong menyebabkan mereka nilai nombor pada padam
		padan pautan pelanggan pemadaman pemilik peniaga penuh perbandingan perlu
		peranan pernah pesanan pilih pilihan pulihkan pulihan ralat ratusan rujukan
		sahaja sahkan saiz salah sana sasaran sedia sebelum sebagai seperti semakan
		semasa semua semula senarai selepas selesai sesi setiap sila simpan soalan
		sokong supaya taip tambah tamat tanpa tapis terkini terakhir terhad terlalu
		terdapat tersedia tertutup tetapan tidak tiada tolak tutup ubah untuk wang
		wajib wujud yang
		akses aksara alat apa argumen asal balas baca besar berubah dibaca dicapai
		dikenali diselesaikan daripada emel hujah hujahnya jawapan kad kelayakan
		khusus kontrak kontraknya maklum manusia membenarkan mesej menetapkan
		menolak menulisnya menyediakan menyelesaikan minta pelayan pembantu
		pemusnah pendaftaran pengesahan pengguna penghantaran penulisan penerangan
		permintaan perubahan rekod sahkan saya sepadan sistem skema sudah tindakan
	`) {
		malayChatWords[w] = struct{}{}
	}
}

// chatWords lowercases and splits a literal into letter runs. Digits and
// punctuation are separators, so `price_sen` yields {price, sen} and neither is in
// the vocabulary.
func chatWords(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return r < 'a' || r > 'z'
	})
}

// malayWordsIn returns the vocabulary hits in one literal, in order of appearance.
func malayWordsIn(s string) []string {
	var hits []string
	for _, w := range chatWords(s) {
		if _, bad := malayChatWords[w]; bad {
			hits = append(hits, w)
		}
	}
	return hits
}

func TestChatTextIsEnglish(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob *.go: %v", err)
	}
	fset := token.NewFileSet()
	var offenders []string
	checked, parsed := 0, 0

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
		ast.Inspect(af, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.Field:
				// Struct tags: `json:"…"` names a field, `jsonschema:"…"` is
				// registry prose. Both are machine/prose tokens, not refusals.
				if v.Tag != nil {
					skip[v.Tag] = true
				}
			}
			return true
		})

		ast.Inspect(af, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING || skip[lit] {
				return true
			}
			val, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			checked++
			if hits := malayWordsIn(val); len(hits) > 0 {
				offenders = append(offenders, fmt.Sprintf("%s:%d: %q (Malay: %s)",
					fset.Position(lit.Pos()).Filename,
					fset.Position(lit.Pos()).Line,
					val,
					strings.Join(hits, ", ")))
			}
			return true
		})
	}

	// A floor, so a refactor that empties the scan fails instead of passing.
	if parsed < 3 {
		t.Fatalf("only %d non-test files parsed — the guard is looking at nothing", parsed)
	}
	if checked < 150 {
		t.Fatalf("only %d string literals checked — the guard is looking at nothing", checked)
	}
	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Errorf("%d Malay string literal(s) in a lane whose prompt, tool results and web copy are English:\n\t%s",
			len(offenders), strings.Join(offenders, "\n\t"))
	}
}
