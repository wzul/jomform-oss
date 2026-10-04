// The chat's write-proposal lane (phase 2 of the in-app assistant).
//
// THE PROBLEM THIS FILE SOLVES.
//
// Phase 1 made "the assistant cannot write" a property of the server: the model
// was offered read tools plus one in-process filing tool, and every write tool
// was refused before dispatch. Phase 2 lets a merchant ASK the assistant to
// change something ("rename that product to X", "make the form blue") without
// handing the model a write.
//
// The mechanism is a two-step the model cannot complete on its own:
//
//  1. PROPOSE  the model calls propose_write {tool, arguments, reason}. Nothing
//     is executed. The server stores (tool, canonical arguments, caller,
//     expiry) and returns an OPAQUE TOKEN plus a card describing the effect in
//     the user's language.
//  2. CONFIRM  the USER clicks in the UI. The server claims the token (single
//     use, atomic), re-checks the arguments against the hash the card carried,
//     and only then dispatches the tool — under the HUMAN's identity.
//
// WHY `confirm=true` IN THE MODEL'S ARGUMENTS IS NOT A CONFIRMATION.
//
// MCP's own confirmation shape is a boolean argument the caller sets
// (api_key_mcp.go: `revokeAPIKeyIn.Confirm`, gated at the KeyFromContext check).
// That works because there the caller IS the human: a person typed confirm=true
// into their own client. In the chat the caller is the model, and a boolean the
// model wrote proves nothing — the model can write it itself. If the proposal
// comes from the model and the confirmation comes from the model, no
// confirmation happened.
//
// So the confirmation here is NOT an argument. It is an opaque token that exists
// only in the user's browser, and completing the step requires BEING the person
// the token was issued to. A model that calls a write tool directly — with or
// without `confirm=true` — is refused in Runner.dispatch, never executed.
//
// THE FOUR PROPERTIES, EACH WITH THE MECHANISM THAT HOLDS IT:
//
//	binding    the STORED arguments are what execute, and the card carries
//	           args_hash which the client must echo; a changed argument set
//	           hashes differently and is refused before the token is spent.
//	single use the claim is one UPDATE ... WHERE consumed_at IS NULL RETURNING,
//	           so two confirms cannot both win.
//	expiry     ProposalTTL (five minutes). Short on purpose: a confirmation is
//	           decided while the user is reading the card in the conversation
//	           they just had, and every extra minute widens the gap between what
//	           they read and what runs.
//	caller     every read in the store is scoped by user_id, so another person's
//	           token answers exactly like a token that was never issued (no
//	           existence oracle), and the claim re-checks it atomically.
//
// WHAT IS NOT CLAIMED. The card is rendered from (tool, arguments) by the SERVER
// (Describe below), so it cannot describe an effect the arguments do not have.
// That is not the same as guaranteeing the merchant will understand it: the
// rendering is a short factual list, not a preview of every consequence, and the
// proposable set is kept small and reversible precisely because the card is
// thin. See mcpsrv.chatProposableTools for the classes excluded and why.
package chat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/mcpsrv"
)

const (
	// ProposalTTL is how long a proposal stays confirmable. Five minutes is a
	// conversation-length window: the user is looking at the card it belongs to,
	// and the assistant is not a queue of authorisations to be worked through
	// later. It is also short enough that a token captured from a shared screen is
	// worthless by the time it could be replayed.
	ProposalTTL = 5 * time.Minute
	// MaxProposalsPerRun caps how many proposals ONE message may produce. The
	// model has MaxSteps tool round-trips, so this is a tighter explicit bound on
	// how many decision cards one answer can put in front of the user — a wall of
	// confirmations is the failure mode that produces a careless click.
	MaxProposalsPerRun = 3
	// MaxProposalArgsChars bounds the serialised arguments a proposal may carry,
	// so a proposal cannot be used as storage (the model cannot park a document in
	// a row by proposing a write with a huge argument).
	MaxProposalArgsChars = 8000
	// ProposalTokenBytes is the entropy behind one token. 32 bytes (256 bits) of
	// crypto/rand, hex-encoded: unguessable regardless of how the token travels,
	// because the security does not rest on the token being secret in transit —
	// it rests on the lookup being scoped to the caller.
	ProposalTokenBytes = 32
)

// Errors the proposal lane returns. They are typed so the REST edge can answer a
// different status for "your card is stale" (409) than for "you sent rubbish"
// (400), and so a test can assert WHICH refusal fired rather than matching prose.
//
// ErrProposalArgsChanged is exported because the REST layer maps it; the rest are
// internal to the lane and are surfaced through ProposeRefusalKind.
var (
	errProposalsNotConfigured = errors.New("pengesahan tindakan tidak dikonfigurasi pada pelayan ini")
	errNoUserIdentity         = errors.New("pengguna tidak dikenali")
	errNoAccountIdentity      = errors.New("akaun tidak dikenali")
	errProposalTokenRequired  = errors.New("token cadangan diperlukan")
	ErrProposalArgsChanged    = errors.New("argumen cadangan berubah — sahkan semula cadangan asal")
	errProposalRunLimit       = errors.New("terlalu banyak cadangan dalam satu jawapan")
	errProposalUnrenderable   = errors.New("cadangan tanpa penerangan kad tidak dibenarkan")
	// ErrProposalArgsInvalid is the CONFIRM-side refusal for a stored proposal
	// whose arguments the target tool would never accept. Such a card can never
	// be carried out, so it is refused BEFORE the token is claimed: the token is
	// not spent, and the user is told to have the assistant prepare a fresh card
	// rather than to "try again" on one that cannot work. Exported because the
	// REST layer maps it.
	ErrProposalArgsInvalid = errors.New("cadangan itu tidak boleh dilaksanakan kerana hujahnya " +
		"tidak diterima oleh alat itu — minta pembantu menyediakan kad baharu")
	// errProposalArgsRejected is the PROPOSE-side form of the same verdict. It is
	// returned with the SPECIFIC detail attached (which required key is absent,
	// which key is not recognised) because the model is the one who can correct
	// the call, and it is classified separately in the audit log.
	errProposalArgsRejected = errors.New("hujah cadangan tidak diterima oleh alat itu")
	// errNoToolSchemaSource means the tool's own input schema could not be read:
	// the caller is not a live MCP session, so there is no contract to check the
	// arguments against. Refusing is deliberate — recording a proposal whose
	// arguments nothing verified is the defect this check exists to close.
	errNoToolSchemaSource = errors.New("sesi alat tiada — kontrak alat tidak dapat dibaca")
)

// ProposeRefusalKind classifies a proposal-lane error as a short STABLE token for
// the audit log — a machine-readable "why", never the message text (which can name
// an internal detail). An unrecognised error is "internal": the log says a refusal
// happened without guessing at its cause.
func ProposeRefusalKind(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrProposalArgsChanged):
		return "args_changed"
	case errors.Is(err, dbstore.ErrChatProposalConsumed):
		return "consumed"
	case errors.Is(err, dbstore.ErrChatProposalExpired):
		return "expired"
	case errors.Is(err, dbstore.ErrChatProposalNotFound):
		return "not_found"
	case errors.Is(err, errProposalsNotConfigured):
		return "not_configured"
	case errors.Is(err, errProposalTokenRequired):
		return "token_required"
	case errors.Is(err, errProposalUnrenderable):
		return "unrenderable"
	case errors.Is(err, ErrProposalArgsInvalid):
		return "args_unacceptable"
	case errors.Is(err, errProposalArgsRejected):
		return "args_rejected"
	case errors.Is(err, errNoToolSchemaSource):
		return "no_tool_contract"
	case errors.Is(err, errProposalRunLimit):
		return "run_limit"
	}
	return "internal"
}

// ProposalTokenFingerprint returns a short, non-reversible fingerprint of a raw
// token, for the audit log.
//
// WHY NOT THE TOKEN, AND WHY NOT NOTHING. Logging the raw token would put a live
// authorisation into a table operators read — the one place it must never be. But
// an audit row with no handle at all cannot be correlated with the proposal it
// refers to, which is what makes a refusal investigation possible. The first 12 hex
// characters of the sha256 give both: a stable identifier that reveals nothing.
func ProposalTokenFingerprint(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	h := dbstore.HashProposalToken(raw)
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

// ProposalField is one specific argument, rendered for the card: the label in the
// user's terms and the value they are agreeing to. The card must show WHAT changes
// (which form, what price), not just that something will.
type ProposalField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// ProposalCard is what the UI shows the user, and the only thing that can be
// confirmed. It carries the token; the MODEL never sees the token — the card goes
// to the browser and the model is told only that a proposal is pending.
type ProposalCard struct {
	// Token is the opaque, single-use, server-issued confirmation token.
	Token string `json:"token"`
	// ArgsHash is sha256(canonical arguments). The confirm must echo it, which is
	// how "the thing confirmed is the thing shown" is enforced.
	ArgsHash string `json:"args_hash"`
	// Tool is the real MCP tool name, shown as written down rather than prettified:
	// a merchant who wants to check what the assistant did can look it up.
	Tool string `json:"tool"`
	// Title names the effect in human language ("Kemas kini produk").
	Title string `json:"title"`
	// Summary states what will happen, in one sentence.
	Summary string `json:"summary"`
	// Fields are the specific arguments the user is agreeing to.
	Fields []ProposalField `json:"fields"`
	// Note is the model's own explanation, displayed as the assistant's wording.
	// It is untrusted text: shown, never parsed, never the authority for what the
	// card says will happen.
	Note string `json:"note,omitempty"`
	// ExpiresAt is when the token stops working (RFC3339, UTC).
	ExpiresAt string `json:"expires_at"`
	// Reversible is true for every tool this phase allows. It is on the card
	// because "you can put this back" is the fact that makes confirming a small
	// decision — a card that ever said false would need a different UI.
	Reversible bool `json:"reversible"`
}

// withoutSecrets is the card as the MODEL may see it: everything except the two
// values that authorise the confirmation.
//
// The token is the user's authorisation and the args_hash is its binding, and
// neither has any business in a prompt. A model that read the token could complete
// the confirmation itself — the one thing this whole design exists to prevent — and
// an args_hash in the prompt would make the model think it participates in the
// binding check. So the model is told what the card says and nothing about how to
// settle it.
func (c ProposalCard) withoutSecrets() ProposalCard {
	c.Token = ""
	c.ArgsHash = ""
	return c
}

// Labeler resolves an id to a human name for the card ("which form", "which
// product"). It returns "" when it cannot resolve one; the card then shows the id
// rather than inventing a name. Built over the chat's own scoped read tools, so
// the card names objects through the same reads the answer used.
type Labeler func(kind, id string) string

// proposalDoc describes one proposable tool for the model: the title/summary the
// card will show, and a reminder of the arguments. The argument schema itself is
// NOT copied here — it comes from the registered tool over tools/list, so the
// model builds arguments against the same contract the gate validates.
type proposalDoc struct {
	title   string
	summary string
}

// proposalDocs is the ONE place a tool is declared proposable-with-a-card. A
// proposable name with no entry here has no card and is refused (Describe), so
// the proposable set in mcpsrv and the renderable set here cannot drift: the
// guard test asserts every mcpsrv.ChatProposableTools() name exists here.
var proposalDocs = map[string]proposalDoc{
	"update_product": {
		title:   "Kemas kini produk",
		summary: "Medan produk yang disenaraikan di bawah akan ditukar. Baki medan kekal seperti sedia ada.",
	},
	"update_product_variation": {
		title:   "Kemas kini variasi produk",
		summary: "Variasi yang dinamakan di bawah akan ditukar. Variasi lain dan medan yang tidak disenaraikan kekal.",
	},
	"set_form_theme": {
		title:   "Kemas kini gaya borang",
		summary: "Gaya dan branding borang akan ditukar. Kandungan borang, harga dan tetapan lain tidak berubah.",
	},
	// t_804040be (Wan's C): the assistant may CREATE a form. The summary says which
	// half is happening, because the same tool both creates and updates and a user
	// confirming "kemas kini" when a NEW form is about to appear has been misled.
	"upsert_form": {
		title:   "Cipta atau kemas kini borang",
		summary: "Borang baharu akan DICIPTA (atau yang sedia ada dikemas kini, jika id diberi). Tiada apa-apa dihapuskan, dan borang yang tersilap boleh dihantar ke tong sampah dan dipulihkan.",
	},
}

// ProposalTitle returns the card's title and summary for a tool.
func ProposalTitle(tool string) (title, summary string, ok bool) {
	d, ok := proposalDocs[tool]
	return d.title, d.summary, ok
}

// cardRenderer renders one tool's card fields, plus the arguments that MUST name
// the object the card is about.
//
// WHY THE KEYS ARE DECLARED BESIDE THE RENDERER (card t_623676d1). The card's
// summary asserts that the object is listed ("Medan produk yang disenaraikan di
// bawah akan ditukar"), and the object row was rendered from ONE hardcoded key
// with a plain `if id != ""`. So a proposal whose arguments keyed — or blanked —
// the object as anything else rendered a card that said a product was listed with
// no product on it, and whose Confirm could then only fail. Declaring the key as
// part of the SAME map entry as the renderer means the two cannot drift: Describe
// refuses a tool whose declared object key is absent or blank, so "a card is never
// wrong about WHICH object" (the property its own doc comment claims) is enforced
// rather than asserted.
type cardRenderer struct {
	// objectKeys are the argument keys whose values identify the object(s) the
	// card acts on. Each must be present and non-blank for the card to be
	// renderable. Empty for a tool that acts on NO EXISTING OBJECT — a creation,
	// where "which object" is not a question the user can get wrong.
	objectKeys []string
	fn         func(map[string]any, Labeler) []ProposalField
}

// describeFns renders one tool's card fields. Each proposable tool has one, and
// Describe refuses a tool with none — so adding a tool to the proposable set
// without teaching the card about it fails the guard rather than shipping an
// unexplained confirmation.
var describeFns = map[string]cardRenderer{
	"update_product":           {objectKeys: []string{"product_id"}, fn: describeProductUpdate},
	"update_product_variation": {objectKeys: []string{"product_id", "variation_id"}, fn: describeVariationUpdate},
	"set_form_theme":           {objectKeys: []string{"form_id"}, fn: describeFormTheme},
	// upsert_form creates OR updates: with form_id blank it is a CREATION (no
	// object to name, and the card says so explicitly), so it declares none.
	"upsert_form": {fn: describeFormCreation},
}

// describeFormCreation renders the card for t_804040be's creation.
//
// It shows what the form will BE — the title, the address — and says plainly when
// the id is empty that this CREATES rather than renames something. A card that
// listed only a title would leave the user unable to tell a creation from a rename
// of an existing form, which is the difference the confirmation is FOR.
func describeFormCreation(args map[string]any, label Labeler) []ProposalField {
	var fields []ProposalField
	if id := asString(args["form_id"]); id != "" {
		fields = append(fields, ProposalField{Label: "Borang", Value: named(label, "form", id)})
	} else {
		fields = append(fields, ProposalField{Label: "Tindakan", Value: "Cipta borang baharu"})
	}
	if title := asString(args["title"]); title != "" {
		fields = append(fields, ProposalField{Label: "Tajuk", Value: renderValue(title)})
	}
	if slug := asString(args["slug"]); slug != "" {
		fields = append(fields, ProposalField{Label: "Slug (URL)", Value: renderValue(slug)})
	}
	// The rest of the arguments are stated by COUNT rather than by value: config and
	// theme are nested maps whose contents are the form's whole body, and dumping
	// them would bury the two fields the user actually judges. The count is what
	// tells them the card is not hiding an empty submission.
	for _, key := range []string{"config", "theme", "notifications", "chip_feeds"} {
		v, ok := args[key]
		if !ok || v == nil {
			continue
		}
		if m, isMap := v.(map[string]any); isMap {
			fields = append(fields, ProposalField{
				Label: key, Value: fmt.Sprintf("%d medan", len(m))})
			continue
		}
		if list, isList := v.([]any); isList {
			fields = append(fields, ProposalField{
				Label: key, Value: fmt.Sprintf("%d item", len(list))})
			continue
		}
		fields = append(fields, ProposalField{Label: key, Value: renderValue(v)})
	}
	return fields
}

// Describe renders a proposal card's human wording FROM THE TOOL AND ARGUMENTS
// ONLY. The model's prose is not an input: it cannot make the card promise
// something the arguments do not do.
//
// ok=false means this tool has no card, or its arguments do not name the object
// the card would claim: either it is not proposable at all, it was added to the
// proposable set without a renderer, or its declared object key is absent/blank.
// All of them must refuse — a card the server cannot explain, or one that asserts
// a listed object it cannot name, is a card the user cannot judge (t_623676d1).
func Describe(tool string, args map[string]any, label Labeler) (title, summary string, fields []ProposalField, ok bool) {
	title, summary, ok = ProposalTitle(tool)
	if !ok {
		return "", "", nil, false
	}
	renderer, ok := describeFns[tool]
	if !ok {
		return "", "", nil, false
	}
	// The object must be NAMEABLE before a card that claims to name it is built.
	for _, key := range renderer.objectKeys {
		if strings.TrimSpace(asString(args[key])) == "" {
			return "", "", nil, false
		}
	}
	if label == nil {
		label = func(string, string) string { return "" }
	}
	return title, summary, renderer.fn(args, label), true
}

// named renders "Name (id)" when a name resolved, else just the id — so a card is
// never wrong about WHICH object, even when the name lookup failed.
func named(label Labeler, kind, id string) string {
	if id == "" {
		return ""
	}
	if n := strings.TrimSpace(label(kind, id)); n != "" {
		return n + " (" + id + ")"
	}
	return id
}

// money renders sen as RM. Money is integer sen everywhere in this repo; a card
// that showed raw sen would be the one place a user reads a number wrong.
func money(sen int64) string {
	neg := sen < 0
	if neg {
		sen = -sen
	}
	s := fmt.Sprintf("RM%d.%02d", sen/100, sen%100)
	if neg {
		return "-" + s
	}
	return s
}

// asInt64 reads a JSON number. JSON numbers arrive as float64; the cast is exact
// for the integer ranges these fields use (2^53 sen is 90 trillion ringgit).
func asInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	}
	return 0, false
}

func asBool(v any) (bool, bool) {
	b, ok := v.(bool)
	return b, ok
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func asStringList(v any) ([]string, bool) {
	arr, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		out = append(out, renderValue(e))
	}
	return out, true
}

// describeProductUpdate renders update_product's arguments. The fields mirror
// updateProductIn (api/internal/mcpsrv/parity.go): only the keys the caller
// actually sent are shown, because an omitted key means "leave unchanged" — and
// the card must not imply otherwise.
func describeProductUpdate(args map[string]any, label Labeler) []ProposalField {
	var fields []ProposalField
	if id := asString(args["product_id"]); id != "" {
		fields = append(fields, ProposalField{Label: "Produk", Value: named(label, "product", id)})
	}
	if v, ok := args["name"]; ok {
		fields = append(fields, ProposalField{Label: "Nama", Value: renderValue(v)})
	}
	if v, ok := args["description"]; ok {
		fields = append(fields, ProposalField{Label: "Penerangan", Value: renderValue(v)})
	}
	if sen, ok := asInt64(args["price_sen"]); ok {
		fields = append(fields, ProposalField{Label: "Harga", Value: money(sen)})
	}
	if n, ok := asInt64(args["stock"]); ok {
		fields = append(fields, ProposalField{Label: "Stok", Value: fmt.Sprintf("%d", n)})
	}
	if b, ok := asBool(args["active"]); ok {
		fields = append(fields, ProposalField{Label: "Aktif", Value: yesNo(b)})
	}
	if v, ok := args["type"]; ok {
		fields = append(fields, ProposalField{Label: "Jenis", Value: renderValue(v)})
	}
	if v, ok := args["billing_period"]; ok {
		fields = append(fields, ProposalField{Label: "Kitaran bil", Value: renderValue(v)})
	}
	if n, ok := asInt64(args["trial_days"]); ok {
		fields = append(fields, ProposalField{Label: "Hari percubaan", Value: fmt.Sprintf("%d", n)})
	}
	if v, ok := args["schedule_mode"]; ok {
		fields = append(fields, ProposalField{Label: "Polisi tarikh caj", Value: renderValue(v)})
	}
	if imgs, ok := asStringList(args["images"]); ok {
		fields = append(fields, ProposalField{Label: "Gambar", Value: describeList(imgs)})
	}
	if vars, ok := args["variations"]; ok {
		n := 0
		if arr, ok := vars.([]any); ok {
			n = len(arr)
		}
		fields = append(fields, ProposalField{
			Label: "Variasi",
			Value: fmt.Sprintf("%d baris — senarai penuh, menggantikan senarai sedia ada", n),
		})
	}
	if v, ok := args["subscription_charges"]; ok {
		if v == nil {
			fields = append(fields, ProposalField{Label: "Bilangan caj", Value: "tiada had (infinite)"})
		} else if n, ok := asInt64(v); ok {
			fields = append(fields, ProposalField{Label: "Bilangan caj", Value: fmt.Sprintf("%d kali", n)})
		}
	}
	return fields
}

// describeVariationUpdate renders update_product_variation's arguments
// (updateVariationIn in parity.go), including the clear_* flags: those are the
// fields that reset a cycle back to "inherit the product", and a card that hid
// them would omit the most consequential part of that call.
func describeVariationUpdate(args map[string]any, label Labeler) []ProposalField {
	var fields []ProposalField
	if id := asString(args["product_id"]); id != "" {
		fields = append(fields, ProposalField{Label: "Produk", Value: named(label, "product", id)})
	}
	if id := asString(args["variation_id"]); id != "" {
		fields = append(fields, ProposalField{Label: "Variasi", Value: id})
	}
	if v, ok := args["name"]; ok {
		fields = append(fields, ProposalField{Label: "Nama", Value: renderValue(v)})
	}
	if sen, ok := asInt64(args["price_sen"]); ok {
		fields = append(fields, ProposalField{Label: "Harga", Value: money(sen)})
	}
	if n, ok := asInt64(args["stock"]); ok {
		fields = append(fields, ProposalField{Label: "Stok", Value: fmt.Sprintf("%d", n)})
	}
	if v, ok := args["billing_period"]; ok {
		fields = append(fields, ProposalField{Label: "Kitaran bil", Value: renderValue(v)})
	}
	if n, ok := asInt64(args["trial_days"]); ok {
		fields = append(fields, ProposalField{Label: "Hari percubaan", Value: fmt.Sprintf("%d", n)})
	}
	if n, ok := asInt64(args["subscription_charges"]); ok {
		fields = append(fields, ProposalField{Label: "Bilangan caj", Value: fmt.Sprintf("%d kali", n)})
	}
	if imgs, ok := asStringList(args["images"]); ok {
		fields = append(fields, ProposalField{Label: "Gambar", Value: describeList(imgs)})
	}
	for _, c := range []struct{ key, label string }{
		{"clear_billing_period", "Kosongkan kitaran variasi (warisi produk)"},
		{"clear_trial_days", "Kosongkan hari percubaan (warisi produk)"},
		{"clear_subscription_charges", "Kosongkan bilangan caj (warisi produk)"},
	} {
		if b, ok := asBool(args[c.key]); ok && b {
			fields = append(fields, ProposalField{Label: c.label, Value: "ya"})
		}
	}
	return fields
}

// themeFieldLabels names the theme keys set_form_theme accepts, in the words the
// dashboard's theme editor uses. The KEY LIST is exactly setFormThemeIn
// (api/internal/mcpsrv/form_creation.go) — measured from the struct, not invented
// — and describeFormTheme falls back to the raw key for anything else, so a theme
// key that is added later is still SHOWN rather than silently dropped from the
// card.
var themeFieldLabels = map[string]string{
	"primary":              "Warna utama",
	"background":           "Warna latar",
	"text":                 "Warna teks",
	"card_background":      "Latar kad",
	"card_border_radius":   "Jejari sudut kad",
	"font_family":          "Fon",
	"logo_url":             "URL logo",
	"layout":               "Susun atur",
	"spacing":              "Jarak",
	"product_image_size":   "Saiz imej produk",
	"product_image_layout": "Susunan imej produk",
	"custom_css":           "CSS tersuai",
	"custom_js":            "JS tersuai",
}

// describeFormTheme renders set_form_theme's arguments. Every key present in the
// arguments is listed — an unknown key under its own name rather than dropped,
// because a card that hides a field lets a user confirm something they were not
// shown.
func describeFormTheme(args map[string]any, label Labeler) []ProposalField {
	var fields []ProposalField
	if id := asString(args["form_id"]); id != "" {
		fields = append(fields, ProposalField{Label: "Borang", Value: named(label, "form", id)})
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		if k == "form_id" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		lbl := themeFieldLabels[k]
		if lbl == "" {
			lbl = k
		}
		fields = append(fields, ProposalField{Label: lbl, Value: renderValue(args[k])})
	}
	return fields
}

// renderValue turns one argument value into display text. Long strings are
// truncated for the CARD only — the stored arguments are untouched, which is why
// the truncation is safe: the user approves the whole value, and the hash covers
// all of it.
func renderValue(v any) string {
	switch t := v.(type) {
	case nil:
		return "(kosong)"
	case string:
		if strings.TrimSpace(t) == "" {
			return "(kosong)"
		}
		return truncateText(t, 200)
	case bool:
		return yesNo(t)
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%g", t)
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			out = append(out, renderValue(e))
		}
		return describeList(out)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "(tidak dapat dipaparkan)"
	}
	return truncateText(string(b), 200)
}

func describeList(items []string) string {
	if len(items) == 0 {
		return "(kosong)"
	}
	if len(items) == 1 {
		return "1 item: " + truncateText(items[0], 120)
	}
	return fmt.Sprintf("%d item: %s …", len(items), truncateText(items[0], 80))
}

func yesNo(b bool) string {
	if b {
		return "ya"
	}
	return "tidak"
}

func truncateText(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max]) + "…"
}

// ── the propose/confirm wiring the runner uses ───────────────────────────────

// proposalStore is the slice of the proposal store the runner needs. An interface
// so the runner's tests can drive propose/confirm without a database; production
// always uses dbstore.ChatWriteProposalStore.
type proposalStore interface {
	Create(ctx context.Context, p *dbstore.ChatWriteProposal) error
	ByToken(ctx context.Context, userID, tokenHash string) (*dbstore.ChatWriteProposal, error)
	Claim(ctx context.Context, userID, tokenHash string) (*dbstore.ChatWriteProposal, error)
}

// toolSchemaSource is the optional capability a tool channel has when it can
// report a registered tool's DECLARED input schema (see mcpCaller.toolInputSchema,
// which reads it over tools/list from the live registry).
//
// WHY IT IS A CAPABILITY AND NOT PART OF toolCaller (card t_623676d1). The
// proposal lane must check the model's arguments against the target tool's REAL
// contract, and the only honest source of that contract is the registry the tool
// itself validates with. A caller that cannot produce one (a test fake) has no
// contract to check against, and the lane refuses rather than recording a
// proposal whose arguments nothing verified — which is precisely the defect.
type toolSchemaSource interface {
	toolInputSchema(ctx context.Context, name string) (schema any, found bool, err error)
}

// validateProposalArgs checks a proposal's arguments against the TARGET TOOL's
// own input schema.
//
// THE DEFECT THIS CLOSES (card t_623676d1). The card's object row was rendered
// from ONE hardcoded key (`args["product_id"]`), and nothing checked the
// arguments against the tool at all — so a model that keyed the object as `id`
// (which is what the chat's own READ tools return: mcpsrv.handlers.go's
// productOut is `json:"id"`) produced a card whose summary said "the product
// fields listed below will be changed" with no product row, and whose Confirm
// then failed with a bare 400 after spending the token. The model was being asked
// to rename a key between two tool schemas and nothing told it, and the user was
// shown a decision that could never be carried out.
//
// WHY THE SCHEMA AND NOT A LIST OF KNOWN KEY NAMES. The target tool's MCP input
// schema IS its accept/reject predicate — the SDK validates every dispatch
// against it before the handler runs — so checking the same schema makes the
// proposal lane agree with the tool BY CONSTRUCTION: it can be neither stricter
// (it would refuse something the tool accepts) nor looser (it would card
// something the tool refuses). A hand-written alias table would be a second
// declaration of the same rule, and the whole class of defect here is two
// declarations drifting apart.
func (r *Runner) validateProposalArgs(ctx context.Context, caller toolCaller, tool string, args map[string]any) error {
	src, ok := caller.(toolSchemaSource)
	if !ok {
		return errNoToolSchemaSource
	}
	raw, found, err := src.toolInputSchema(ctx, tool)
	if err != nil {
		return fmt.Errorf("baca skema alat %s: %w", tool, err)
	}
	if !found {
		return fmt.Errorf("alat %q tiada dalam pendaftaran — kontraknya tidak dapat disahkan", tool)
	}
	// The TOOL NAME is put in front of the schema's own message so the refusal is
	// self-contained for the model: propose_write's arguments carry the tool, but a
	// refusal the model can act on should name the contract it violated rather than
	// require it to remember which call it just made.
	if verr := validateAgainstToolSchema(tool, raw, args); verr != nil {
		return fmt.Errorf("alat %s: %w", tool, verr)
	}
	return nil
}

// validateAgainstToolSchema is the schema check itself, split out so both lanes
// (propose and confirm) share ONE rule and a test can drive it without a session.
//
// The arguments are validated on a COPY: Validate may apply the schema's own
// defaults in place, and the stored arguments must remain exactly the ones the
// card described (the card is rendered from them, and the args_hash binds the
// confirmation to them). Our schemas declare no defaults, so in practice the copy
// is belt and braces — but a future default must not silently rewrite what the
// user was shown.
func validateAgainstToolSchema(tool string, rawSchema any, args map[string]any) error {
	b, err := json.Marshal(rawSchema)
	if err != nil {
		return fmt.Errorf("%w: skema alat %s tidak boleh dibaca (%v)", errProposalArgsRejected, tool, err)
	}
	var sch jsonschema.Schema
	if err := json.Unmarshal(b, &sch); err != nil {
		return fmt.Errorf("%w: skema alat %s tidak boleh dibaca (%v)", errProposalArgsRejected, tool, err)
	}
	resolved, err := sch.Resolve(&jsonschema.ResolveOptions{})
	if err != nil || resolved == nil {
		return fmt.Errorf("%w: skema alat %s tidak boleh diselesaikan (%v)", errProposalArgsRejected, tool, err)
	}
	// MISSING REQUIRED KEYS FIRST, and reported as their own sentence.
	//
	// WHY THIS IS SEPARATE FROM THE VALIDATION BELOW (t_623676d1). A validator
	// reports the FIRST rule it trips, and for the shape this card is about the two
	// rules overlap: a model that keys the object `id` trips BOTH
	// `additionalProperties:false` (the undeclared `id`) and `required` (no
	// `product_id`). Only the second tells the model WHAT TO SEND — "unexpected
	// additional properties [\"id\"]" says what was wrong without saying what the
	// tool wants, so the model can only guess or repeat itself. Naming the missing
	// required keys explicitly is the correction cue that turns a dead card into one
	// corrected turn, and it comes from the SAME schema, so it cannot disagree with
	// the tool.
	var missing []string
	for _, req := range sch.Required {
		if _, present := args[req]; !present {
			missing = append(missing, req)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("%w: medan wajib tiada: %s", errProposalArgsRejected, strings.Join(missing, ", "))
	}
	cp := make(map[string]any, len(args))
	for k, v := range args {
		cp[k] = v
	}
	if err := resolved.Validate(cp); err != nil {
		// The schema's own message is the model's correction cue for the rules the
		// required check above cannot cover (an undeclared key, a wrong type), so it
		// is carried verbatim.
		return fmt.Errorf("%w: %v", errProposalArgsRejected, err)
	}
	return nil
}

// newProposalToken mints the raw opaque token. crypto/rand, never math/rand: the
// token is the one value that distinguishes "the user confirmed" from "somebody
// guessed".
func newProposalToken() (string, error) {
	b := make([]byte, ProposalTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ProposeArgs is the propose_write tool's input, as the model supplies it.
type ProposeArgs struct {
	// Tool is the write tool the model wants to perform.
	Tool string `json:"tool"`
	// Arguments is the tool's own input, passed through unchanged. It is
	// UNTRUSTED: it is stored and later validated by the tool's own gate, never
	// assumed to match the declared schema.
	Arguments map[string]any `json:"arguments"`
	// Reason is the model's explanation, shown to the user as the assistant's
	// wording. Never parsed.
	Reason string `json:"reason,omitempty"`
}

// proposeFromArgs parses one propose_write call and records the proposal. It
// executes nothing, and it is the ONLY way a proposal enters the system — the
// token is always minted here.
//
// caller is the live tool channel of the CURRENT exchange; the card uses it to
// name the objects it touches through the chat's own scoped reads, so no second
// session is opened and the labels are read under exactly the identity the answer
// ran under.
func (r *Runner) proposeFromArgs(ctx context.Context, caller toolCaller, argsJSON string) (*ProposalCard, error) {
	var in ProposeArgs
	if err := json.Unmarshal([]byte(argsJSON), &in); err != nil {
		return nil, fmt.Errorf("argumen tidak sah: alat dan argumen diperlukan")
	}
	in.Tool = strings.TrimSpace(in.Tool)
	if in.Tool == "" {
		return nil, errors.New("medan 'tool' diperlukan untuk mencadangkan tindakan")
	}
	if !mcpsrv.ChatToolIsProposable(in.Tool) {
		// The refusal names the reason, so the model can tell the user why it
		// will not even OFFER this one instead of silently doing nothing.
		return nil, fmt.Errorf("alat %q tidak boleh dicadangkan dalam chat ini — "+
			"tindakan pemusnah, wang, kelayakan akses dan penghantaran emel dikecualikan; "+
			"cuba salah satu daripada: %s", in.Tool, strings.Join(mcpsrv.ChatProposableTools(), ", "))
	}
	if in.Arguments == nil {
		in.Arguments = map[string]any{}
	}
	// THE ARGUMENTS MUST BE ONE THE TOOL WILL ACTUALLY ACCEPT, and that is checked
	// HERE — at propose time, against the tool's own registered schema — because a
	// card that cannot be carried out is worse than a refusal: it asks the user to
	// make a decision, spends their attention, and (before this check) spent the
	// token too, on a Confirm that could only answer a bare 400.
	//
	// It runs BEFORE the renderability check below, and before the token is minted,
	// so:
	//   - a rejected proposal leaves no row and no token behind, and
	//   - where the two refuse the same argument set (a mis-keyed object is both
	//     "unrenderable" and "not accepted"), the model receives the ACTIONABLE
	//     refusal — the schema names the key it wants — rather than the opaque one.
	//
	// The refusal is deliberately MODEL-facing: it names the tool and the schema's
	// own objection, so the model can correct a mis-keyed object and propose again
	// (the read tools return rows keyed `id`, while the write tools want
	// `product_id` — the exact rename the shipped data model invites, t_623676d1).
	// That turns a dead card the user cannot act on into one corrected turn.
	if err := r.validateProposalArgs(ctx, caller, in.Tool, in.Arguments); err != nil {
		return nil, err
	}
	if _, _, _, ok := Describe(in.Tool, in.Arguments, nil); !ok {
		return nil, errProposalUnrenderable
	}
	canon, err := dbstore.CanonicalArgs(in.Arguments)
	if err != nil {
		return nil, fmt.Errorf("argumen tidak sah: %w", err)
	}
	if len(canon) > MaxProposalArgsChars {
		return nil, fmt.Errorf("argumen cadangan terlalu besar (maksimum %d aksara)", MaxProposalArgsChars)
	}
	if r.proposals == nil {
		return nil, errProposalsNotConfigured
	}
	uid := userIDFrom(ctx)
	if uid == "" {
		return nil, errNoUserIdentity
	}
	acct := accountIDFrom(ctx)
	if acct == "" {
		return nil, errNoAccountIdentity
	}

	// The card is rendered from (tool, args) — NEVER from the model's prose. The
	// prose is stored as Note and displayed as the assistant's own wording.
	label := r.labelResolver(ctx, caller)
	title, summary, fields, _ := Describe(in.Tool, in.Arguments, label)

	raw, err := newProposalToken()
	if err != nil {
		return nil, err
	}
	p := &dbstore.ChatWriteProposal{
		TokenHash: dbstore.HashProposalToken(raw),
		UserID:    uid,
		AccountID: acct,
		Tool:      in.Tool,
		Args:      in.Arguments,
		Title:     title,
		Summary:   summary,
		Note:      truncateText(in.Reason, 500),
		ExpiresAt: time.Now().UTC().Add(ProposalTTL),
	}
	if err := r.proposals.Create(ctx, p); err != nil {
		return nil, fmt.Errorf("simpan cadangan: %w", err)
	}
	return &ProposalCard{
		Token:      raw,
		ArgsHash:   p.ArgsHash,
		Tool:       in.Tool,
		Title:      title,
		Summary:    summary,
		Fields:     fields,
		Note:       p.Note,
		ExpiresAt:  p.ExpiresAt.Format(time.RFC3339),
		Reversible: true,
	}, nil
}

// Propose records a proposal from a caller OUTSIDE the tool loop (the REST layer
// and tests), opening its own tool channel. The loop uses proposeFromArgs, which
// reuses the exchange's channel.
func (r *Runner) Propose(ctx context.Context, tool string, args map[string]any, note string) (*ProposalCard, error) {
	caller, err := r.newSession(ctx)
	if err != nil {
		return nil, err
	}
	defer caller.close()
	body, err := json.Marshal(ProposeArgs{Tool: tool, Arguments: args, Reason: note})
	if err != nil {
		return nil, err
	}
	return r.proposeFromArgs(ctx, caller, string(body))
}

// Confirmed is the outcome of executing a confirmed proposal.
type Confirmed struct {
	Tool   string
	Result string
	// ProposalID is the audit handle for the executed proposal.
	ProposalID string
}

// Confirm executes a proposal the USER confirmed, and returns what the tool said.
//
// ORDER MATTERS, and it is:
//
//  1. look the token up for THIS caller (no existence oracle),
//  2. refuse a spent or expired token,
//  3. refuse if the arguments the card carried are not the stored ones, or the
//     tool is no longer proposable,
//  4. CLAIM it — the atomic single-use step,
//  5. dispatch the tool with the STORED arguments.
//
// Steps 1–3 run before the claim so a tampered or stale request does not burn the
// user's token; step 4 is what makes a double confirm impossible, and it is still
// a single statement.
//
// writeCtx is the context the tool runs under. The caller builds it with the
// HUMAN's identity and the WRITE scope: the human's click is the authorisation,
// the tool's own workspace-role and ownership gates still apply unchanged, and the
// scope is granted for THIS bound proposal rather than opened for the conversation.
func (r *Runner) Confirm(ctx context.Context, writeCtx context.Context, userID, token, argsHash string) (*Confirmed, error) {
	if r.proposals == nil {
		return nil, errProposalsNotConfigured
	}
	if userID == "" {
		return nil, errNoUserIdentity
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errProposalTokenRequired
	}
	hash := dbstore.HashProposalToken(token)

	p, err := r.proposals.ByToken(ctx, userID, hash)
	if err != nil {
		return nil, err
	}
	if p.IsSpent() {
		return nil, dbstore.ErrChatProposalConsumed
	}
	if !p.ExpiresAt.After(time.Now().UTC()) {
		return nil, dbstore.ErrChatProposalExpired
	}
	if !p.ArgsHashMatches(argsHash) {
		// THE BINDING. A client that submits a different argument set, or no hash
		// at all, is refused HERE — before the token is spent — and the stored
		// arguments remain the only thing that could ever execute.
		return nil, ErrProposalArgsChanged
	}
	if !mcpsrv.ChatToolIsProposable(p.Tool) {
		// The proposable set is a product decision that can shrink; a row written
		// under an older set must not execute under a newer one.
		return nil, fmt.Errorf("alat %q tidak lagi boleh dilaksanakan melalui chat", p.Tool)
	}
	// THE STORED ARGUMENTS MUST BE ONE THE TOOL ACCEPTS, and this is checked
	// BEFORE the claim for the same reason the args-hash check is (t_623676d1): a
	// row that slipped into the store under an older, unchecked contract would
	// otherwise spend the user's token on a Confirm that can only fail. Refusing
	// here leaves the token unspent, so the recovery the user is offered —
	// "ask the assistant to prepare it again" — is TRUE, instead of the old
	// "please try again" hint for a card that could never succeed.
	//
	// It is checked BEFORE the renderability test below so that a card which is
	// both unrenderable AND unacceptable (the mis-keyed object is both) answers the
	// refusal that explains WHY — "the tool does not accept these arguments" — and
	// not the one that only says no card could be drawn.
	schemaCaller, err := r.newSession(ctx)
	if err != nil {
		return nil, err
	}
	verr := r.validateProposalArgs(ctx, schemaCaller, p.Tool, p.Args)
	_ = schemaCaller.close()
	if verr != nil {
		return nil, fmt.Errorf("%w", ErrProposalArgsInvalid)
	}
	if _, _, _, ok := Describe(p.Tool, p.Args, nil); !ok {
		return nil, errProposalUnrenderable
	}

	claimed, err := r.proposals.Claim(ctx, userID, hash)
	if err != nil {
		return nil, err
	}
	if claimed.ID != p.ID {
		// A different proposal answered the same token hash. Cannot happen with a
		// UNIQUE token_hash; refused loudly rather than executed.
		return nil, errors.New("token cadangan tidak sepadan dengan rekod")
	}

	// The STORED arguments, re-serialised as the tool's own JSON input. This —
	// never the request — is what runs.
	argsJSON, err := dbstore.CanonicalArgs(claimed.Args)
	if err != nil {
		return nil, fmt.Errorf("argumen cadangan tidak boleh dibaca: %w", err)
	}

	caller, err := r.newSession(writeCtx)
	if err != nil {
		return nil, err
	}
	defer caller.close()
	out, refused, err := caller.call(writeCtx, claimed.Tool, string(argsJSON))
	if err != nil {
		return nil, err
	}
	if refused {
		// The server's own gates refused it (workspace role, ownership, or a
		// validation the card could not have predicted). The token is spent — see
		// the file header on why a failed execution does not release it.
		return nil, fmt.Errorf("server menolak: %s", strings.TrimSpace(bound(out, 300)))
	}
	return &Confirmed{Tool: claimed.Tool, Result: bound(out, MaxToolResultChars), ProposalID: claimed.ID}, nil
}

// Cancel spends a proposal without executing it — the user's "no". It is the same
// atomic claim, so a cancel that races a confirm leaves exactly one winner and a
// cancelled token is as dead as a confirmed one.
func (r *Runner) Cancel(ctx context.Context, userID, token string) error {
	if r.proposals == nil {
		return errProposalsNotConfigured
	}
	if userID == "" {
		return errNoUserIdentity
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return errProposalTokenRequired
	}
	_, err := r.proposals.Claim(ctx, userID, dbstore.HashProposalToken(token))
	return err
}

// labelResolver builds the Labeler the card uses to name the objects it touches,
// through the chat's own scoped READ tools. Every lookup is best-effort: a failure
// yields "" and the card shows the id instead of a name, because a card that
// invented a name would be worse than one that shows a raw id.
//
// It uses the caller it is given (the exchange's own channel) rather than opening
// a second session, so the names are read under exactly the identity the answer
// ran under — and so a card can never name an object the caller could not read.
func (r *Runner) labelResolver(ctx context.Context, caller toolCaller) Labeler {
	if caller == nil {
		return nil
	}
	return func(kind, id string) string {
		if strings.TrimSpace(id) == "" {
			return ""
		}
		switch kind {
		case "form":
			body, _ := json.Marshal(map[string]any{"form_id": id})
			out, refused, err := caller.call(ctx, "get_form", string(body))
			if err != nil || refused {
				return ""
			}
			return stringField(out, "title")
		case "product":
			out, refused, err := caller.call(ctx, "list_products", `{}`)
			if err != nil || refused {
				return ""
			}
			return nameForID(out, id)
		}
		return ""
	}
}

// stringField reads one string field out of a tool's JSON result.
func stringField(out, field string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		return ""
	}
	if s, ok := m[field].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// nameForID finds an object's display name inside a list tool's result. It looks
// for the documented {items:[…]} shape (list_output.go) and falls back to a bare
// array, so a change of envelope degrades to showing the id rather than a wrong
// name.
func nameForID(out, id string) string {
	var env struct {
		Items []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil || len(env.Items) == 0 {
		var arr []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal([]byte(out), &arr); err != nil {
			return ""
		}
		env.Items = arr
	}
	for _, it := range env.Items {
		if it.ID == id {
			return strings.TrimSpace(it.Name)
		}
	}
	return ""
}

// accountIDFrom reads the account id off the identity mcpsrv carries: the
// workspace context a proposal's confirm will run in.
func accountIDFrom(ctx context.Context) string {
	id := mcpsrv.IdentityFromContext(ctx)
	if id == nil {
		return ""
	}
	return id.AccountID
}

// WriteScopes is the scope set the CONFIRM path uses, and the only place the chat
// gains a write. It is exported next to ReadOnlyScopes so the two are read
// together: a chat message runs read-only, a confirmed proposal runs with write,
// and the diff between them is one named function rather than a string somewhere
// in a handler.
func WriteScopes() []string { return []string{dbstore.ScopeReadWrite} }
